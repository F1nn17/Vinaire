package api

import "net/http"

// ClusterRow matches the frontend's Cluster type (frontend/src/lib/types.ts).
type ClusterRow struct {
	Name    string `json:"name"`
	Default bool   `json:"default"`
}

// ListClusters handles GET /api/v1/clusters (Roadmap Phase 11) — every
// kubeconfig context this backend is configured to talk to, in
// configuration order. Named ListClusters (not Clusters) because Handler
// already has a Clusters field for the registry itself.
func (h *Handler) ListClusters(w http.ResponseWriter, r *http.Request) {
	names := h.Clusters.Names()
	def := h.Clusters.Default()

	rows := make([]ClusterRow, 0, len(names))
	for _, name := range names {
		rows = append(rows, ClusterRow{Name: name, Default: def != nil && def.Name == name})
	}
	respondJSON(w, rows)
}
