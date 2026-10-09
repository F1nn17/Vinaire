package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"k8s.io/client-go/kubernetes"

	"github.com/shiraku/vinaire/backend/internal/k8s"
)

// deleteWorkload builds the handler of DELETE /api/v1/{statefulsets,daemonsets,jobs}/{namespace}/{name}:
// a plain Delete as the caller (the cluster's RBAC decides), audited as `action`. These are the
// writes the Workloads lists' multi-select and the pages' Delete button use on those kinds.
func (h *Handler) deleteWorkload(action string, del func(r *http.Request, cs *kubernetes.Clientset, namespace, name string) error) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		_, clientset, ok := h.resolveCluster(w, r)
		if !ok {
			return
		}
		namespace := chi.URLParam(r, "namespace")
		name := chi.URLParam(r, "name")
		if !k8sNameRe.MatchString(namespace) || !k8sNameRe.MatchString(name) {
			respondError(w, http.StatusBadRequest, "invalid namespace or name")
			return
		}
		if err := del(r, clientset, namespace, name); err != nil {
			respondK8sError(w, err)
			return
		}
		h.auditLog(r, action, namespace, name)
		respondJSON(w, map[string]string{"status": "ok"})
	}
}

// DeleteStatefulSet handles DELETE /api/v1/statefulsets/{namespace}/{name}.
func (h *Handler) DeleteStatefulSet(w http.ResponseWriter, r *http.Request) {
	h.deleteWorkload("delete-statefulset", func(r *http.Request, cs *kubernetes.Clientset, ns, name string) error {
		return k8s.DeleteStatefulSet(r.Context(), cs, ns, name)
	})(w, r)
}

// DeleteDaemonSet handles DELETE /api/v1/daemonsets/{namespace}/{name}.
func (h *Handler) DeleteDaemonSet(w http.ResponseWriter, r *http.Request) {
	h.deleteWorkload("delete-daemonset", func(r *http.Request, cs *kubernetes.Clientset, ns, name string) error {
		return k8s.DeleteDaemonSet(r.Context(), cs, ns, name)
	})(w, r)
}

// DeleteJob handles DELETE /api/v1/jobs/{namespace}/{name}.
func (h *Handler) DeleteJob(w http.ResponseWriter, r *http.Request) {
	h.deleteWorkload("delete-job", func(r *http.Request, cs *kubernetes.Clientset, ns, name string) error {
		return k8s.DeleteJob(r.Context(), cs, ns, name)
	})(w, r)
}
