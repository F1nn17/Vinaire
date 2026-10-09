package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

	"github.com/shiraku/vinaire/backend/internal/helm"
)

// Roadmap Phase 28: the application catalog. Browsing charts is read-only and
// needs no cluster at all; installing, upgrading, rolling back and uninstalling
// Helm releases are writes (see AGENTS.md) and run as the caller's own
// Kubernetes identity — `resolveRESTConfig` hands Helm the caller's config, so
// the cluster's RBAC decides what a chart may create. Every one is audited,
// without the values (they can hold passwords).

const maxCatalogBody = 1 << 20

func (h *Handler) catalogAvailable(w http.ResponseWriter) bool {
	if h.Catalog == nil || h.Releases == nil {
		respondError(w, http.StatusServiceUnavailable, "the application catalog is not configured")
		return false
	}
	return true
}

// respondHelmError maps what Helm and the catalog return to an HTTP status.
func respondHelmError(w http.ResponseWriter, err error) {
	var status apierrors.APIStatus
	switch {
	case errors.Is(err, helm.ErrInvalid):
		respondError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, helm.ErrNotFound):
		respondError(w, http.StatusNotFound, err.Error())
	case errors.Is(err, helm.ErrConflict):
		respondError(w, http.StatusConflict, err.Error())
	case errors.Is(err, helm.ErrForbidden):
		respondError(w, http.StatusForbidden, err.Error())
	case errors.Is(err, context.DeadlineExceeded):
		respondError(w, http.StatusGatewayTimeout, "the operation timed out: "+err.Error())
	case errors.As(err, &status):
		respondK8sError(w, err)
	default:
		respondError(w, http.StatusBadGateway, err.Error())
	}
}

// ---- browsing ---------------------------------------------------------------

// CatalogRepos handles GET /api/v1/catalog/repos.
func (h *Handler) CatalogRepos(w http.ResponseWriter, _ *http.Request) {
	if !h.catalogAvailable(w) {
		return
	}
	// Only what the UI shows — not the configuration's per-chart search tags.
	type repoView struct {
		Name         string `json:"name"`
		URL          string `json:"url"`
		Description  string `json:"description,omitempty"`
		Category     string `json:"category,omitempty"`
		OCI          bool   `json:"oci,omitempty"`
		Unmaintained bool   `json:"unmaintained,omitempty"`
	}
	var repos []repoView
	for _, r := range h.Catalog.Repos() {
		repos = append(repos, repoView{r.Name, r.URL, r.Description, r.Category, r.IsOCI(), r.Unmaintained})
	}
	respondJSON(w, map[string]any{"repos": repos})
}

// CatalogSearch handles GET /api/v1/catalog/charts?search=&repo=&category=.
func (h *Handler) CatalogSearch(w http.ResponseWriter, r *http.Request) {
	if !h.catalogAvailable(w) {
		return
	}
	q := strings.TrimSpace(r.URL.Query().Get("search"))
	if len(q) > 100 {
		respondError(w, http.StatusBadRequest, "search is too long")
		return
	}
	respondJSON(w, h.Catalog.SearchOpts(r.Context(), q, r.URL.Query().Get("repo"), r.URL.Query().Get("category"),
		helm.SearchOptions{
			IncludeDeprecated:   r.URL.Query().Get("deprecated") == "true",
			IncludeUnmaintained: r.URL.Query().Get("unmaintained") == "true",
		}))
}

// CatalogChart handles GET /api/v1/catalog/charts/{repo}/{chart}?version=.
func (h *Handler) CatalogChart(w http.ResponseWriter, r *http.Request) {
	if !h.catalogAvailable(w) {
		return
	}
	d, err := h.Catalog.Details(r.Context(), chi.URLParam(r, "repo"), chi.URLParam(r, "chart"), r.URL.Query().Get("version"))
	if err != nil {
		respondHelmError(w, err)
		return
	}
	respondJSON(w, d)
}

// CatalogIcon handles GET /api/v1/catalog/icons/{repo}/{chart}: a chart's icon,
// fetched and checked by the backend (see internal/helm/icons.go) so the browser
// never contacts the host that serves it.
func (h *Handler) CatalogIcon(w http.ResponseWriter, r *http.Request) {
	if !h.catalogAvailable(w) {
		return
	}
	icon, err := h.Catalog.Icon(r.Context(), chi.URLParam(r, "repo"), chi.URLParam(r, "chart"))
	if err != nil {
		if errors.Is(err, helm.ErrNotFound) {
			http.NotFound(w, r)
			return
		}
		respondHelmError(w, err)
		return
	}
	w.Header().Set("Content-Type", icon.ContentType)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; sandbox")
	w.Header().Set("Cache-Control", "private, max-age=86400")
	// #nosec G705 -- not HTML: the bytes were content-sniffed to a raster image or a script-free SVG (internal/helm/icons.go), and are served nosniff under a sandboxing CSP
	_, _ = w.Write(icon.Data)
}

// ---- releases (read) ----------------------------------------------------------

// CatalogReleases handles GET /api/v1/catalog/releases?namespace=.
func (h *Handler) CatalogReleases(w http.ResponseWriter, r *http.Request) {
	if !h.catalogAvailable(w) {
		return
	}
	_, cfg, ok := h.resolveRESTConfig(w, r)
	if !ok {
		return
	}
	rels, err := h.Releases.List(r.Context(), cfg, r.URL.Query().Get("namespace"))
	if err != nil {
		respondHelmError(w, err)
		return
	}
	respondJSON(w, map[string]any{"releases": rels})
}

// CatalogReleaseHistory handles GET /api/v1/catalog/releases/{namespace}/{name}/history.
func (h *Handler) CatalogReleaseHistory(w http.ResponseWriter, r *http.Request) {
	if !h.catalogAvailable(w) {
		return
	}
	_, cfg, ok := h.resolveRESTConfig(w, r)
	if !ok {
		return
	}
	rels, err := h.Releases.History(r.Context(), cfg, chi.URLParam(r, "namespace"), chi.URLParam(r, "name"))
	if err != nil {
		respondHelmError(w, err)
		return
	}
	respondJSON(w, map[string]any{"revisions": rels})
}

// CatalogReleaseValues handles GET /api/v1/catalog/releases/{namespace}/{name}/values:
// the values a release was installed with. They can hold passwords, so this is
// audited the way a Secret reveal is.
func (h *Handler) CatalogReleaseValues(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if !h.catalogAvailable(w) {
		return
	}
	_, cfg, ok := h.resolveRESTConfig(w, r)
	if !ok {
		return
	}
	ns, name := chi.URLParam(r, "namespace"), chi.URLParam(r, "name")
	vals, err := h.Releases.Values(r.Context(), cfg, ns, name)
	if err != nil {
		respondHelmError(w, err)
		return
	}
	h.auditLog(r, "helm-reveal-values", ns, name)
	respondJSON(w, map[string]string{"values": vals})
}

// ---- releases (write) ---------------------------------------------------------

type installBody struct {
	Repo            string `json:"repo"`
	Chart           string `json:"chart"`
	Version         string `json:"version"`
	Namespace       string `json:"namespace"`
	Release         string `json:"release"`
	Values          string `json:"values"` // YAML
	NodeName        string `json:"nodeName"`
	CreateNamespace bool   `json:"createNamespace"`
	Wait            bool   `json:"wait"`
	// AllowDeprecated is the explicit "yes, install this unmaintained chart".
	AllowDeprecated bool `json:"allowDeprecated"`
}

func decodeCatalogBody(w http.ResponseWriter, r *http.Request, v any) bool {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxCatalogBody))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return false
	}
	return true
}

// auditHelm records a Helm write — success or failure, since a failed install
// can still have created some of what the chart defines. Values are never
// included, and the error text is capped.
func (h *Handler) auditHelm(r *http.Request, action, namespace, release string, err error, extra ...any) {
	if err != nil {
		msg := err.Error()
		if len(msg) > 300 {
			msg = msg[:300] + "…"
		}
		action += "-failed"
		extra = append(extra, "error", msg)
	}
	h.auditLog(r, action, namespace, release, extra...)
}

// CatalogInstall handles POST /api/v1/catalog/install.
func (h *Handler) CatalogInstall(w http.ResponseWriter, r *http.Request) {
	if !h.catalogAvailable(w) {
		return
	}
	_, cfg, ok := h.resolveRESTConfig(w, r)
	if !ok {
		return
	}
	var b installBody
	if !decodeCatalogBody(w, r, &b) {
		return
	}
	req := helm.InstallRequest{
		Repo: b.Repo, Chart: b.Chart, Version: b.Version, Namespace: b.Namespace, Release: b.Release,
		ValuesYAML: b.Values, NodeName: b.NodeName, CreateNamespace: b.CreateNamespace, Wait: b.Wait, AllowDeprecated: b.AllowDeprecated,
	}
	// Refuse to start an install the caller's rights can't finish: it would fail
	// halfway, with some objects already created. (A failure of the check itself
	// doesn't block — the cluster's RBAC still decides during the install.)
	if pf, perr := h.Releases.Preflight(r.Context(), cfg, req); perr == nil && !pf.OK() {
		h.auditLog(r, "helm-install-refused", b.Namespace, b.Release, "repo", b.Repo, "chart", b.Chart)
		respondInstallForbidden(w, pf)
		return
	} else if perr != nil && errors.Is(perr, helm.ErrInvalid) && !errors.Is(perr, helm.ErrRender) {
		respondHelmError(w, perr)
		return
	}
	res, err := h.Releases.Install(r.Context(), cfg, req)
	if err != nil {
		if !errors.Is(err, helm.ErrInvalid) { // a rejected request touched nothing
			h.auditHelm(r, "helm-install", b.Namespace, b.Release, err, "repo", b.Repo, "chart", b.Chart, "version", b.Version)
		}
		respondHelmError(w, err)
		return
	}
	h.auditHelm(r, "helm-install", b.Namespace, b.Release, nil,
		"repo", b.Repo, "chart", b.Chart, "version", res.Release.ChartVersion, "node", b.NodeName, "createNamespace", b.CreateNamespace)
	w.WriteHeader(http.StatusCreated)
	respondJSON(w, res)
}

type upgradeBody struct {
	Repo     string `json:"repo"`
	Chart    string `json:"chart"`
	Version  string `json:"version"`
	Values   string `json:"values"`
	NodeName string `json:"nodeName"`
	Wait     bool   `json:"wait"`
}

// CatalogUpgrade handles POST /api/v1/catalog/releases/{namespace}/{name}/upgrade.
func (h *Handler) CatalogUpgrade(w http.ResponseWriter, r *http.Request) {
	if !h.catalogAvailable(w) {
		return
	}
	_, cfg, ok := h.resolveRESTConfig(w, r)
	if !ok {
		return
	}
	ns, name := chi.URLParam(r, "namespace"), chi.URLParam(r, "name")
	var b upgradeBody
	if !decodeCatalogBody(w, r, &b) {
		return
	}
	res, err := h.Releases.Upgrade(r.Context(), cfg, helm.UpgradeRequest{
		Repo: b.Repo, Chart: b.Chart, Version: b.Version, Namespace: ns, Release: name,
		ValuesYAML: b.Values, NodeName: b.NodeName, Wait: b.Wait,
	})
	if err != nil {
		if !errors.Is(err, helm.ErrInvalid) {
			h.auditHelm(r, "helm-upgrade", ns, name, err, "repo", b.Repo, "chart", b.Chart, "version", b.Version)
		}
		respondHelmError(w, err)
		return
	}
	h.auditHelm(r, "helm-upgrade", ns, name, nil,
		"repo", b.Repo, "chart", b.Chart, "version", res.Release.ChartVersion, "revision", res.Release.Revision)
	respondJSON(w, res)
}

type rollbackBody struct {
	Revision int `json:"revision"`
}

// CatalogRollback handles POST /api/v1/catalog/releases/{namespace}/{name}/rollback.
func (h *Handler) CatalogRollback(w http.ResponseWriter, r *http.Request) {
	if !h.catalogAvailable(w) {
		return
	}
	_, cfg, ok := h.resolveRESTConfig(w, r)
	if !ok {
		return
	}
	ns, name := chi.URLParam(r, "namespace"), chi.URLParam(r, "name")
	var b rollbackBody
	if !decodeCatalogBody(w, r, &b) {
		return
	}
	if err := h.Releases.Rollback(r.Context(), cfg, ns, name, b.Revision); err != nil {
		if !errors.Is(err, helm.ErrInvalid) {
			h.auditHelm(r, "helm-rollback", ns, name, err, "toRevision", strconv.Itoa(b.Revision))
		}
		respondHelmError(w, err)
		return
	}
	h.auditHelm(r, "helm-rollback", ns, name, nil, "toRevision", strconv.Itoa(b.Revision))
	respondJSON(w, map[string]string{"status": "ok"})
}

type uninstallBody struct {
	ConfirmName string `json:"confirmName"`
}

// CatalogUninstall handles DELETE /api/v1/catalog/releases/{namespace}/{name}.
// Like deleting a namespace, it needs the release's name repeated in the body.
func (h *Handler) CatalogUninstall(w http.ResponseWriter, r *http.Request) {
	if !h.catalogAvailable(w) {
		return
	}
	_, cfg, ok := h.resolveRESTConfig(w, r)
	if !ok {
		return
	}
	ns, name := chi.URLParam(r, "namespace"), chi.URLParam(r, "name")
	var b uninstallBody
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&b); err != nil || b.ConfirmName != name {
		respondError(w, http.StatusBadRequest, `request body must be {"confirmName": "<the release's name>"}`)
		return
	}
	if err := h.Releases.Uninstall(r.Context(), cfg, ns, name); err != nil {
		if !errors.Is(err, helm.ErrInvalid) {
			h.auditHelm(r, "helm-uninstall", ns, name, err)
		}
		respondHelmError(w, err)
		return
	}
	h.auditHelm(r, "helm-uninstall", ns, name, nil)
	// Roadmap Phase 59: Helm leaves a StatefulSet's volume claims (and any a chart marks to be
	// kept) behind. Say which, so the data left in the namespace isn't a surprise.
	respondJSON(w, map[string]any{"status": "ok", "leftovers": helm.LeftoverClaims(r.Context(), cfg, ns, name)})
}

// respondInstallForbidden answers an install the caller lacks rights for: the
// usual {"error"} plus the structured list the UI turns into a checklist.
func respondInstallForbidden(w http.ResponseWriter, pf *helm.PreflightResult) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusForbidden)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"error":   "you don't have the permissions this chart needs: " + pf.String(),
		"missing": pf.Missing,
	})
}

// CatalogInstallPreflight handles POST /api/v1/catalog/install/preflight: what
// installing this chart with these values would need, and what the caller lacks.
// Nothing is created — the chart is rendered locally and the cluster is only
// asked access questions, as the caller.
func (h *Handler) CatalogInstallPreflight(w http.ResponseWriter, r *http.Request) {
	if !h.catalogAvailable(w) {
		return
	}
	_, cfg, ok := h.resolveRESTConfig(w, r)
	if !ok {
		return
	}
	var b installBody
	if !decodeCatalogBody(w, r, &b) {
		return
	}
	pf, err := h.Releases.Preflight(r.Context(), cfg, helm.InstallRequest{
		Repo: b.Repo, Chart: b.Chart, Version: b.Version, Namespace: b.Namespace, Release: b.Release,
		ValuesYAML: b.Values, NodeName: b.NodeName, CreateNamespace: b.CreateNamespace, AllowDeprecated: b.AllowDeprecated,
	})
	if err != nil {
		respondHelmError(w, err)
		return
	}
	respondJSON(w, pf)
}

// CatalogInstallManifest handles POST /api/v1/catalog/install/manifest: the YAML this install would
// apply, rendered locally with the given values, Secret values redacted (Roadmap Phase 59). Nothing
// is created — like the preflight, it is a read as the caller.
func (h *Handler) CatalogInstallManifest(w http.ResponseWriter, r *http.Request) {
	if !h.catalogAvailable(w) {
		return
	}
	_, cfg, ok := h.resolveRESTConfig(w, r)
	if !ok {
		return
	}
	var b installBody
	if !decodeCatalogBody(w, r, &b) {
		return
	}
	manifest, truncated, err := h.Releases.RenderManifest(r.Context(), cfg, helm.InstallRequest{
		Repo: b.Repo, Chart: b.Chart, Version: b.Version, Namespace: b.Namespace, Release: b.Release,
		ValuesYAML: b.Values, NodeName: b.NodeName, CreateNamespace: b.CreateNamespace, AllowDeprecated: b.AllowDeprecated,
	})
	if err != nil {
		respondHelmError(w, err)
		return
	}
	respondJSON(w, map[string]any{"manifest": manifest, "truncated": truncated})
}

// ---- install jobs (progress) ----------------------------------------------------

func (h *Handler) installJobsAvailable(w http.ResponseWriter) bool {
	if !h.catalogAvailable(w) {
		return false
	}
	if h.InstallJobs == nil {
		respondError(w, http.StatusServiceUnavailable, "background installs are not configured")
		return false
	}
	return true
}

// CatalogStartInstallJob handles POST /api/v1/catalog/install-jobs: the same
// install as POST /catalog/install, run in the background as the caller so the
// UI can poll its progress. Audited exactly like a direct install (the outcome,
// when it ends), and a request that is refused or invalid touches nothing.
func (h *Handler) CatalogStartInstallJob(w http.ResponseWriter, r *http.Request) {
	if !h.installJobsAvailable(w) {
		return
	}
	_, cfg, ok := h.resolveRESTConfig(w, r)
	if !ok {
		return
	}
	var b installBody
	if !decodeCatalogBody(w, r, &b) {
		return
	}
	req := helm.InstallRequest{
		Repo: b.Repo, Chart: b.Chart, Version: b.Version, Namespace: b.Namespace, Release: b.Release,
		ValuesYAML: b.Values, NodeName: b.NodeName, CreateNamespace: b.CreateNamespace, Wait: b.Wait, AllowDeprecated: b.AllowDeprecated,
	}
	// The job outlives this request, but it must keep the caller's identity for
	// the audit line it writes when it ends.
	detached := r.Clone(context.WithoutCancel(r.Context()))
	job, err := h.InstallJobs.StartInstall(h.Releases, cfg, ownerKey(r), req, func(res *helm.InstallResult, err error) {
		switch {
		case err == nil:
			h.auditHelm(detached, "helm-install", b.Namespace, b.Release, nil,
				"repo", b.Repo, "chart", b.Chart, "version", res.Release.ChartVersion, "node", b.NodeName, "createNamespace", b.CreateNamespace)
		case errors.Is(err, helm.ErrForbidden) && strings.Contains(err.Error(), "permissions this chart needs"):
			h.auditLog(detached, "helm-install-refused", b.Namespace, b.Release, "repo", b.Repo, "chart", b.Chart)
		case errors.Is(err, context.Canceled):
			h.auditLog(detached, "helm-install-cancelled", b.Namespace, b.Release, "repo", b.Repo, "chart", b.Chart)
		case errors.Is(err, helm.ErrInvalid):
			// a rejected request touched nothing
		default:
			h.auditHelm(detached, "helm-install", b.Namespace, b.Release, err, "repo", b.Repo, "chart", b.Chart, "version", b.Version)
		}
	}, helm.WithRollbackHook(func(err error) {
		// Roadmap Phase 60: the release was taken back (or it couldn't be). Audited on its own,
		// without values, like every other Helm write.
		h.auditHelm(detached, "helm-install-rollback", b.Namespace, b.Release, err, "repo", b.Repo, "chart", b.Chart)
	}))
	if err != nil {
		if errors.Is(err, helm.ErrTooManyJobs) {
			respondError(w, http.StatusTooManyRequests, err.Error())
			return
		}
		respondHelmError(w, err)
		return
	}
	w.WriteHeader(http.StatusAccepted)
	respondJSON(w, map[string]string{"id": job.ID})
}

type installPod struct {
	Name   string `json:"name"`
	Phase  string `json:"phase"`
	Ready  int    `json:"ready"`
	Total  int    `json:"total"`
	Reason string `json:"reason,omitempty"`
}

// CatalogInstallJob handles GET /api/v1/catalog/install-jobs/{id}: where the
// install is, and — once objects exist — how its pods are coming up (found by the
// standard app.kubernetes.io/instance label, read as the caller).
func (h *Handler) CatalogInstallJob(w http.ResponseWriter, r *http.Request) {
	if !h.installJobsAvailable(w) {
		return
	}
	job, ok := h.InstallJobs.Get(chi.URLParam(r, "id"), ownerKey(r))
	if !ok {
		respondError(w, http.StatusNotFound, "no such install")
		return
	}
	snap := job.Snapshot()
	out := map[string]any{"job": snap}
	if snap.Stage == helm.StageInstalling || snap.Stage == helm.StageDone {
		if _, cfg, ok := h.resolveRESTConfig(w, r); ok {
			out["pods"] = installPods(r.Context(), cfg, snap.Namespace, snap.Release)
		} else {
			return
		}
	}
	respondJSON(w, out)
}

func installPods(ctx context.Context, cfg *rest.Config, namespace, release string) []installPod {
	cs, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return nil
	}
	list, err := cs.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: "app.kubernetes.io/instance=" + release, Limit: 30,
	})
	if err != nil {
		return nil // can't list pods (RBAC, not created yet) — the progress just has no pod rows
	}
	pods := make([]installPod, 0, len(list.Items))
	for _, p := range list.Items {
		ip := installPod{Name: p.Name, Phase: string(p.Status.Phase), Total: len(p.Spec.Containers)}
		for _, st := range p.Status.ContainerStatuses {
			if st.Ready {
				ip.Ready++
			}
			if st.State.Waiting != nil && ip.Reason == "" {
				ip.Reason = st.State.Waiting.Reason // ContainerCreating, ImagePullBackOff, …
			}
		}
		for _, st := range p.Status.InitContainerStatuses {
			if st.State.Waiting != nil && ip.Reason == "" {
				ip.Reason = "init: " + st.State.Waiting.Reason
			}
		}
		pods = append(pods, ip)
	}
	return pods
}

// CatalogCancelInstallJob handles DELETE /api/v1/catalog/install-jobs/{id}: stops a running
// install. Before Helm starts that is all; once it is creating objects (Roadmap Phase 60) Helm is
// interrupted and the release it began is uninstalled — as the caller, like the install. On an
// install that already failed after Helm began, the same call undoes it. The job's owner only.
func (h *Handler) CatalogCancelInstallJob(w http.ResponseWriter, r *http.Request) {
	if !h.installJobsAvailable(w) {
		return
	}
	job, ok := h.InstallJobs.Get(chi.URLParam(r, "id"), ownerKey(r))
	if !ok {
		respondError(w, http.StatusNotFound, "no such install")
		return
	}
	if job.Cancel() {
		respondJSON(w, map[string]bool{"cancelled": true})
		return
	}
	_, cfg, ok := h.resolveRESTConfig(w, r)
	if !ok {
		return
	}
	if h.InstallJobs.RollbackFailed(h.Releases, cfg, job) {
		respondJSON(w, map[string]bool{"rollingBack": true})
		return
	}
	respondError(w, http.StatusConflict, "this install isn't running and has nothing to roll back")
}
