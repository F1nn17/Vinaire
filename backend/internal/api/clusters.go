package api

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/shiraku/vinaire/backend/internal/clusters"
)

// ClusterRow matches the frontend's Cluster type (frontend/src/lib/types.ts).
type ClusterRow struct {
	Name    string `json:"name"`
	Default bool   `json:"default"`
	// Removable is false for the env-var-configured cluster(s) — the
	// settings UI shouldn't offer to remove those (see clusters.Entry).
	Removable bool `json:"removable"`
}

// ListClusters handles GET /api/v1/clusters (Roadmap Phase 11) — every
// kubeconfig context this backend is configured to talk to, in
// configuration order, plus any cluster added dynamically through the
// cluster-management UI. Named ListClusters (not Clusters) because Handler
// already has a Clusters field for the registry itself.
func (h *Handler) ListClusters(w http.ResponseWriter, r *http.Request) {
	names := h.Clusters.Names()
	def := h.Clusters.Default()

	rows := make([]ClusterRow, 0, len(names))
	for _, name := range names {
		entry, _ := h.Clusters.Get(name)
		rows = append(rows, ClusterRow{
			Name:      name,
			Default:   def != nil && def.Name == name,
			Removable: entry != nil && entry.Removable,
		})
	}
	respondJSON(w, rows)
}

// addClusterRequest is the cluster-management UI's "add a cluster" form:
// name + server URL + a bearer token (typically a ServiceAccount token
// from that cluster, the same shape Vinaire's own token-mode login already
// asks users to paste — see AGENTS.md's ClusterRole example). caData is an
// optional base64-free PEM CA certificate; omitting it means the
// connection skips TLS certificate verification, a real tradeoff the
// frontend must surface explicitly rather than silently accept.
type addClusterRequest struct {
	Name   string `json:"name"`
	Server string `json:"server"`
	Token  string `json:"token"`
	CAData string `json:"caData"`
}

// AddCluster handles POST /api/v1/clusters — cluster management via the
// web UI. Validates the submitted credentials against a real API call
// before persisting or registering anything, so a typo'd token/server
// fails loudly and immediately instead of silently showing up broken in
// every page that later tries to use it. Available only when this backend
// is running in-cluster (h.ClusterStore is nil otherwise) — there's
// nowhere to durably persist an added cluster from outside one.
func (h *Handler) AddCluster(w http.ResponseWriter, r *http.Request) {
	if h.ClusterStore == nil {
		respondError(w, http.StatusServiceUnavailable, "cluster management is only available when Vinaire runs in-cluster")
		return
	}

	var req addClusterRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	req.Server = strings.TrimSpace(req.Server)
	req.Token = strings.TrimSpace(req.Token)
	req.CAData = strings.TrimSpace(req.CAData)
	if req.Name == "" || req.Server == "" || req.Token == "" {
		respondError(w, http.StatusBadRequest, "name, server, and token are all required")
		return
	}
	if _, exists := h.Clusters.Get(req.Name); exists {
		respondError(w, http.StatusConflict, "a cluster named "+req.Name+" is already configured")
		return
	}

	sc := clusters.StoredCluster{Name: req.Name, Server: req.Server, Token: req.Token, CAData: req.CAData}
	entry, err := clusters.EntryFromStored(sc)
	if err != nil {
		respondError(w, http.StatusBadRequest, "failed to build a client for this cluster: "+err.Error())
		return
	}
	if err := clusters.VerifyConnection(entry); err != nil {
		respondError(w, http.StatusBadRequest, "could not connect to this cluster: "+err.Error())
		return
	}

	stored, err := h.ClusterStore.Load(r.Context())
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to load existing clusters: "+err.Error())
		return
	}
	stored = append(stored, sc)
	if err := h.ClusterStore.Save(r.Context(), stored); err != nil {
		respondError(w, http.StatusInternalServerError, "failed to save cluster: "+err.Error())
		return
	}

	h.Clusters.AddDynamic(entry)
	h.auditLog(r, "add_cluster", "", req.Name, "server", req.Server)
	respondJSON(w, ClusterRow{Name: req.Name, Default: false, Removable: true})
}

// RemoveCluster handles DELETE /api/v1/clusters/{name} — the counterpart
// to AddCluster. Refuses to remove the default cluster (typically the one
// Vinaire itself runs inside) and anything not added dynamically, since
// those would just reappear on the next restart from their env var anyway.
func (h *Handler) RemoveCluster(w http.ResponseWriter, r *http.Request) {
	if h.ClusterStore == nil {
		respondError(w, http.StatusServiceUnavailable, "cluster management is only available when Vinaire runs in-cluster")
		return
	}

	name := chi.URLParam(r, "name")
	entry, exists := h.Clusters.Get(name)
	if !exists {
		respondError(w, http.StatusNotFound, "no such cluster")
		return
	}
	if !entry.Removable {
		respondError(w, http.StatusBadRequest, "this cluster is configured via an environment variable and can't be removed here")
		return
	}

	stored, err := h.ClusterStore.Load(r.Context())
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to load existing clusters: "+err.Error())
		return
	}
	kept := make([]clusters.StoredCluster, 0, len(stored))
	for _, sc := range stored {
		if sc.Name != name {
			kept = append(kept, sc)
		}
	}
	if err := h.ClusterStore.Save(r.Context(), kept); err != nil {
		respondError(w, http.StatusInternalServerError, "failed to save clusters: "+err.Error())
		return
	}

	h.Clusters.Remove(name)
	h.auditLog(r, "remove_cluster", "", name)
	respondJSON(w, map[string]string{"status": "ok"})
}
