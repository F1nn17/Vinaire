package api

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"sort"
	"strconv"
	"time"

	corev1 "k8s.io/api/core/v1"
	apiextensionsclientset "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	metricsclientset "k8s.io/metrics/pkg/client/clientset/versioned"

	"github.com/f1nn17/vinaire/backend/internal/k8s"
)

// dashboardSampleLimit caps how many example pod/application names ride
// along with a count (e.g. "payments-api, kafka-broker-0"), matching the
// original mock's style without dumping a potentially huge list.
const dashboardSampleLimit = 3

// defaultEventsLimit is how many recent events Events returns when the
// caller doesn't specify `?limit=`.
const defaultEventsLimit = 20

// NamespacePodCount matches the frontend's NamespacePodCount type.
type NamespacePodCount struct {
	Name string `json:"name"`
	Pods int    `json:"pods"`
}

// DashboardSummaryResponse matches the frontend's DashboardSummary type.
// PVC-near-capacity deliberately isn't included here — it stays an
// explicit frontend-side "unavailable" stub at this stage (see this
// phase's own scope note in ROADMAP.md): Phase 13 built the real data
// source, but wiring it into the Dashboard isn't part of this phase.
type DashboardSummaryResponse struct {
	PodsDown        int                 `json:"podsDown"`
	PodsDownSample  []string            `json:"podsDownSample"`
	HealthyPods     int                 `json:"healthyPods"`
	Namespaces      []NamespacePodCount `json:"namespaces"`
	ArgoCDAvailable bool                `json:"argoCDAvailable"`
	DriftedCount    int                 `json:"driftedCount"`
	DriftedSample   []string            `json:"driftedSample"`
	// CPU/Memory are nil when metrics-server isn't installed/reachable —
	// the same degrade-gracefully rule pod_detail.go's podResourceUsage
	// already follows, applied cluster-wide via Node capacity + NodeMetricses
	// instead of one pod's limits + PodMetricses.
	CPU    *ResourceUsage `json:"cpu,omitempty"`
	Memory *ResourceUsage `json:"memory,omitempty"`
	// Disk is the nodes' root disks added together: used against size when Prometheus has the
	// node-exporter's filesystem series, else only the size (what the nodes report as ephemeral
	// storage) with Used empty. nil when neither is known.
	Disk *ResourceUsage `json:"disk,omitempty"`
	// PVC is the "PVC near capacity" card (Roadmap Phase 90); nil when the claims can't be listed.
	PVC *PVCCapacitySummary `json:"pvc,omitempty"`
}

// DashboardSummary handles GET /api/v1/dashboard/summary — aggregates
// pods-down/healthy-pods counts and per-namespace pod counts from the same
// pod list Phase 1 already fetches (no new Kubernetes API surface for
// that), plus an ArgoCD Application-drift check.
func (h *Handler) DashboardSummary(w http.ResponseWriter, r *http.Request) {
	cluster, clientset, ok := h.resolveCluster(w, r)
	if !ok {
		return
	}

	pods, err := k8s.ListPods(r.Context(), clientset, "")
	if err != nil {
		respondK8sError(w, err)
		return
	}

	podsDown := 0
	podsDownSample := make([]string, 0, dashboardSampleLimit)
	nsCounts := make(map[string]int)
	for _, p := range pods {
		nsCounts[p.Namespace]++
		if _, color := podStatus(p); color != StatusOK {
			podsDown++
			if len(podsDownSample) < dashboardSampleLimit {
				podsDownSample = append(podsDownSample, p.Name)
			}
		}
	}

	namespaces := make([]NamespacePodCount, 0, len(nsCounts))
	for ns, count := range nsCounts {
		namespaces = append(namespaces, NamespacePodCount{Name: ns, Pods: count})
	}
	sort.Slice(namespaces, func(i, j int) bool { return namespaces[i].Name < namespaces[j].Name })

	resp := DashboardSummaryResponse{
		PodsDown:       podsDown,
		PodsDownSample: podsDownSample,
		HealthyPods:    len(pods) - podsDown,
		Namespaces:     namespaces,
	}
	resp.CPU, resp.Memory = h.clusterResourceUsage(r.Context(), clientset, h.metricsFor(r, cluster))
	resp.Disk = h.clusterDiskUsage(r.Context(), cluster.Name, clientset)
	resp.PVC = h.clusterPVCCapacity(r.Context(), cluster.Name, clientset)

	// A discarding ResponseWriter: resolveDynamic writes its own HTTP error
	// on failure, but an ArgoCD-detection hiccup should only make this one
	// field "unavailable," never fail the whole dashboard summary.
	if dyn, apiext, ok := h.resolveDynamic(&discardResponseWriter{}, r); ok {
		resp.ArgoCDAvailable, resp.DriftedCount, resp.DriftedSample = argoCDDrift(r.Context(), dyn, apiext)
	}

	respondJSON(w, resp)
}

// clusterResourceUsage reports current cluster-wide CPU/memory usage (via
// metrics-server, if available) against every Node's allocatable capacity —
// the same "degrade to nil rather than fail the request" rule
// podResourceUsage (pod_detail.go) follows for a single pod, applied
// cluster-wide. Returns (nil, nil) if metrics-server isn't installed, isn't
// reachable, or Nodes/NodeMetricses can't be listed.
func (h *Handler) clusterResourceUsage(ctx context.Context, clientset *kubernetes.Clientset, metricsClientset *metricsclientset.Clientset) (cpu, memory *ResourceUsage) {
	if metricsClientset == nil {
		return nil, nil
	}

	nodes, err := clientset.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if err != nil {
		slog.Warn("cluster node list unavailable for dashboard metrics", "error", err)
		return nil, nil
	}

	cpuCapacity := resource.Quantity{}
	memCapacity := resource.Quantity{}
	for _, n := range nodes.Items {
		if q, ok := n.Status.Allocatable[corev1.ResourceCPU]; ok {
			cpuCapacity.Add(q)
		}
		if q, ok := n.Status.Allocatable[corev1.ResourceMemory]; ok {
			memCapacity.Add(q)
		}
	}

	nodeMetrics, err := metricsClientset.MetricsV1beta1().NodeMetricses().List(ctx, metav1.ListOptions{})
	if err != nil {
		slog.Warn("cluster node metrics unavailable for dashboard metrics", "error", err)
		return nil, nil
	}

	var cpuUsedMilli, memUsedBytes int64
	for _, m := range nodeMetrics.Items {
		if q, ok := m.Usage[corev1.ResourceCPU]; ok {
			cpuUsedMilli += q.MilliValue()
		}
		if q, ok := m.Usage[corev1.ResourceMemory]; ok {
			memUsedBytes += q.Value()
		}
	}

	cpu = &ResourceUsage{Used: fmt.Sprintf("%dm", cpuUsedMilli), Limit: formatQuantity(cpuCapacity, formatMilli)}
	memory = &ResourceUsage{Used: fmt.Sprintf("%dMi", memUsedBytes/(1024*1024)), Limit: formatQuantity(memCapacity, formatMebibytes)}

	if capMilli := cpuCapacity.MilliValue(); capMilli > 0 {
		cpu.Pct = int(cpuUsedMilli * 100 / capMilli)
	}
	if capBytes := memCapacity.Value(); capBytes > 0 {
		memory.Pct = int(memUsedBytes * 100 / capBytes)
	}
	return cpu, memory
}

var argoCDApplicationGVR = schema.GroupVersionResource{
	Group:    "argoproj.io",
	Version:  "v1alpha1",
	Resource: "applications",
}

const argoCDApplicationCRDName = "applications.argoproj.io"

// argoCDDrift checks whether ArgoCD's applications.argoproj.io CRD is
// installed and, if so, counts Applications reporting sync status
// "OutOfSync" — the same generic dynamic-client pattern Phase 15's CRD
// viewer uses, applied here to one specific, well-known CRD instead of a
// user-selected one.
func argoCDDrift(ctx context.Context, dyn dynamic.Interface, apiext *apiextensionsclientset.Clientset) (available bool, driftedCount int, sample []string) {
	sample = make([]string, 0, dashboardSampleLimit)

	if _, err := k8s.GetCRD(ctx, apiext, argoCDApplicationCRDName); err != nil {
		return false, 0, sample
	}

	list, err := dyn.Resource(argoCDApplicationGVR).List(ctx, metav1.ListOptions{})
	if err != nil {
		// The CRD exists but instances can't be listed (e.g. RBAC) — still
		// "available" (ArgoCD is installed), just can't report a count.
		return true, 0, sample
	}

	for _, item := range list.Items {
		status, found, _ := unstructured.NestedString(item.Object, "status", "sync", "status")
		if found && status == "OutOfSync" {
			driftedCount++
			if len(sample) < dashboardSampleLimit {
				sample = append(sample, item.GetName())
			}
		}
	}
	return true, driftedCount, sample
}

// discardResponseWriter swallows whatever gets written to it — used when a
// caller wants to treat a resolveDynamic/resolveCluster-style failure as
// "this specific sub-feature is unavailable," not abort its own response.
type discardResponseWriter struct{ header http.Header }

func (d *discardResponseWriter) Header() http.Header {
	if d.header == nil {
		d.header = http.Header{}
	}
	return d.header
}

func (d *discardResponseWriter) Write(b []byte) (int, error) { return len(b), nil }

func (d *discardResponseWriter) WriteHeader(int) {}

// Events handles GET /api/v1/events?namespace=&limit= — a general recent-
// events feed (cluster-wide, or namespace-scoped), sorted newest first and
// capped at limit (default 20). Distinct from Phase 3's per-object events
// query used internally by detail handlers (ListEventsForObject).
func (h *Handler) Events(w http.ResponseWriter, r *http.Request) {
	_, clientset, ok := h.resolveCluster(w, r)
	if !ok {
		return
	}

	namespace := r.URL.Query().Get("namespace")
	limit := defaultEventsLimit
	if raw := r.URL.Query().Get("limit"); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil && parsed > 0 {
			limit = parsed
		}
	}

	events, err := k8s.ListEvents(r.Context(), clientset, namespace)
	if err != nil {
		respondK8sError(w, err)
		return
	}

	sortEventsNewestFirst(events)
	if len(events) > limit {
		events = events[:limit]
	}

	rows := make([]PodEventRow, 0, len(events))
	for _, e := range events {
		rows = append(rows, podEventRow(e))
	}
	respondJSON(w, rows)
}

// diskFilter keeps the real block devices: a disk is counted once per node however many places it is
// mounted (a node and its kubelet directories, a container runtime's bind mounts all sit on the same
// one), and memory-backed, overlay and loop filesystems are left out.
const diskFilter = `{device=~"/dev/.+",device!~"/dev/loop.*",fstype!~"tmpfs|overlay|squashfs|ramfs|nsfs|devtmpfs"}`

func diskSum(metric string) string {
	return "sum(max by (instance, device) (" + metric + diskFilter + "))"
}

// clusterDiskUsage adds up the nodes' root disks: used and size from Prometheus (the node-exporter),
// or, without it, only the size the nodes report as ephemeral storage. It never fails the dashboard:
// a missing Prometheus just means no "used".
func (h *Handler) clusterDiskUsage(ctx context.Context, cluster string, cs *kubernetes.Clientset) *ResourceUsage {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if fetch, _ := h.prometheusFetcher(ctx, cluster, cs); fetch != nil {
		size, okSize := promInstant(ctx, fetch, diskSum("node_filesystem_size_bytes"))
		avail, okAvail := promInstant(ctx, fetch, diskSum("node_filesystem_avail_bytes"))
		if okSize && okAvail && size > 0 {
			used := size - avail
			if used < 0 {
				used = 0
			}
			return &ResourceUsage{Used: formatGiB(used), Limit: formatGiB(size), Pct: int(used * 100 / size)}
		}
	}
	nodes, err := cs.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil
	}
	var total int64
	for _, n := range nodes.Items {
		if q, ok := n.Status.Capacity[corev1.ResourceEphemeralStorage]; ok {
			total += q.Value()
		}
	}
	if total == 0 {
		return nil
	}
	return &ResourceUsage{Limit: formatGiB(float64(total))}
}

func formatGiB(b float64) string { return fmt.Sprintf("%.1f GiB", b/(1<<30)) }
