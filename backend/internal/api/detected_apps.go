package api

import (
	"net/http"

	"github.com/f1nn17/vinaire/backend/internal/apps"
	"github.com/f1nn17/vinaire/backend/internal/k8s"
)

// DetectedApps handles GET /api/v1/apps/detected (Roadmap Phase 74): the well-known applications —
// PostgreSQL, Redis/Valkey, MinIO, Keycloak — that run in the cluster, found among the pods the
// caller may list, by their images (internal/apps). It reads as the caller and writes nothing; the
// Dashboards page uses it to show a dashboard for an application only when there is one.
func (h *Handler) DetectedApps(w http.ResponseWriter, r *http.Request) {
	_, clientset, ok := h.resolveCluster(w, r)
	if !ok {
		return
	}
	pods, err := k8s.ListPods(r.Context(), clientset, "")
	if err != nil {
		respondK8sError(w, err)
		return
	}
	found := apps.Detect(pods)
	if found == nil {
		found = []apps.App{}
	}
	respondJSON(w, map[string]any{"apps": found})
}
