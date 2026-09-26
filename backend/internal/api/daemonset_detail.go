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

// DaemonSetDetailResponse matches the frontend's DaemonSetDetail type
// (frontend/src/lib/types.ts).
type DaemonSetDetailResponse struct {
	Name           string            `json:"name"`
	Namespace      string            `json:"namespace"`
	Status         string            `json:"status"`
	StatusColor    string            `json:"statusColor"`
	Ready          string            `json:"ready"`
	Desired        int32             `json:"desired"`
	Current        int32             `json:"current"`
	UpdateStrategy string            `json:"updateStrategy"`
	Image          string            `json:"image"`
	Age            string            `json:"age"`
	Labels         map[string]string `json:"labels"`
	Events         []PodEventRow     `json:"events"`
	YAML           string            `json:"yaml"`
}

// DaemonSetDetail handles GET /api/v1/daemonsets/{namespace}/{name}.
func (h *Handler) DaemonSetDetail(w http.ResponseWriter, r *http.Request) {
	_, clientset, ok := h.resolveCluster(w, r)
	if !ok {
		return
	}

	namespace := chi.URLParam(r, "namespace")
	name := chi.URLParam(r, "name")

	ds, err := k8s.GetDaemonSet(r.Context(), clientset, namespace, name)
	if err != nil {
		respondK8sError(w, err)
		return
	}

	events, err := k8s.ListEventsForObject(r.Context(), clientset, namespace, name)
	if err != nil {
		// Non-fatal: still show daemonset metadata even if events can't be listed.
		slog.Warn("list events failed", "namespace", namespace, "name", name, "error", err)
		events = nil
	}

	respondJSON(w, buildDaemonSetDetail(*ds, events))
}

func buildDaemonSetDetail(ds appsv1.DaemonSet, events []corev1.Event) DaemonSetDetailResponse {
	status, color := readyStatus(ds.Status.NumberReady, ds.Status.DesiredNumberScheduled)

	image := ""
	if len(ds.Spec.Template.Spec.Containers) > 0 {
		image = ds.Spec.Template.Spec.Containers[0].Image
	}

	labels := ds.Labels
	if labels == nil {
		labels = map[string]string{}
	}

	sortEventsNewestFirst(events)
	eventRows := make([]PodEventRow, 0, len(events))
	for _, e := range events {
		eventRows = append(eventRows, podEventRow(e))
	}

	ds.TypeMeta = metav1.TypeMeta{Kind: "DaemonSet", APIVersion: "apps/v1"}
	yaml := marshalYAML(ds)

	return DaemonSetDetailResponse{
		Name:           ds.Name,
		Namespace:      ds.Namespace,
		Status:         status,
		StatusColor:    string(color),
		Ready:          formatReady(ds.Status.NumberReady, ds.Status.DesiredNumberScheduled),
		Desired:        ds.Status.DesiredNumberScheduled,
		Current:        ds.Status.CurrentNumberScheduled,
		UpdateStrategy: string(ds.Spec.UpdateStrategy.Type),
		Image:          image,
		Age:            formatAge(ds.CreationTimestamp.Time),
		Labels:         labels,
		Events:         eventRows,
		YAML:           yaml,
	}
}
