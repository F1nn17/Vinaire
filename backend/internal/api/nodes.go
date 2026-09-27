package api

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"sort"
	"strings"

	"github.com/go-chi/chi/v5"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	metricsclientset "k8s.io/metrics/pkg/client/clientset/versioned"

	"github.com/shiraku/vinaire/backend/internal/k8s"
)

// nodeRoleLabelPrefix mirrors kubectl's own convention for deriving a
// Node's "Roles" column: any label of the form
// node-role.kubernetes.io/<role> (value is conventionally ignored/empty).
const nodeRoleLabelPrefix = "node-role.kubernetes.io/"

// NodeRow matches the frontend's NodeRow type — used both for
// GET /api/v1/nodes and as the live stream.Event.Row shape for "nodes"
// events (Roadmap Phase 19). CPU/Memory are nil on the live-streamed
// variant (informer callbacks don't carry a metrics-server snapshot); only
// the REST list handler below fills them in.
type NodeRow struct {
	Name        string         `json:"name"`
	Status      string         `json:"status"`
	StatusColor string         `json:"statusColor"`
	Roles       string         `json:"roles"`
	CPU         *ResourceUsage `json:"cpu,omitempty"`
	Memory      *ResourceUsage `json:"memory,omitempty"`
	Age         string         `json:"age"`
}

// NodeRowOf maps a Node to its list-row shape, with no resource-usage
// data — the pure, cheap mapping stream.StartWorkloadInformers uses for
// every live add/modify/delete event. Exported for that package.
func NodeRowOf(n corev1.Node) NodeRow {
	status, color := nodeStatus(n)
	return NodeRow{
		Name:        n.Name,
		Status:      status,
		StatusColor: string(color),
		Roles:       nodeRoles(n),
		Age:         formatAge(n.CreationTimestamp.Time),
	}
}

// nodeStatus mirrors `kubectl get nodes`' STATUS column: the Ready
// condition, plus a "SchedulingDisabled" suffix when the node is cordoned
// (spec.unschedulable) — this is exactly the field `kubectl cordon` flips,
// which is what Phase 19's definition of done exercises.
func nodeStatus(n corev1.Node) (status string, color StatusColor) {
	ready := false
	for _, c := range n.Status.Conditions {
		if c.Type == corev1.NodeReady {
			ready = c.Status == corev1.ConditionTrue
			break
		}
	}

	if ready {
		status, color = "Ready", StatusOK
	} else {
		status, color = "NotReady", StatusErr
	}

	if n.Spec.Unschedulable {
		status += ",SchedulingDisabled"
		if color == StatusOK {
			color = StatusWarn
		}
	}
	return status, color
}

// nodeRoles joins every node-role.kubernetes.io/<role> label's <role> part,
// the same convention `kubectl get nodes` reads — "<none>" when a node
// carries none (common for plain worker nodes).
func nodeRoles(n corev1.Node) string {
	roles := make([]string, 0, 2)
	for label := range n.Labels {
		if role, ok := strings.CutPrefix(label, nodeRoleLabelPrefix); ok {
			roles = append(roles, role)
		}
	}
	if len(roles) == 0 {
		return "<none>"
	}
	sort.Strings(roles)
	return strings.Join(roles, ",")
}

// Nodes handles GET /api/v1/nodes.
func (h *Handler) Nodes(w http.ResponseWriter, r *http.Request) {
	cluster, clientset, ok := h.resolveCluster(w, r)
	if !ok {
		return
	}

	nodes, err := k8s.ListNodes(r.Context(), clientset)
	if err != nil {
		respondK8sError(w, err)
		return
	}

	rows := make([]NodeRow, 0, len(nodes))
	for _, n := range nodes {
		row := NodeRowOf(n)
		row.CPU, row.Memory = nodeResourceUsage(r.Context(), cluster.MetricsClientset, n)
		rows = append(rows, row)
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].Name < rows[j].Name })

	respondJSON(w, rows)
}

// NodeConditionRow matches the frontend's NodeConditionRow type.
type NodeConditionRow struct {
	Type    string `json:"type"`
	Status  string `json:"status"`
	Reason  string `json:"reason"`
	Message string `json:"message"`
}

// NodeTaintRow matches the frontend's NodeTaintRow type.
type NodeTaintRow struct {
	Key    string `json:"key"`
	Value  string `json:"value"`
	Effect string `json:"effect"`
}

// NodeResources matches the frontend's NodeResources type — raw
// resource.Quantity strings (e.g. "4", "8036804Ki"), the same values
// `kubectl describe node` prints, rather than a reformatted/converted unit
// that could silently misrepresent them.
type NodeResources struct {
	CPU    string `json:"cpu"`
	Memory string `json:"memory"`
	Pods   string `json:"pods"`
}

// NodeScheduledPodRow matches the frontend's NodeScheduledPodRow type — a
// Pod actually running on this node, across any namespace.
type NodeScheduledPodRow struct {
	Name        string `json:"name"`
	Namespace   string `json:"namespace"`
	Ready       string `json:"ready"`
	Status      string `json:"status"`
	StatusColor string `json:"statusColor"`
	Age         string `json:"age"`
}

// NodeDetailResponse matches the frontend's NodeDetail type.
type NodeDetailResponse struct {
	Name           string                `json:"name"`
	Status         string                `json:"status"`
	StatusColor    string                `json:"statusColor"`
	Roles          string                `json:"roles"`
	Age            string                `json:"age"`
	KubeletVersion string                `json:"kubeletVersion"`
	OS             string                `json:"os"`
	Capacity       NodeResources         `json:"capacity"`
	Allocatable    NodeResources         `json:"allocatable"`
	CPU            *ResourceUsage        `json:"cpu,omitempty"`
	Memory         *ResourceUsage        `json:"memory,omitempty"`
	Conditions     []NodeConditionRow    `json:"conditions"`
	Taints         []NodeTaintRow        `json:"taints"`
	Pods           []NodeScheduledPodRow `json:"pods"`
	Labels         map[string]string     `json:"labels"`
	YAML           string                `json:"yaml"`
}

// NodeDetail handles GET /api/v1/nodes/{name}.
func (h *Handler) NodeDetail(w http.ResponseWriter, r *http.Request) {
	cluster, clientset, ok := h.resolveCluster(w, r)
	if !ok {
		return
	}

	name := chi.URLParam(r, "name")

	node, err := k8s.GetNode(r.Context(), clientset, name)
	if err != nil {
		respondK8sError(w, err)
		return
	}

	pods, err := k8s.ListPodsOnNode(r.Context(), clientset, name)
	if err != nil {
		slog.Warn("list pods on node failed", "node", name, "error", err)
		pods = nil
	}

	cpu, memory := nodeResourceUsage(r.Context(), cluster.MetricsClientset, *node)

	respondJSON(w, buildNodeDetail(*node, pods, cpu, memory))
}

func buildNodeDetail(n corev1.Node, pods []corev1.Pod, cpu, memory *ResourceUsage) NodeDetailResponse {
	status, color := nodeStatus(n)

	conditions := make([]NodeConditionRow, 0, len(n.Status.Conditions))
	for _, c := range n.Status.Conditions {
		conditions = append(conditions, NodeConditionRow{
			Type:    string(c.Type),
			Status:  string(c.Status),
			Reason:  c.Reason,
			Message: c.Message,
		})
	}

	taints := make([]NodeTaintRow, 0, len(n.Spec.Taints))
	for _, t := range n.Spec.Taints {
		taints = append(taints, NodeTaintRow{Key: t.Key, Value: t.Value, Effect: string(t.Effect)})
	}

	podRows := make([]NodeScheduledPodRow, 0, len(pods))
	for _, p := range pods {
		ready, total := podReadyCount(p)
		pStatus, pColor := podStatus(p)
		podRows = append(podRows, NodeScheduledPodRow{
			Name:        p.Name,
			Namespace:   p.Namespace,
			Ready:       fmt.Sprintf("%d/%d", ready, total),
			Status:      pStatus,
			StatusColor: string(pColor),
			Age:         formatAge(p.CreationTimestamp.Time),
		})
	}

	labels := n.Labels
	if labels == nil {
		labels = map[string]string{}
	}

	n.TypeMeta.Kind = "Node"
	n.TypeMeta.APIVersion = "v1"
	yaml := marshalYAML(n)

	return NodeDetailResponse{
		Name:           n.Name,
		Status:         status,
		StatusColor:    string(color),
		Roles:          nodeRoles(n),
		Age:            formatAge(n.CreationTimestamp.Time),
		KubeletVersion: n.Status.NodeInfo.KubeletVersion,
		OS:             n.Status.NodeInfo.OSImage,
		Capacity: NodeResources{
			CPU:    n.Status.Capacity.Cpu().String(),
			Memory: n.Status.Capacity.Memory().String(),
			Pods:   n.Status.Capacity.Pods().String(),
		},
		Allocatable: NodeResources{
			CPU:    n.Status.Allocatable.Cpu().String(),
			Memory: n.Status.Allocatable.Memory().String(),
			Pods:   n.Status.Allocatable.Pods().String(),
		},
		CPU:        cpu,
		Memory:     memory,
		Conditions: conditions,
		Taints:     taints,
		Pods:       podRows,
		Labels:     labels,
		YAML:       yaml,
	}
}

// nodeResourceusage reports one node's current CPU/memory usage (via
// metrics-server, if available) against its own allocatable capacity — the
// same "degrade to nil rather than fail" rule clusterResourceUsage
// (dashboard.go) follows cluster-wide, applied to a single node.
func nodeResourceUsage(ctx context.Context, metricsClientset *metricsclientset.Clientset, n corev1.Node) (cpu, memory *ResourceUsage) {
	if metricsClientset == nil {
		return nil, nil
	}

	metrics, err := metricsClientset.MetricsV1beta1().NodeMetricses().Get(ctx, n.Name, metav1.GetOptions{})
	if err != nil {
		slog.Warn("node metrics unavailable", "node", n.Name, "error", err)
		return nil, nil
	}

	cpuAllocatable := n.Status.Allocatable.Cpu()
	memAllocatable := n.Status.Allocatable.Memory()

	cpuUsedMilli := metrics.Usage.Cpu().MilliValue()
	memUsedBytes := metrics.Usage.Memory().Value()

	cpu = &ResourceUsage{Used: fmt.Sprintf("%dm", cpuUsedMilli), Limit: formatQuantity(*cpuAllocatable, formatMilli)}
	memory = &ResourceUsage{Used: fmt.Sprintf("%dMi", memUsedBytes/(1024*1024)), Limit: formatQuantity(*memAllocatable, formatMebibytes)}

	if capMilli := cpuAllocatable.MilliValue(); capMilli > 0 {
		cpu.Pct = int(cpuUsedMilli * 100 / capMilli)
	}
	if capBytes := memAllocatable.Value(); capBytes > 0 {
		memory.Pct = int(memUsedBytes * 100 / capBytes)
	}
	return cpu, memory
}
