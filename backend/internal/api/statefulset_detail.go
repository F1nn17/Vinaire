package api

import (
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/shiraku/vinaire/backend/internal/k8s"
)

// StatefulSetDetailResponse matches the frontend's StatefulSetDetail type
// (frontend/src/lib/types.ts).
type StatefulSetDetailResponse struct {
	Name        string `json:"name"`
	Namespace   string `json:"namespace"`
	Status      string `json:"status"`
	StatusColor string `json:"statusColor"`
	Ready       string `json:"ready"`
	// DesiredReplicas backs Roadmap Phase 33's Scale control.
	DesiredReplicas int32             `json:"desiredReplicas"`
	ServiceName     string            `json:"serviceName"`
	UpdateStrategy  string            `json:"updateStrategy"`
	Image           string            `json:"image"`
	Age             string            `json:"age"`
	Labels          map[string]string `json:"labels"`
	Events          []PodEventRow     `json:"events"`
	YAML            string            `json:"yaml"`
}

// StatefulSetDetail handles GET /api/v1/statefulsets/{namespace}/{name}.
func (h *Handler) StatefulSetDetail(w http.ResponseWriter, r *http.Request) {
	_, clientset, ok := h.resolveCluster(w, r)
	if !ok {
		return
	}

	namespace := chi.URLParam(r, "namespace")
	name := chi.URLParam(r, "name")

	sts, err := k8s.GetStatefulSet(r.Context(), clientset, namespace, name)
	if err != nil {
		respondK8sError(w, err)
		return
	}

	events, err := k8s.ListEventsForObject(r.Context(), clientset, namespace, name)
	if err != nil {
		// Non-fatal: still show statefulset metadata even if events can't be listed.
		slog.Warn("list events failed", "namespace", namespace, "name", name, "error", err)
		events = nil
	}

	respondJSON(w, buildStatefulSetDetail(*sts, events))
}

func buildStatefulSetDetail(s appsv1.StatefulSet, events []corev1.Event) StatefulSetDetailResponse {
	desired := int32(1)
	if s.Spec.Replicas != nil {
		desired = *s.Spec.Replicas
	}
	status, color := readyStatus(s.Status.ReadyReplicas, desired)

	image := ""
	if len(s.Spec.Template.Spec.Containers) > 0 {
		image = s.Spec.Template.Spec.Containers[0].Image
	}

	labels := s.Labels
	if labels == nil {
		labels = map[string]string{}
	}

	sortEventsNewestFirst(events)
	eventRows := make([]PodEventRow, 0, len(events))
	for _, e := range events {
		eventRows = append(eventRows, podEventRow(e))
	}

	s.TypeMeta = metav1.TypeMeta{Kind: "StatefulSet", APIVersion: "apps/v1"}
	yaml := marshalYAML(s)

	return StatefulSetDetailResponse{
		Name:            s.Name,
		Namespace:       s.Namespace,
		Status:          status,
		StatusColor:     string(color),
		Ready:           formatReady(s.Status.ReadyReplicas, desired),
		DesiredReplicas: desired,
		ServiceName:     s.Spec.ServiceName,
		UpdateStrategy:  string(s.Spec.UpdateStrategy.Type),
		Image:           image,
		Age:             formatAge(s.CreationTimestamp.Time),
		Labels:          labels,
		Events:          eventRows,
		YAML:            yaml,
	}
}
