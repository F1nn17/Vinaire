// Package api holds HTTP handlers that shape internal/k8s's raw listers
// into the JSON responses the frontend expects. Response mapping and
// status/color derivation (business logic) lives here; internal/k8s stays
// a thin, read-only wrapper around the Kubernetes API itself.
package api

import (
	"fmt"
	"net/http"

	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"

	"github.com/shiraku/vinaire/backend/internal/k8s"
)

// WorkloadRow matches the frontend's WorkloadRow type exactly
// (frontend/src/lib/types.ts) — keep the JSON tags in sync with it.
type WorkloadRow struct {
	Name        string `json:"name"`
	Namespace   string `json:"namespace"`
	Ready       string `json:"ready"`
	Status      string `json:"status"`
	StatusColor string `json:"statusColor"`
	Restarts    string `json:"restarts"`
	Age         string `json:"age"`
	Href        string `json:"href"`
}

// Handler holds the dependencies HTTP handlers need. Clientset may be nil
// if the backend booted without a reachable cluster (see main.go) — every
// handler must check requireClientset before using it.
type Handler struct {
	Clientset *kubernetes.Clientset
}

func NewHandler(clientset *kubernetes.Clientset) *Handler {
	return &Handler{Clientset: clientset}
}

func (h *Handler) requireClientset(w http.ResponseWriter) bool {
	if h.Clientset == nil {
		respondError(w, http.StatusServiceUnavailable, "kubernetes client unavailable")
		return false
	}
	return true
}

func (h *Handler) Pods(w http.ResponseWriter, r *http.Request) {
	if !h.requireClientset(w) {
		return
	}

	pods, err := k8s.ListPods(r.Context(), h.Clientset, r.URL.Query().Get("namespace"))
	if err != nil {
		respondError(w, http.StatusBadGateway, err.Error())
		return
	}

	rows := make([]WorkloadRow, 0, len(pods))
	for _, pod := range pods {
		rows = append(rows, PodRow(pod))
	}
	respondJSON(w, rows)
}

// PodRow maps a pod to the frontend's row shape. Exported so internal/stream
// can reuse the exact same mapping for live WebSocket diffs.
func PodRow(pod corev1.Pod) WorkloadRow {
	ready, total := podReadyCount(pod)
	status, color := podStatus(pod)

	return WorkloadRow{
		Name:        pod.Name,
		Namespace:   pod.Namespace,
		Ready:       fmt.Sprintf("%d/%d", ready, total),
		Status:      status,
		StatusColor: string(color),
		Restarts:    fmt.Sprintf("%d", podRestarts(pod)),
		Age:         formatAge(pod.CreationTimestamp.Time),
		Href:        fmt.Sprintf("/workloads/pods/%s/%s", pod.Namespace, pod.Name),
	}
}

func (h *Handler) Deployments(w http.ResponseWriter, r *http.Request) {
	if !h.requireClientset(w) {
		return
	}

	deployments, err := k8s.ListDeployments(r.Context(), h.Clientset, r.URL.Query().Get("namespace"))
	if err != nil {
		respondError(w, http.StatusBadGateway, err.Error())
		return
	}

	rows := make([]WorkloadRow, 0, len(deployments))
	for _, d := range deployments {
		rows = append(rows, DeploymentRow(d))
	}
	respondJSON(w, rows)
}

// DeploymentRow maps a deployment to the frontend's row shape. Exported so
// internal/stream can reuse the exact same mapping for live WebSocket diffs.
func DeploymentRow(d appsv1.Deployment) WorkloadRow {
	desired := int32(1)
	if d.Spec.Replicas != nil {
		desired = *d.Spec.Replicas
	}
	status, color := readyStatus(d.Status.ReadyReplicas, desired)

	return WorkloadRow{
		Name:        d.Name,
		Namespace:   d.Namespace,
		Ready:       fmt.Sprintf("%d/%d", d.Status.ReadyReplicas, desired),
		Status:      status,
		StatusColor: string(color),
		Restarts:    "—",
		Age:         formatAge(d.CreationTimestamp.Time),
		Href:        fmt.Sprintf("/workloads/deployments/%s/%s", d.Namespace, d.Name),
	}
}

func (h *Handler) StatefulSets(w http.ResponseWriter, r *http.Request) {
	if !h.requireClientset(w) {
		return
	}

	sets, err := k8s.ListStatefulSets(r.Context(), h.Clientset, r.URL.Query().Get("namespace"))
	if err != nil {
		respondError(w, http.StatusBadGateway, err.Error())
		return
	}

	rows := make([]WorkloadRow, 0, len(sets))
	for _, s := range sets {
		rows = append(rows, StatefulSetRow(s))
	}
	respondJSON(w, rows)
}

// StatefulSetRow maps a statefulset to the frontend's row shape. Exported so
// internal/stream can reuse the exact same mapping for live WebSocket diffs.
func StatefulSetRow(s appsv1.StatefulSet) WorkloadRow {
	desired := int32(1)
	if s.Spec.Replicas != nil {
		desired = *s.Spec.Replicas
	}
	status, color := readyStatus(s.Status.ReadyReplicas, desired)

	return WorkloadRow{
		Name:        s.Name,
		Namespace:   s.Namespace,
		Ready:       fmt.Sprintf("%d/%d", s.Status.ReadyReplicas, desired),
		Status:      status,
		StatusColor: string(color),
		Restarts:    "—",
		Age:         formatAge(s.CreationTimestamp.Time),
		// No StatefulSet detail screen yet — link back to the list.
		Href: "/workloads",
	}
}

func (h *Handler) DaemonSets(w http.ResponseWriter, r *http.Request) {
	if !h.requireClientset(w) {
		return
	}

	sets, err := k8s.ListDaemonSets(r.Context(), h.Clientset, r.URL.Query().Get("namespace"))
	if err != nil {
		respondError(w, http.StatusBadGateway, err.Error())
		return
	}

	rows := make([]WorkloadRow, 0, len(sets))
	for _, ds := range sets {
		rows = append(rows, DaemonSetRow(ds))
	}
	respondJSON(w, rows)
}

// DaemonSetRow maps a daemonset to the frontend's row shape. Exported so
// internal/stream can reuse the exact same mapping for live WebSocket diffs.
func DaemonSetRow(ds appsv1.DaemonSet) WorkloadRow {
	status, color := readyStatus(ds.Status.NumberReady, ds.Status.DesiredNumberScheduled)

	return WorkloadRow{
		Name:        ds.Name,
		Namespace:   ds.Namespace,
		Ready:       fmt.Sprintf("%d/%d", ds.Status.NumberReady, ds.Status.DesiredNumberScheduled),
		Status:      status,
		StatusColor: string(color),
		Restarts:    "—",
		Age:         formatAge(ds.CreationTimestamp.Time),
		// No DaemonSet detail screen yet — link back to the list.
		Href: "/workloads",
	}
}

func (h *Handler) Jobs(w http.ResponseWriter, r *http.Request) {
	if !h.requireClientset(w) {
		return
	}

	jobs, err := k8s.ListJobs(r.Context(), h.Clientset, r.URL.Query().Get("namespace"))
	if err != nil {
		respondError(w, http.StatusBadGateway, err.Error())
		return
	}

	rows := make([]WorkloadRow, 0, len(jobs))
	for _, j := range jobs {
		rows = append(rows, JobRow(j))
	}
	respondJSON(w, rows)
}

// JobRow maps a job to the frontend's row shape. Exported so internal/stream
// can reuse the exact same mapping for live WebSocket diffs.
func JobRow(j batchv1.Job) WorkloadRow {
	completions := int32(1)
	if j.Spec.Completions != nil {
		completions = *j.Spec.Completions
	}

	var status string
	var color StatusColor
	switch {
	case j.Status.Failed > 0:
		status, color = "Failed", StatusErr
	case j.Status.Succeeded >= completions:
		status, color = "Complete", StatusOK
	default:
		status, color = "Running", StatusWarn
	}

	return WorkloadRow{
		Name:        j.Name,
		Namespace:   j.Namespace,
		Ready:       fmt.Sprintf("%d/%d", j.Status.Succeeded, completions),
		Status:      status,
		StatusColor: string(color),
		Restarts:    "—",
		Age:         formatAge(j.CreationTimestamp.Time),
		// No Job detail screen yet — link back to the list.
		Href: "/workloads",
	}
}
