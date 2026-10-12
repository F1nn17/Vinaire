package api

import (
	"context"
	"log/slog"
	"net/http"
	"sort"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	metricsclientset "k8s.io/metrics/pkg/client/clientset/versioned"

	"github.com/f1nn17/vinaire/backend/internal/k8s"
)

// The "Requests & Limits" view: for the whole cluster, each node and each
// namespace, how much CPU / memory / ephemeral storage / how many pods are
// *requested*, *limited* and (when metrics-server is there) actually *used*, next
// to what the node(s) can allocate. Requests are what the scheduler reserves,
// limits are the ceiling a container may burst to, usage is what it uses right
// now — three different things, so they are three different figures here.
//
// Everything is read as the caller (their pods, their nodes, their metrics): a
// namespace they can't list doesn't appear, because Kubernetes doesn't return it.
//
// Quantities are raw numbers (millicores, bytes, counts) so the UI draws the
// charts and formats the units; nothing here is pre-rendered text.

// ResourceFigure is one resource of one row. Allocatable is set for the cluster
// and for nodes (what can be scheduled), absent for a namespace (which has no
// capacity of its own). Usage is absent when metrics-server isn't available.
type ResourceFigure struct {
	Allocatable *int64 `json:"allocatable,omitempty"`
	Requests    int64  `json:"requests"`
	Limits      int64  `json:"limits"`
	Usage       *int64 `json:"usage,omitempty"`
	// ContainersWithoutRequests / ContainersWithoutLimits count containers that set
	// none — their reservation / ceiling is unknown, so the sums are a lower bound.
	ContainersWithoutRequests int `json:"containersWithoutRequests,omitempty"`
	ContainersWithoutLimits   int `json:"containersWithoutLimits,omitempty"`
}

// ResourceRow is the cluster, one node or one namespace.
type ResourceRow struct {
	Name           string         `json:"name"`
	CPU            ResourceFigure `json:"cpu"`    // millicores
	Memory         ResourceFigure `json:"memory"` // bytes
	Ephemeral      ResourceFigure `json:"ephemeralStorage"`
	Pods           int64          `json:"pods"`
	PodsAllocation *int64         `json:"podsAllocatable,omitempty"`
	Containers     int            `json:"containers"`
}

// ResourceOverview is GET /api/v1/requests-limits.
type ResourceOverview struct {
	Cluster          ResourceRow   `json:"cluster"`
	Nodes            []ResourceRow `json:"nodes"`
	Namespaces       []ResourceRow `json:"namespaces"`
	MetricsAvailable bool          `json:"metricsAvailable"`
}

func addPtr(p *int64, v int64) *int64 {
	if p == nil {
		x := v
		return &x
	}
	*p += v
	return p
}

// accumulate adds a pod's containers to a row.
func (row *ResourceRow) accumulate(p *corev1.Pod) {
	row.Pods++
	for _, c := range p.Spec.Containers {
		row.Containers++
		addResource(&row.CPU, c.Resources.Requests, c.Resources.Limits, corev1.ResourceCPU, true)
		addResource(&row.Memory, c.Resources.Requests, c.Resources.Limits, corev1.ResourceMemory, false)
		addResource(&row.Ephemeral, c.Resources.Requests, c.Resources.Limits, corev1.ResourceEphemeralStorage, false)
	}
}

func addResource(f *ResourceFigure, requests, limits corev1.ResourceList, name corev1.ResourceName, milli bool) {
	val := func(q interface {
		MilliValue() int64
		Value() int64
	}) int64 {
		if milli {
			return q.MilliValue()
		}
		return q.Value()
	}
	if q, ok := requests[name]; ok {
		f.Requests += val(&q)
	} else {
		f.ContainersWithoutRequests++
	}
	if q, ok := limits[name]; ok {
		f.Limits += val(&q)
	} else {
		f.ContainersWithoutLimits++
	}
}

func (row *ResourceRow) merge(o *ResourceRow) {
	row.Pods += o.Pods
	row.Containers += o.Containers
	for _, pair := range [][2]*ResourceFigure{{&row.CPU, &o.CPU}, {&row.Memory, &o.Memory}, {&row.Ephemeral, &o.Ephemeral}} {
		dst, src := pair[0], pair[1]
		dst.Requests += src.Requests
		dst.Limits += src.Limits
		dst.ContainersWithoutRequests += src.ContainersWithoutRequests
		dst.ContainersWithoutLimits += src.ContainersWithoutLimits
	}
}

// RequestsLimits handles GET /api/v1/requests-limits.
func (h *Handler) RequestsLimits(w http.ResponseWriter, r *http.Request) {
	cluster, clientset, ok := h.resolveCluster(w, r)
	if !ok {
		return
	}
	ctx := r.Context()
	nodes, err := k8s.ListNodes(ctx, clientset)
	if err != nil {
		respondK8sError(w, err)
		return
	}
	pods, err := k8s.ListPods(ctx, clientset, "")
	if err != nil {
		respondK8sError(w, err)
		return
	}

	out := ResourceOverview{Nodes: []ResourceRow{}, Namespaces: []ResourceRow{}}
	byNode := map[string]*ResourceRow{}
	cl := ResourceRow{Name: "cluster"}
	for i := range nodes {
		n := &nodes[i]
		row := &ResourceRow{Name: n.Name}
		cpu, mem := n.Status.Allocatable.Cpu().MilliValue(), n.Status.Allocatable.Memory().Value()
		row.CPU.Allocatable, row.Memory.Allocatable = &cpu, &mem
		if q, ok := n.Status.Allocatable[corev1.ResourceEphemeralStorage]; ok {
			v := q.Value()
			row.Ephemeral.Allocatable = &v
			cl.Ephemeral.Allocatable = addPtr(cl.Ephemeral.Allocatable, v)
		}
		podsAlloc := n.Status.Allocatable.Pods().Value()
		row.PodsAllocation = &podsAlloc
		cl.CPU.Allocatable = addPtr(cl.CPU.Allocatable, cpu)
		cl.Memory.Allocatable = addPtr(cl.Memory.Allocatable, mem)
		cl.PodsAllocation = addPtr(cl.PodsAllocation, podsAlloc)
		byNode[n.Name] = row
	}

	byNS := map[string]*ResourceRow{}
	for i := range pods {
		p := &pods[i]
		// the same scope `kubectl describe node` uses: a finished pod holds nothing
		if p.Status.Phase == corev1.PodSucceeded || p.Status.Phase == corev1.PodFailed {
			continue
		}
		ns := byNS[p.Namespace]
		if ns == nil {
			ns = &ResourceRow{Name: p.Namespace}
			byNS[p.Namespace] = ns
		}
		ns.accumulate(p)
		if nd := byNode[p.Spec.NodeName]; nd != nil {
			nd.accumulate(p)
		}
		var one ResourceRow
		one.accumulate(p)
		cl.merge(&one)
	}

	// live usage, if metrics-server answers (as the caller)
	if mc := h.metricsFor(r, cluster); mc != nil {
		out.MetricsAvailable = fillUsage(ctx, mc, &cl, byNode, byNS)
	}

	for _, n := range nodes {
		out.Nodes = append(out.Nodes, *byNode[n.Name])
	}
	for _, ns := range byNS {
		out.Namespaces = append(out.Namespaces, *ns)
	}
	sort.Slice(out.Namespaces, func(i, j int) bool { return out.Namespaces[i].Name < out.Namespaces[j].Name })
	out.Cluster = cl
	respondJSON(w, out)
}

// fillUsage adds metrics-server's current usage to the rows. It reports whether
// the metrics were available at all.
func fillUsage(ctx context.Context, mc *metricsclientset.Clientset, cl *ResourceRow, byNode, byNS map[string]*ResourceRow) bool {
	nodeMetrics, err := mc.MetricsV1beta1().NodeMetricses().List(ctx, metav1.ListOptions{})
	if err != nil {
		slog.Warn("node metrics unavailable for requests & limits", "error", err)
		return false
	}
	for _, m := range nodeMetrics.Items {
		cpu, mem := m.Usage.Cpu().MilliValue(), m.Usage.Memory().Value()
		if row := byNode[m.Name]; row != nil {
			row.CPU.Usage, row.Memory.Usage = &cpu, &mem
		}
		cl.CPU.Usage = addPtr(cl.CPU.Usage, cpu)
		cl.Memory.Usage = addPtr(cl.Memory.Usage, mem)
	}
	podMetrics, err := mc.MetricsV1beta1().PodMetricses("").List(ctx, metav1.ListOptions{})
	if err != nil {
		slog.Warn("pod metrics unavailable for requests & limits", "error", err)
		return true // nodes and the cluster still have usage
	}
	for _, pm := range podMetrics.Items {
		row := byNS[pm.Namespace]
		if row == nil {
			continue
		}
		for _, c := range pm.Containers {
			row.CPU.Usage = addPtr(row.CPU.Usage, c.Usage.Cpu().MilliValue())
			row.Memory.Usage = addPtr(row.Memory.Usage, c.Usage.Memory().Value())
		}
	}
	return true
}
