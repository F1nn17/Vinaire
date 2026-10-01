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
		Name        string `json:"name"`
		URL         string `json:"url"`
		Description string `json:"description,omitempty"`
		Category    string `json:"category,omitempty"`
		OCI         bool   `json:"oci,omitempty"`
	}
	var repos []repoView
	for _, r := range h.Catalog.Repos() {
		repos = append(repos, repoView{r.Name, r.URL, r.Description, r.Category, r.IsOCI()})
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
	respondJSON(w, h.Catalog.Search(r.Context(), q, r.URL.Query().Get("repo"), r.URL.Query().Get("category")))
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
	res, err := h.Releases.Install(r.Context(), cfg, helm.InstallRequest{
		Repo: b.Repo, Chart: b.Chart, Version: b.Version, Namespace: b.Namespace, Release: b.Release,
		ValuesYAML: b.Values, NodeName: b.NodeName, CreateNamespace: b.CreateNamespace, Wait: b.Wait,
	})
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
	respondJSON(w, map[string]string{"status": "ok"})
}
