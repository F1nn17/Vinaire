package api

import (
	"fmt"
	"log/slog"
	"net/http"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"

	"github.com/shiraku/vinaire/backend/internal/k8s"
)

// capacityWarnPct/capacityErrPct match the roadmap's own thresholds
// ("--warn above 80% usage", "--err when close to the pod-count or memory
// ceiling") — deliberately below 100 for err, since the point is to flag a
// node *before* it's already full, not after.
const (
	capacityWarnPct = 80
	capacityErrPct  = 95
)

// CapacityUsage matches the frontend's CapacityUsage type — a
// requests-based capacity figure, distinct from ResourceUsage (which is
// live metrics-server usage). Deliberately its own type rather than
// reusing ResourceUsage: conflating "what's requested" with "what's
// actually being used right now" is exactly the confusion this phase's
// own risk note warns against.
type CapacityUsage struct {
	Used  string `json:"used"`
	Total string `json:"total"`
	Pct   int    `json:"pct"`
	Color string `json:"color"`
}

// NodeCapacityRow matches the frontend's NodeCapacityRow type.
type NodeCapacityRow struct {
	Name   string        `json:"name"`
	CPU    CapacityUsage `json:"cpu"`
	Memory CapacityUsage `json:"memory"`
	Pods   CapacityUsage `json:"pods"`
}

// CapacityResponse matches the frontend's CapacityResponse type.
type CapacityResponse struct {
	Nodes   []NodeCapacityRow `json:"nodes"`
	Cluster NodeCapacityRow   `json:"cluster"`
}

// Capacity handles GET /api/v1/capacity — total Allocatable vs. total
// Requests across every non-terminated pod, cluster-wide and broken down
// per node. This is a requests-based figure (what's reserved), not live
// usage (what's actually running right now, which needs metrics-server —
// see the Nodes page's own CPU/Memory columns for that instead); the
// frontend must keep the two visually distinct.
func (h *Handler) Capacity(w http.ResponseWriter, r *http.Request) {
	_, clientset, ok := h.resolveCluster(w, r)
	if !ok {
		return
	}

	ctx := r.Context()
	nodes, err := k8s.ListNodes(ctx, clientset)
	if err != nil {
		respondK8sError(w, err)
		return
	}

	rows := make([]NodeCapacityRow, 0, len(nodes))
	var totalCPUUsed, totalCPUAllocatable, totalMemUsed, totalMemAllocatable, totalPodsUsed, totalPodsAllocatable int64

	for _, n := range nodes {
		pods, err := k8s.ListPodsOnNode(ctx, clientset, n.Name)
		if err != nil {
			slog.Warn("list pods on node failed for capacity", "node", n.Name, "error", err)
			pods = nil
		}

		cpuMilli, memBytes, podCount := sumPodRequests(pods)
		cpuAllocatable := n.Status.Allocatable.Cpu().MilliValue()
		memAllocatable := n.Status.Allocatable.Memory().Value()
		podsAllocatable := n.Status.Allocatable.Pods().Value()

		rows = append(rows, NodeCapacityRow{
			Name:   n.Name,
			CPU:    cpuCapacityUsage(cpuMilli, cpuAllocatable),
			Memory: capacityUsage(memBytes, memAllocatable, formatMebibytes),
			Pods:   capacityUsage(podCount, podsAllocatable, formatCount),
		})

		totalCPUUsed += cpuMilli
		totalCPUAllocatable += cpuAllocatable
		totalMemUsed += memBytes
		totalMemAllocatable += memAllocatable
		totalPodsUsed += podCount
		totalPodsAllocatable += podsAllocatable
	}

	respondJSON(w, CapacityResponse{
		Nodes: rows,
		Cluster: NodeCapacityRow{
			Name:   "cluster",
			CPU:    cpuCapacityUsage(totalCPUUsed, totalCPUAllocatable),
			Memory: capacityUsage(totalMemUsed, totalMemAllocatable, formatMebibytes),
			Pods:   capacityUsage(totalPodsUsed, totalPodsAllocatable, formatCount),
		},
	})
}

// sumPodRequests sums CPU (millicores)/memory (bytes) requests across every
// non-terminated pod, the same "Non-terminated Pods" scope
// `kubectl describe node` uses — a Succeeded/Failed pod no longer holds any
// real allocation on the node.
func sumPodRequests(pods []corev1.Pod) (cpuMilli, memBytes, podCount int64) {
	for _, p := range pods {
		if p.Status.Phase == corev1.PodSucceeded || p.Status.Phase == corev1.PodFailed {
			continue
		}
		podCount++
		for _, c := range p.Spec.Containers {
			if q, ok := c.Resources.Requests[corev1.ResourceCPU]; ok {
				cpuMilli += q.MilliValue()
			}
			if q, ok := c.Resources.Requests[corev1.ResourceMemory]; ok {
				memBytes += q.Value()
			}
		}
	}
	return cpuMilli, memBytes, podCount
}

func formatCount(q resource.Quantity) string { return fmt.Sprintf("%d", q.Value()) }

// capacityUsage builds a CapacityUsage from values already in the base
// unit format's own formatter reads directly via .Value() (bytes for
// formatMebibytes, a plain count for formatCount) — NOT for a millicore
// figure, which needs cpuCapacityUsage's NewMilliQuantity instead, since
// resource.NewQuantity(200, ...).MilliValue() means "200000m", not "200m".
func capacityUsage(used, total int64, format func(resource.Quantity) string) CapacityUsage {
	usedQty := *resource.NewQuantity(used, resource.DecimalSI)
	totalQty := *resource.NewQuantity(total, resource.DecimalSI)
	return buildCapacityUsage(used, total, format(usedQty), format(totalQty))
}

// cpuCapacityUsage is capacityUsage's CPU-specific counterpart: usedMilli/
// totalMilli are already millicore values (see sumPodRequests/
// Node.Status.Allocatable.Cpu().MilliValue()), so they need
// NewMilliQuantity, not NewQuantity, to format back out correctly via
// formatMilli.
func cpuCapacityUsage(usedMilli, totalMilli int64) CapacityUsage {
	usedQty := *resource.NewMilliQuantity(usedMilli, resource.DecimalSI)
	totalQty := *resource.NewMilliQuantity(totalMilli, resource.DecimalSI)
	return buildCapacityUsage(usedMilli, totalMilli, formatMilli(usedQty), formatMilli(totalQty))
}

func buildCapacityUsage(used, total int64, usedStr, totalStr string) CapacityUsage {
	pct := 0
	if total > 0 {
		pct = int(used * 100 / total)
	}

	color := StatusOK
	switch {
	case pct >= capacityErrPct:
		color = StatusErr
	case pct >= capacityWarnPct:
		color = StatusWarn
	}

	return CapacityUsage{Used: usedStr, Total: totalStr, Pct: pct, Color: string(color)}
}
