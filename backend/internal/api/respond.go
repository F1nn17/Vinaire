package api

import (
	"encoding/json"
	"log/slog"
	"net/http"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
)

func respondJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(v); err != nil {
		slog.Error("encode response", "error", err)
	}
}

func respondError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(map[string]string{"error": message}); err != nil {
		slog.Error("encode error response", "error", err)
	}
}

// respondK8sError maps a client-go error to the HTTP status it actually
// means, instead of flattening everything to 502. Since Roadmap Phase 10,
// every Kubernetes API call runs as the caller's own logged-in identity
// (see resolveCluster), so a permission error here is a *real* RBAC
// decision — an unauthorized attempt must come back as 403, not a
// generic gateway error, for the frontend's "not authorized" handling to
// work and for this to actually match what `kubectl` would show.
func respondK8sError(w http.ResponseWriter, err error) {
	status := http.StatusBadGateway
	switch {
	case apierrors.IsForbidden(err):
		status = http.StatusForbidden
	case apierrors.IsUnauthorized(err):
		status = http.StatusUnauthorized
	case apierrors.IsNotFound(err):
		status = http.StatusNotFound
	}
	respondError(w, status, err.Error())
}
