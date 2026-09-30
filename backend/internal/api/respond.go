package api

import (
	"encoding/json"
	"errors"
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
	message := err.Error()

	switch {
	case apierrors.IsForbidden(err):
		status = http.StatusForbidden
		message = forbiddenMessage(err)
	case apierrors.IsUnauthorized(err):
		status = http.StatusUnauthorized
	case apierrors.IsNotFound(err):
		status = http.StatusNotFound
	case apierrors.IsConflict(err), apierrors.IsAlreadyExists(err):
		status = http.StatusConflict
	case apierrors.IsInvalid(err), apierrors.IsBadRequest(err):
		status = http.StatusUnprocessableEntity
		message = statusMessage(err)
	}
	respondError(w, status, message)
}

// forbiddenMessage extracts the Kubernetes API server's own explanation for
// a Forbidden error (it already names the verb/resource/namespace, e.g.
// `deployments.apps "x" is forbidden: User "y" cannot patch resource
// "deployments" in API group "apps" in the namespace "z"`) — Roadmap
// Phase 17 wants a specific, request-derived message in the response body,
// not this backend's own wrapping prefix (e.g. "list pods: ") tacked in
// front of it. errors.As unwraps through that wrapping to find the
// underlying *apierrors.StatusError.
func forbiddenMessage(err error) string {
	var statusErr *apierrors.StatusError
	if errors.As(err, &statusErr) {
		return statusErr.Status().Message
	}
	return err.Error()
}

// statusMessage pulls the API server's own explanation out of a StatusError
// (for a dry-run rejection that's the specific field-level validation
// failure), not this backend's wrapping prefix.
func statusMessage(err error) string {
	var statusErr *apierrors.StatusError
	if errors.As(err, &statusErr) {
		return statusErr.Status().Message
	}
	return err.Error()
}
