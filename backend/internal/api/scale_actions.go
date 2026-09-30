package api

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/shiraku/vinaire/backend/internal/k8s"
)

// This file holds Roadmap Phase 33's write actions — Scale (Deployment/
// StatefulSet), Pause/Resume rollout (Deployment), and pod-level Delete.
// Same authorization model as Phase 9's three actions and Phase 24's
// cluster management: the caller's own Kubernetes RBAC decides what's
// actually allowed, not a check here — an unauthorized attempt gets a
// real 403 back from the Kubernetes API itself.

type scaleRequest struct {
	Replicas int32 `json:"replicas"`
}

func decodeScaleRequest(w http.ResponseWriter, r *http.Request) (int32, bool) {
	var req scaleRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Replicas < 0 {
		respondError(w, http.StatusBadRequest, `request body must be {"replicas": <non-negative integer>}`)
		return 0, false
	}
	return req.Replicas, true
}

// ScaleDeployment handles POST /api/v1/deployments/{namespace}/{name}/scale.
func (h *Handler) ScaleDeployment(w http.ResponseWriter, r *http.Request) {
	_, clientset, ok := h.resolveCluster(w, r)
	if !ok {
		return
	}
	namespace := chi.URLParam(r, "namespace")
	name := chi.URLParam(r, "name")

	replicas, ok := decodeScaleRequest(w, r)
	if !ok {
		return
	}

	if err := k8s.ScaleDeployment(r.Context(), clientset, namespace, name, replicas); err != nil {
		respondK8sError(w, err)
		return
	}

	h.auditLog(r, "scale", namespace, name, "replicas", replicas)
	respondJSON(w, map[string]string{"status": "ok"})
}

// ScaleStatefulSet handles POST /api/v1/statefulsets/{namespace}/{name}/scale.
func (h *Handler) ScaleStatefulSet(w http.ResponseWriter, r *http.Request) {
	_, clientset, ok := h.resolveCluster(w, r)
	if !ok {
		return
	}
	namespace := chi.URLParam(r, "namespace")
	name := chi.URLParam(r, "name")

	replicas, ok := decodeScaleRequest(w, r)
	if !ok {
		return
	}

	if err := k8s.ScaleStatefulSet(r.Context(), clientset, namespace, name, replicas); err != nil {
		respondK8sError(w, err)
		return
	}

	h.auditLog(r, "scale", namespace, name, "replicas", replicas)
	respondJSON(w, map[string]string{"status": "ok"})
}

// PauseDeploymentRollout handles POST /api/v1/deployments/{namespace}/{name}/pause.
func (h *Handler) PauseDeploymentRollout(w http.ResponseWriter, r *http.Request) {
	_, clientset, ok := h.resolveCluster(w, r)
	if !ok {
		return
	}
	namespace := chi.URLParam(r, "namespace")
	name := chi.URLParam(r, "name")

	if err := k8s.PauseDeploymentRollout(r.Context(), clientset, namespace, name); err != nil {
		respondK8sError(w, err)
		return
	}

	h.auditLog(r, "pause-rollout", namespace, name)
	respondJSON(w, map[string]string{"status": "ok"})
}

// ResumeDeploymentRollout handles POST /api/v1/deployments/{namespace}/{name}/resume.
func (h *Handler) ResumeDeploymentRollout(w http.ResponseWriter, r *http.Request) {
	_, clientset, ok := h.resolveCluster(w, r)
	if !ok {
		return
	}
	namespace := chi.URLParam(r, "namespace")
	name := chi.URLParam(r, "name")

	if err := k8s.ResumeDeploymentRollout(r.Context(), clientset, namespace, name); err != nil {
		respondK8sError(w, err)
		return
	}

	h.auditLog(r, "resume-rollout", namespace, name)
	respondJSON(w, map[string]string{"status": "ok"})
}

// DeletePod handles DELETE /api/v1/pods/{namespace}/{name}.
func (h *Handler) DeletePod(w http.ResponseWriter, r *http.Request) {
	_, clientset, ok := h.resolveCluster(w, r)
	if !ok {
		return
	}
	namespace := chi.URLParam(r, "namespace")
	name := chi.URLParam(r, "name")

	if err := k8s.DeletePod(r.Context(), clientset, namespace, name); err != nil {
		respondK8sError(w, err)
		return
	}

	h.auditLog(r, "delete-pod", namespace, name)
	respondJSON(w, map[string]string{"status": "ok"})
}
