package api

import (
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/f1nn17/vinaire/backend/internal/k8s"
)

// JobDetailResponse matches the frontend's JobDetail type
// (frontend/src/lib/types.ts). Scoped to plain batch/v1 Jobs only — a
// CronJob is a distinct resource with a different status shape and isn't
// covered here (see ROADMAP.md's Phase 12 risk note).
type JobDetailResponse struct {
	Name           string             `json:"name"`
	Namespace      string             `json:"namespace"`
	Status         string             `json:"status"`
	StatusColor    string             `json:"statusColor"`
	Completions    string             `json:"completions"`
	Active         int32              `json:"active"`
	Failed         int32              `json:"failed"`
	StartTime      string             `json:"startTime"`
	CompletionTime string             `json:"completionTime"`
	Image          string             `json:"image"`
	Age            string             `json:"age"`
	Labels         map[string]string  `json:"labels"`
	Pods           []DeploymentPodRow `json:"pods"`
	Events         []PodEventRow      `json:"events"`
	YAML           string             `json:"yaml"`
}

// JobDetail handles GET /api/v1/jobs/{namespace}/{name}.
func (h *Handler) JobDetail(w http.ResponseWriter, r *http.Request) {
	_, clientset, ok := h.resolveCluster(w, r)
	if !ok {
		return
	}

	namespace := chi.URLParam(r, "namespace")
	name := chi.URLParam(r, "name")

	job, err := k8s.GetJob(r.Context(), clientset, namespace, name)
	if err != nil {
		respondK8sError(w, err)
		return
	}

	events, err := k8s.ListEventsForObject(r.Context(), clientset, namespace, name)
	if err != nil {
		// Non-fatal: still show job metadata even if events can't be listed.
		slog.Warn("list events failed", "namespace", namespace, "name", name, "error", err)
		events = nil
	}

	respondJSON(w, buildJobDetail(*job, events, listSelectedPods(r.Context(), clientset, namespace, job.Spec.Selector)))
}

func buildJobDetail(j batchv1.Job, events []corev1.Event, pods *corev1.PodList) JobDetailResponse {
	completions := jobCompletions(j)
	status, color := jobStatus(j, completions)

	image := ""
	if len(j.Spec.Template.Spec.Containers) > 0 {
		image = j.Spec.Template.Spec.Containers[0].Image
	}

	labels := j.Labels
	if labels == nil {
		labels = map[string]string{}
	}

	startTime := ""
	if j.Status.StartTime != nil {
		startTime = formatAge(j.Status.StartTime.Time) + " ago"
	}
	completionTime := ""
	if j.Status.CompletionTime != nil {
		completionTime = formatAge(j.Status.CompletionTime.Time) + " ago"
	}

	sortEventsNewestFirst(events)
	eventRows := make([]PodEventRow, 0, len(events))
	for _, e := range events {
		eventRows = append(eventRows, podEventRow(e))
	}

	j.TypeMeta = metav1.TypeMeta{Kind: "Job", APIVersion: "batch/v1"}
	yaml := marshalYAML(j)

	return JobDetailResponse{
		Name:           j.Name,
		Namespace:      j.Namespace,
		Status:         status,
		StatusColor:    string(color),
		Completions:    formatReady(j.Status.Succeeded, completions),
		Active:         j.Status.Active,
		Failed:         j.Status.Failed,
		StartTime:      startTime,
		CompletionTime: completionTime,
		Image:          image,
		Age:            formatAge(j.CreationTimestamp.Time),
		Labels:         labels,
		Pods:           podRowsFor(pods),
		Events:         eventRows,
		YAML:           yaml,
	}
}
