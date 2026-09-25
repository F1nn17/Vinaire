package api

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/shiraku/vinaire/backend/internal/authz"
	"github.com/shiraku/vinaire/backend/internal/k8s"
)

// This file holds the HTTP handlers for the three write actions Phase 9 /
// AGENTS.md's core rule allow — see internal/k8s/deployment_actions.go for
// the actual client-go calls.

// RestartDeployment handles POST /api/v1/deployments/{namespace}/{name}/restart.
func (h *Handler) RestartDeployment(w http.ResponseWriter, r *http.Request) {
	cluster, ok := h.resolveCluster(w, r)
	if !ok {
		return
	}
	namespace := chi.URLParam(r, "namespace")
	name := chi.URLParam(r, "name")
	if _, _, ok := h.authorize(w, r, namespace, "write"); !ok {
		return
	}

	if err := k8s.RestartDeployment(r.Context(), cluster.Clientset, namespace, name); err != nil {
		respondError(w, http.StatusBadGateway, err.Error())
		return
	}

	auditWriteAction(r, "restart", namespace, name)
	respondJSON(w, map[string]string{"status": "ok"})
}

// DeleteDeployment handles DELETE /api/v1/deployments/{namespace}/{name}.
func (h *Handler) DeleteDeployment(w http.ResponseWriter, r *http.Request) {
	cluster, ok := h.resolveCluster(w, r)
	if !ok {
		return
	}
	namespace := chi.URLParam(r, "namespace")
	name := chi.URLParam(r, "name")
	if _, _, ok := h.authorize(w, r, namespace, "write"); !ok {
		return
	}

	if err := k8s.DeleteDeployment(r.Context(), cluster.Clientset, namespace, name); err != nil {
		respondError(w, http.StatusBadGateway, err.Error())
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
	cluster, ok := h.resolveCluster(w, r)
	if !ok {
		return
	}
	namespace := chi.URLParam(r, "namespace")
	name := chi.URLParam(r, "name")
	if _, _, ok := h.authorize(w, r, namespace, "write"); !ok {
		return
	}

	var req rollbackRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Revision <= 0 {
		respondError(w, http.StatusBadRequest, `request body must be {"revision": <positive integer>}`)
		return
	}

	if err := k8s.RollbackDeployment(r.Context(), cluster.Clientset, namespace, name, req.Revision); err != nil {
		respondError(w, http.StatusBadGateway, err.Error())
		return
	}

	auditWriteAction(r, "rollback", namespace, name, "revision", req.Revision)
	respondJSON(w, map[string]string{"status": "ok"})
}

// auditWriteAction logs every write action Phase 9 allows — see AGENTS.md's
// core rule. "who" is the caller's verified OIDC identity (Roadmap
// Phase 10) plus their source address; extra key/value pairs add
// action-specific context (e.g. rollback's target revision).
func auditWriteAction(r *http.Request, action, namespace, name string, extra ...any) {
	claims, _ := authz.FromContext(r.Context())
	args := []any{"action", action, "namespace", namespace, "name", name, "user", claims.Username, "remote_addr", r.RemoteAddr}
	args = append(args, extra...)
	slog.Warn("write action", args...)
}
