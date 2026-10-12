package api

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/dynamic"

	"github.com/f1nn17/vinaire/backend/internal/audit"
	"github.com/f1nn17/vinaire/backend/internal/authn"
	"github.com/f1nn17/vinaire/backend/internal/clusters"
	"github.com/f1nn17/vinaire/backend/internal/gitops"
)

// Roadmap Phase 29: GitOps. A cluster's mode (off / native / argocd) and the
// native sources are Vinaire's own configuration, like the cluster registry;
// the Argo CD calls run as the caller, so the cluster's RBAC decides. A native
// source's manifests are applied by a background worker as the *ServiceAccount
// whose token the source was created with* — see internal/gitops and AGENTS.md.

const maxGitOpsBody = 64 << 10

func (h *Handler) gitopsAvailable(w http.ResponseWriter) bool {
	if h.GitOps == nil || !h.GitOps.Available() {
		respondError(w, http.StatusServiceUnavailable, gitops.ErrUnavailable.Error())
		return false
	}
	return true
}

func respondGitOpsError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, gitops.ErrInvalid):
		respondError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, gitops.ErrNotFound):
		respondError(w, http.StatusNotFound, err.Error())
	case errors.Is(err, gitops.ErrConflict):
		respondError(w, http.StatusConflict, err.Error())
	case errors.Is(err, gitops.ErrUnavailable), errors.Is(err, gitops.ErrUnencrypted):
		respondError(w, http.StatusServiceUnavailable, err.Error())
	default:
		respondK8sError(w, err)
	}
}

// AuditBackground records an action that has no HTTP request (a background
// sync): the same slog line and durable record as auditLog.
func (h *Handler) AuditBackground(action, name, identity string, detail map[string]any) {
	args := []any{"action", action, "name", name, "user", identity}
	for k, v := range detail {
		args = append(args, k, v)
	}
	slog.Warn("audited action", args...)
	if h.Audit != nil {
		h.Audit.Write(audit.Entry{Action: action, Name: name, Identity: identity, Detail: detail})
	}
}

func callerName(r *http.Request) string {
	if sess, ok := authn.FromContext(r.Context()); ok && sess.Username != "" {
		return sess.Username
	}
	return "user"
}

// argoDetected asks the cluster, as the caller, whether Argo CD's CRD exists —
// cached briefly, since the overview is polled.
var (
	argoMu    sync.Mutex
	argoCache = map[string]struct {
		at time.Time
		ok bool
	}{}
)

func argoDetected(entry *clusters.Entry, d discovery.DiscoveryInterface) bool {
	argoMu.Lock()
	if c, hit := argoCache[entry.Name]; hit && time.Since(c.at) < 30*time.Second {
		argoMu.Unlock()
		return c.ok
	}
	argoMu.Unlock()
	ok := gitops.DetectArgoCD(d)
	argoMu.Lock()
	argoCache[entry.Name] = struct {
		at time.Time
		ok bool
	}{time.Now(), ok}
	argoMu.Unlock()
	return ok
}

type gitopsOverview struct {
	Cluster      string              `json:"cluster"`
	Available    bool                `json:"available"`
	Mode         gitops.Mode         `json:"mode"`
	ArgoDetected bool                `json:"argoDetected"`
	Warning      string              `json:"warning,omitempty"`
	Sources      []gitops.SourceView `json:"sources"`
}

func (h *Handler) overview(entry *clusters.Entry, argo bool) gitopsOverview {
	o := gitopsOverview{Cluster: entry.Name, Available: h.GitOps != nil && h.GitOps.Available(), Mode: gitops.ModeOff, ArgoDetected: argo, Sources: []gitops.SourceView{}}
	if h.GitOps == nil {
		return o
	}
	o.Mode = h.GitOps.Mode(entry.Name)
	o.Sources = h.GitOps.Sources(entry.Name)
	if o.Mode == gitops.ModeNative && argo {
		o.Warning = "Argo CD is installed on this cluster. Running native sync here as well means two controllers apply to the same cluster and can overwrite each other — use Argo CD mode instead, or make sure they manage different objects."
	}
	return o
}

// GitOpsOverview handles GET /api/v1/gitops.
func (h *Handler) GitOpsOverview(w http.ResponseWriter, r *http.Request) {
	entry, cfg, ok := h.resolveRESTConfig(w, r)
	if !ok {
		return
	}
	argo := false
	if dc, err := discovery.NewDiscoveryClientForConfig(cfg); err == nil {
		argo = argoDetected(entry, dc)
	}
	respondJSON(w, h.overview(entry, argo))
}

// GitOpsSetMode handles PUT /api/v1/gitops/mode.
func (h *Handler) GitOpsSetMode(w http.ResponseWriter, r *http.Request) {
	if !h.requireVinaireRight(w, r, "gitops", "update") {
		return
	}
	if !h.gitopsAvailable(w) {
		return
	}
	entry, cfg, ok := h.resolveRESTConfig(w, r)
	if !ok {
		return
	}
	var body struct {
		Mode string `json:"mode"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&body); err != nil {
		respondError(w, http.StatusBadRequest, `request body must be {"mode": "off" | "native" | "argocd"}`)
		return
	}
	mode, valid := gitops.ParseMode(body.Mode)
	if !valid {
		respondError(w, http.StatusBadRequest, "mode must be off, native or argocd")
		return
	}
	prev := h.GitOps.Mode(entry.Name)
	if err := h.GitOps.SetMode(r.Context(), entry.Name, mode); err != nil {
		respondGitOpsError(w, err)
		return
	}
	h.auditLog(r, "gitops-mode", "", entry.Name, "mode", string(mode), "previous", string(prev))
	argo := false
	if dc, err := discovery.NewDiscoveryClientForConfig(cfg); err == nil {
		argo = argoDetected(entry, dc)
	}
	respondJSON(w, h.overview(entry, argo))
}

type sourceBody struct {
	Name            string `json:"name"`
	RepoURL         string `json:"repoURL"`
	Revision        string `json:"revision"`
	Path            string `json:"path"`
	Recurse         bool   `json:"recurse"`
	Namespace       string `json:"namespace"`
	IntervalSeconds int    `json:"intervalSeconds"`
	AutoSync        bool   `json:"autoSync"`
	Force           bool   `json:"force"`
	GitUsername     string `json:"gitUsername"`
	GitToken        string `json:"gitToken"`
	ApplyToken      string `json:"applyToken"`
}

// GitOpsAddSource handles POST /api/v1/gitops/sources. The two tokens arrive in
// the body (never a URL), only over a secure transport, are sealed at rest, and
// are never returned or logged.
func (h *Handler) GitOpsAddSource(w http.ResponseWriter, r *http.Request) {
	if !h.requireVinaireRight(w, r, "gitops", "create") {
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	if !h.gitopsAvailable(w) || !h.requireSecureTransport(w, r) {
		return
	}
	entry, _, ok := h.resolveRESTConfig(w, r)
	if !ok {
		return
	}
	var b sourceBody
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxGitOpsBody))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&b); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}
	v, err := h.GitOps.AddSource(r.Context(), gitops.AddRequest{
		Name: b.Name, Cluster: entry.Name, RepoURL: b.RepoURL, Revision: b.Revision, Path: b.Path,
		Namespace: b.Namespace, Recurse: b.Recurse, AutoSync: b.AutoSync, Force: b.Force,
		IntervalSeconds: b.IntervalSeconds, GitUsername: b.GitUsername, GitToken: b.GitToken, ApplyToken: b.ApplyToken,
	}, callerName(r))
	if err != nil {
		respondGitOpsError(w, err)
		return
	}
	h.auditLog(r, "gitops-source-create", "", v.Name,
		"cluster", entry.Name, "repo", v.RepoURL, "revision", v.Revision, "path", v.Path,
		"autoSync", v.AutoSync, "force", v.Force, "appliesAs", v.ApplyAs, "hasGitToken", v.HasGitToken)
	w.WriteHeader(http.StatusCreated)
	respondJSON(w, v)
}

// sourceFor loads a source and makes sure it belongs to the cluster the request targets.
func (h *Handler) sourceFor(w http.ResponseWriter, r *http.Request) (gitops.SourceView, *clusters.Entry, bool) {
	entry, _, ok := h.resolveRESTConfig(w, r)
	if !ok {
		return gitops.SourceView{}, nil, false
	}
	if h.GitOps == nil {
		respondError(w, http.StatusServiceUnavailable, gitops.ErrUnavailable.Error())
		return gitops.SourceView{}, nil, false
	}
	v, err := h.GitOps.Source(chi.URLParam(r, "id"))
	if err != nil || v.Cluster != entry.Name {
		respondError(w, http.StatusNotFound, "no such source on this cluster")
		return gitops.SourceView{}, nil, false
	}
	return v, entry, true
}

// GitOpsSource handles GET /api/v1/gitops/sources/{id}.
func (h *Handler) GitOpsSource(w http.ResponseWriter, r *http.Request) {
	if v, _, ok := h.sourceFor(w, r); ok {
		respondJSON(w, v)
	}
}

// GitOpsRemoveSource handles DELETE /api/v1/gitops/sources/{id}. What the source
// applied stays in the cluster.
func (h *Handler) GitOpsRemoveSource(w http.ResponseWriter, r *http.Request) {
	if !h.requireVinaireRight(w, r, "gitops", "delete") {
		return
	}
	v, _, ok := h.sourceFor(w, r)
	if !ok {
		return
	}
	if _, err := h.GitOps.RemoveSource(r.Context(), v.ID); err != nil {
		respondGitOpsError(w, err)
		return
	}
	h.auditLog(r, "gitops-source-delete", "", v.Name, "cluster", v.Cluster, "repo", v.RepoURL)
	respondJSON(w, map[string]string{"status": "ok"})
}

// GitOpsSyncSource handles POST /api/v1/gitops/sources/{id}/sync.
func (h *Handler) GitOpsSyncSource(w http.ResponseWriter, r *http.Request) {
	if !h.requireVinaireRight(w, r, "gitops", "update") {
		return
	}
	v, _, ok := h.sourceFor(w, r)
	if !ok {
		return
	}
	got, err := h.GitOps.SyncNow(v.ID, callerName(r))
	if err != nil {
		respondGitOpsError(w, err)
		return
	}
	h.auditLog(r, "gitops-sync-now", "", v.Name, "cluster", v.Cluster)
	w.WriteHeader(http.StatusAccepted)
	respondJSON(w, got)
}

// ---- Argo CD ------------------------------------------------------------------

func (h *Handler) argoDynamic(w http.ResponseWriter, r *http.Request) (dynamic.Interface, *clusters.Entry, bool) {
	entry, cfg, ok := h.resolveRESTConfig(w, r)
	if !ok {
		return nil, nil, false
	}
	dyn, err := dynamic.NewForConfig(cfg)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to build dynamic client: "+err.Error())
		return nil, nil, false
	}
	return dyn, entry, true
}

// GitOpsArgoApplications handles GET /api/v1/gitops/argocd/applications.
func (h *Handler) GitOpsArgoApplications(w http.ResponseWriter, r *http.Request) {
	entry, cfg, ok := h.resolveRESTConfig(w, r)
	if !ok {
		return
	}
	dc, err := discovery.NewDiscoveryClientForConfig(cfg)
	if err != nil || !argoDetected(entry, dc) {
		respondJSON(w, map[string]any{"detected": false, "applications": []gitops.Application{}})
		return
	}
	dyn, _, ok := h.argoDynamic(w, r)
	if !ok {
		return
	}
	apps, err := gitops.ListApplications(r.Context(), dyn)
	if err != nil {
		respondK8sError(w, err)
		return
	}
	respondJSON(w, map[string]any{"detected": true, "applications": apps})
}

// GitOpsArgoSync handles POST /api/v1/gitops/argocd/applications/{namespace}/{name}/sync.
func (h *Handler) GitOpsArgoSync(w http.ResponseWriter, r *http.Request) {
	dyn, entry, ok := h.argoDynamic(w, r)
	if !ok {
		return
	}
	ns, name := chi.URLParam(r, "namespace"), chi.URLParam(r, "name")
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	if err := gitops.SyncApplication(ctx, dyn, ns, name); err != nil {
		respondGitOpsError(w, err)
		return
	}
	h.auditLog(r, "gitops-argocd-sync", ns, name, "cluster", entry.Name)
	w.WriteHeader(http.StatusAccepted)
	respondJSON(w, map[string]string{"status": "requested"})
}
