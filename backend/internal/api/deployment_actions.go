package api

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/shiraku/vinaire/backend/internal/authn"
	"github.com/shiraku/vinaire/backend/internal/k8s"
)

// This file holds the HTTP handlers for the three write actions Phase 9 /
// AGENTS.md's core rule allow — see internal/k8s/deployment_actions.go for
// the actual client-go calls. Whether the caller is actually *allowed* to
// restart/delete/rollback a given Deployment is now decided by the real
// Kubernetes RBAC bound to their own logged-in identity (Roadmap
// Phase 10 — see resolveCluster/Entry.ClientsetForToken), not a separate
// check here: an unauthorized attempt just gets a 403 back from the K8s
// API itself, same as `kubectl` would.

// RestartDeployment handles POST /api/v1/deployments/{namespace}/{name}/restart.
func (h *Handler) RestartDeployment(w http.ResponseWriter, r *http.Request) {
	_, clientset, ok := h.resolveCluster(w, r)
	if !ok {
		return
	}
	namespace := chi.URLParam(r, "namespace")
	name := chi.URLParam(r, "name")

	if err := k8s.RestartDeployment(r.Context(), clientset, namespace, name); err != nil {
		respondK8sError(w, err)
		return
	}

	auditWriteAction(r, "restart", namespace, name)
	respondJSON(w, map[string]string{"status": "ok"})
}

// DeleteDeployment handles DELETE /api/v1/deployments/{namespace}/{name}.
func (h *Handler) DeleteDeployment(w http.ResponseWriter, r *http.Request) {
	_, clientset, ok := h.resolveCluster(w, r)
	if !ok {
		return
	}
	namespace := chi.URLParam(r, "namespace")
	name := chi.URLParam(r, "name")

	if err := k8s.DeleteDeployment(r.Context(), clientset, namespace, name); err != nil {
		respondK8sError(w, err)
		return
	}

	auditWriteAction(r, "delete", namespace, name)
	respondJSON(w, map[string]string{"status": "ok"})
}

type rollbackRequest struct {
	Revision int64 `json:"revision"`
}

// RollbackDeployment handles POST /api/v1/deployments/{namespace}/{name}/rollback.
func (h *Handler) RollbackDeployment(w http.ResponseWriter, r *http.Request) {
	_, clientset, ok := h.resolveCluster(w, r)
	if !ok {
		return
	}
	namespace := chi.URLParam(r, "namespace")
	name := chi.URLParam(r, "name")

	var req rollbackRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Revision <= 0 {
		respondError(w, http.StatusBadRequest, `request body must be {"revision": <positive integer>}`)
		return
	}

	if err := k8s.RollbackDeployment(r.Context(), clientset, namespace, name, req.Revision); err != nil {
		respondK8sError(w, err)
		return
	}

	auditWriteAction(r, "rollback", namespace, name, "revision", req.Revision)
	respondJSON(w, map[string]string{"status": "ok"})
}

// auditWriteAction logs every write action Phase 9 allows — see AGENTS.md's
// core rule. "who" is the caller's logged-in identity (best-effort
// username in OIDC mode, empty in token mode — a pasted ServiceAccount
// token carries no human username) plus their source address; extra
// key/value pairs add action-specific context (e.g. rollback's target
// revision).
func auditWriteAction(r *http.Request, action, namespace, name string, extra ...any) {
	username := ""
	if sess, ok := authn.FromContext(r.Context()); ok {
		username = sess.Username
	}
	args := []any{"action", action, "namespace", namespace, "name", name, "user", username, "remote_addr", r.RemoteAddr}
	args = append(args, extra...)
	slog.Warn("write action", args...)
}
