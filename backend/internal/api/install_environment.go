package api

import (
	"net/http"
	"time"

	"context"

	"github.com/f1nn17/vinaire/backend/internal/helm"
)

// InstallEnvironment handles GET /api/v1/install/environment (Roadmap Phase 75b): what the cluster
// offers an install — StorageClasses and which is the default, IngressClasses, whether anything
// would answer a Service of type LoadBalancer. The "Install from an image" wizard builds its own
// objects (no Helm render to read warnings from), so it asks here before the person presses Install.
// All reads, as the caller; a part the caller may not read comes back as "not known".
func (h *Handler) InstallEnvironment(w http.ResponseWriter, r *http.Request) {
	_, clientset, ok := h.resolveCluster(w, r)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	respondJSON(w, helm.ReadEnvironment(ctx, clientset))
}
