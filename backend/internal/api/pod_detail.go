package api

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"sort"
	"time"

	"github.com/go-chi/chi/v5"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	metricsv1beta1 "k8s.io/metrics/pkg/apis/metrics/v1beta1"
	metricsclientset "k8s.io/metrics/pkg/client/clientset/versioned"

	"github.com/shiraku/vinaire/backend/internal/k8s"
)

// ResourceUsage matches the frontend's `{ used, limit, pct }` shape used for
// both cpu and memory on PodDetail.
type ResourceUsage struct {
	Used  string `json:"used"`
	Limit string `json:"limit"`
	Pct   int    `json:"pct"`
}

// PodContainerSummary matches the frontend's ContainerSummary type.
type PodContainerSummary struct {
	Name        string `json:"name"`
	StatusColor string `json:"statusColor"`
	Restarts    int32  `json:"restarts"`
}

// PodEventRow matches the frontend's ClusterEvent type.
type PodEventRow struct {
	StatusColor string `json:"statusColor"`
	Title       string `json:"title"`
	Meta        string `json:"meta"`
	Time        string `json:"time"`
}

// PodDetailResponse matches the frontend's PodDetail type
// (frontend/src/lib/types.ts). Logs aren't part of this response — the Logs
// tab is a separate live WebSocket stream (see internal/stream.ServeLogs,
// Roadmap Phase 4), not a one-shot fetch.
type PodDetailResponse struct {
	Name        string                `json:"name"`
	Namespace   string                `json:"namespace"`
	Status      string                `json:"status"`
	StatusColor string                `json:"statusColor"`
	Node        string                `json:"node"`
	PodIP       string                `json:"podIp"`
	QOSClass    string                `json:"qosClass"`
	Restarts    int32                 `json:"restarts"`
	Image       string                `json:"image"`
	Age         string                `json:"age"`
	Labels      map[string]string     `json:"labels"`
	CPU         ResourceUsage         `json:"cpu"`
	Memory      ResourceUsage         `json:"memory"`
	Containers  []PodContainerSummary `json:"containers"`
	Events      []PodEventRow         `json:"events"`
	YAML        string                `json:"yaml"`
}

// PodDetail handles GET /api/v1/pods/{namespace}/{name}.
func (h *Handler) PodDetail(w http.ResponseWriter, r *http.Request) {
	cluster, ok := h.resolveCluster(w, r)
	if !ok {
		return
	}

	namespace := chi.URLParam(r, "namespace")
	name := chi.URLParam(r, "name")
	if _, _, ok := h.authorize(w, r, namespace, "read"); !ok {
		return
	}

	pod, err := k8s.GetPod(r.Context(), cluster.Clientset, namespace, name)
	if err != nil {
		respondError(w, http.StatusNotFound, err.Error())
		return
	}

	events, err := k8s.ListEventsForObject(r.Context(), cluster.Clientset, namespace, name)
	if err != nil {
		// Non-fatal: still show pod metadata even if events can't be listed.
		slog.Warn("list events failed", "namespace", namespace, "name", name, "error", err)
		events = nil
	}

	respondJSON(w, h.buildPodDetail(r.Context(), cluster.MetricsClientset, *pod, events))
}

func (h *Handler) buildPodDetail(ctx context.Context, metricsClientset *metricsclientset.Clientset, pod corev1.Pod, events []corev1.Event) PodDetailResponse {
	status, color := podStatus(pod)

	image := ""
	if len(pod.Spec.Containers) > 0 {
		image = pod.Spec.Containers[0].Image
	}

	labels := pod.Labels
	if labels == nil {
		labels = map[string]string{}
	}

	cpu, memory := h.podResourceUsage(ctx, metricsClientset, pod)

	sortEventsNewestFirst(events)
	eventRows := make([]PodEventRow, 0, len(events))
	for _, e := range events {
		eventRows = append(eventRows, podEventRow(e))
	}

	pod.TypeMeta = metav1.TypeMeta{Kind: "Pod", APIVersion: "v1"}
	yaml := marshalYAML(pod)

	return PodDetailResponse{
		Name:        pod.Name,
		Namespace:   pod.Namespace,
		Status:      status,
		StatusColor: string(color),
		Node:        pod.Spec.NodeName,
		PodIP:       pod.Status.PodIP,
		QOSClass:    string(pod.Status.QOSClass),
		Restarts:    podRestarts(pod),
		Image:       image,
		Age:         formatAge(pod.CreationTimestamp.Time),
		Labels:      labels,
		CPU:         cpu,
		Memory:      memory,
		Containers:  containerSummaries(pod),
		Events:      eventRows,
		YAML:        yaml,
	}
}

func containerSummaries(pod corev1.Pod) []PodContainerSummary {
	summaries := make([]PodContainerSummary, 0, len(pod.Spec.Containers))
	statusByName := make(map[string]corev1.ContainerStatus, len(pod.Status.ContainerStatuses))
	for _, cs := range pod.Status.ContainerStatuses {
		statusByName[cs.Name] = cs
	}

	for _, c := range pod.Spec.Containers {
		cs, ok := statusByName[c.Name]
		if !ok {
			summaries = append(summaries, PodContainerSummary{Name: c.Name, StatusColor: string(StatusWarn)})
			continue
		}
		summaries = append(summaries, PodContainerSummary{
			Name:        c.Name,
			StatusColor: string(containerStatusColor(cs)),
			Restarts:    cs.RestartCount,
		})
	}
	return summaries
}

func containerStatusColor(cs corev1.ContainerStatus) StatusColor {
	switch {
	case cs.State.Waiting != nil:
		return waitingColor(cs.State.Waiting.Reason)
	case cs.State.Terminated != nil:
		if cs.State.Terminated.Reason == "Completed" {
			return StatusOK
		}
		return StatusErr
	case cs.Ready:
		return StatusOK
	default:
		return StatusWarn
	}
}

// podResourceUsage reports current CPU/memory usage (via metrics-server, if
// available) against the pod's own resource limits. If metrics-server isn't
// installed, or hasn't scraped this pod yet, Used degrades to "—" rather
// than failing the whole request — metrics are a nice-to-have overlay on
// metadata that's already known to be correct.
func (h *Handler) podResourceUsage(ctx context.Context, metricsClientset *metricsclientset.Clientset, pod corev1.Pod) (cpu, memory ResourceUsage) {
	cpuLimit := sumResourceLimits(pod, corev1.ResourceCPU)
	memLimit := sumResourceLimits(pod, corev1.ResourceMemory)

	cpu = ResourceUsage{Used: "—", Limit: formatQuantity(cpuLimit, formatMilli)}
	memory = ResourceUsage{Used: "—", Limit: formatQuantity(memLimit, formatMebibytes)}

	if metricsClientset == nil {
		return cpu, memory
	}

	metrics, err := metricsClientset.MetricsV1beta1().PodMetricses(pod.Namespace).Get(ctx, pod.Name, metav1.GetOptions{})
	if err != nil {
		slog.Warn("pod metrics unavailable", "namespace", pod.Namespace, "name", pod.Name, "error", err)
		return cpu, memory
	}

	cpuUsedMilli, memUsedBytes := sumContainerUsage(metrics.Containers)

	cpu.Used = fmt.Sprintf("%dm", cpuUsedMilli)
	memory.Used = fmt.Sprintf("%dMi", memUsedBytes/(1024*1024))

	if limitMilli := cpuLimit.MilliValue(); limitMilli > 0 {
		cpu.Pct = int(cpuUsedMilli * 100 / limitMilli)
	}
	if limitBytes := memLimit.Value(); limitBytes > 0 {
		memory.Pct = int(memUsedBytes * 100 / limitBytes)
	}

	return cpu, memory
}

func sumResourceLimits(pod corev1.Pod, name corev1.ResourceName) resource.Quantity {
	total := resource.Quantity{}
	for _, c := range pod.Spec.Containers {
		if q, ok := c.Resources.Limits[name]; ok {
			total.Add(q)
		}
	}
	return total
}

func sumContainerUsage(containers []metricsv1beta1.ContainerMetrics) (cpuMilli, memBytes int64) {
	for _, c := range containers {
		if q, ok := c.Usage[corev1.ResourceCPU]; ok {
			cpuMilli += q.MilliValue()
		}
		if q, ok := c.Usage[corev1.ResourceMemory]; ok {
			memBytes += q.Value()
		}
	}
	return cpuMilli, memBytes
}

func formatMilli(q resource.Quantity) string     { return fmt.Sprintf("%dm", q.MilliValue()) }
func formatMebibytes(q resource.Quantity) string { return fmt.Sprintf("%dMi", q.Value()/(1024*1024)) }

func formatQuantity(q resource.Quantity, format func(resource.Quantity) string) string {
	if q.IsZero() {
		return "no limit"
	}
	return format(q)
}

func eventTime(e corev1.Event) time.Time {
	if !e.LastTimestamp.IsZero() {
		return e.LastTimestamp.Time
	}
	return e.EventTime.Time
}

func sortEventsNewestFirst(events []corev1.Event) {
	sort.Slice(events, func(i, j int) bool {
		return eventTime(events[i]).After(eventTime(events[j]))
	})
}

func podEventRow(e corev1.Event) PodEventRow {
	color := StatusOK
	if e.Type == corev1.EventTypeWarning {
		color = StatusWarn
	}

	meta := e.Source.Component
	if e.Source.Host != "" {
		meta = fmt.Sprintf("%s · %s", e.Source.Component, e.Source.Host)
	}

	return PodEventRow{
		StatusColor: string(color),
		Title:       e.Message,
		Meta:        meta,
		Time:        formatAge(eventTime(e)) + " ago",
	}
}
