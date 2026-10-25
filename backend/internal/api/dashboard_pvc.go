package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"

	"github.com/f1nn17/vinaire/backend/internal/k8s"
)

// Roadmap Phase 90: the Overview's "PVC near capacity" card. It used to be a stub that always said "Not available yet"
// whatever the cluster held; now it is computed from what the kubelet reports (kubelet_volume_stats_*, read from the
// cluster's Prometheus as the caller), and it says which of three things is true: some claims are near their size, none
// is, or nobody can tell — and why.

// pvcNearPercent is the share of its size above which a claim is "near capacity".
const pvcNearPercent = 80

// PVCCapacitySummary is the card's data.
type PVCCapacitySummary struct {
	// State: "near" (at least one claim is at or over the threshold), "ok" (usage is known and none is), or
	// "nodata" (nothing says how full the claims are — see Reason).
	State string `json:"state"`
	// Threshold is the percentage above which a claim counts.
	Threshold int `json:"threshold"`
	// Total is how many claims the caller can list; Measured is how many of them have usage numbers.
	Total    int `json:"total"`
	Measured int `json:"measured"`
	// Count is how many measured claims are at or over the threshold.
	Count int `json:"count"`
	// Sample names the fullest of them ("namespace/name 91%"), at most dashboardSampleLimit.
	Sample []string `json:"sample"`
	// Reason is why State is "nodata".
	Reason string `json:"reason,omitempty"`
}

type promSample struct {
	Labels map[string]string
	Value  float64
}

// promVector runs an instant query and returns every series. ok is false when the query failed or Prometheus
// answered with an error.
func promVector(ctx context.Context, fetch promFetch, expr string) ([]promSample, bool) {
	params := url.Values{}
	params.Set("query", expr)
	body, err := fetch(ctx, "/api/v1/query", params)
	if err != nil {
		return nil, false
	}
	var m struct {
		Status string `json:"status"`
		Data   struct {
			Result []struct {
				Metric map[string]string `json:"metric"`
				Value  [2]any            `json:"value"`
			} `json:"result"`
		} `json:"data"`
	}
	if json.Unmarshal(body, &m) != nil || m.Status != "success" {
		return nil, false
	}
	out := make([]promSample, 0, len(m.Data.Result))
	for _, r := range m.Data.Result {
		str, _ := r.Value[1].(string)
		f, err := strconv.ParseFloat(str, 64)
		if err != nil || f != f { // NaN
			continue
		}
		out = append(out, promSample{Labels: r.Metric, Value: f})
	}
	return out, true
}

// summarizePVCCapacity decides the card from the claims and (when there is a Prometheus) the kubelet's numbers. fetch may be
// nil, and whyNoPrometheus then says why.
func summarizePVCCapacity(ctx context.Context, fetch promFetch, whyNoPrometheus string, pvcs []corev1.PersistentVolumeClaim) PVCCapacitySummary {
	res := PVCCapacitySummary{State: "nodata", Threshold: pvcNearPercent, Total: len(pvcs), Sample: []string{}}
	if len(pvcs) == 0 {
		res.State, res.Reason = "ok", ""
		return res
	}
	if fetch == nil {
		res.Reason = whyNoPrometheus
		if res.Reason == "" {
			res.Reason = "no Prometheus to ask"
		}
		return res
	}
	samples, ok := promVector(ctx, fetch,
		"max by (namespace, persistentvolumeclaim) (kubelet_volume_stats_used_bytes) * 100"+
			" / max by (namespace, persistentvolumeclaim) (kubelet_volume_stats_capacity_bytes)")
	if !ok {
		res.Reason = "Prometheus didn't answer"
		return res
	}
	// only claims that exist now count (a series can outlive a deleted claim for a while)
	exists := map[string]bool{}
	for _, c := range pvcs {
		exists[c.Namespace+"/"+c.Name] = true
	}
	type row struct {
		name string
		pct  float64
	}
	var near []row
	for _, s := range samples {
		key := s.Labels["namespace"] + "/" + s.Labels["persistentvolumeclaim"]
		if !exists[key] {
			continue
		}
		res.Measured++
		if s.Value >= pvcNearPercent {
			near = append(near, row{key, s.Value})
		}
	}
	if res.Measured == 0 {
		res.Reason = "the kubelet reports no usage for these volumes (it doesn't for some storage, such as k3s's local-path)"
		return res
	}
	sort.Slice(near, func(i, j int) bool {
		if near[i].pct != near[j].pct {
			return near[i].pct > near[j].pct
		}
		return near[i].name < near[j].name
	})
	res.Count = len(near)
	for i, r := range near {
		if i == dashboardSampleLimit {
			break
		}
		res.Sample = append(res.Sample, fmt.Sprintf("%s %d%%", r.name, int(r.pct)))
	}
	res.State = "ok"
	if res.Count > 0 {
		res.State = "near"
	}
	return res
}

// clusterPVCCapacity lists the claims and asks Prometheus, as the caller. It never fails the dashboard.
func (h *Handler) clusterPVCCapacity(ctx context.Context, cluster string, cs *kubernetes.Clientset) *PVCCapacitySummary {
	ctx, cancel := context.WithTimeout(ctx, 6*time.Second)
	defer cancel()
	pvcs, err := k8s.ListPersistentVolumeClaims(ctx, cs, "")
	if err != nil {
		return nil // can't list claims: say nothing rather than guess
	}
	fetch, why := h.prometheusFetcher(ctx, cluster, cs)
	res := summarizePVCCapacity(ctx, fetch, why, pvcs)
	return &res
}
