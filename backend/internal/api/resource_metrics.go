package api

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
)

// Roadmap Phase 32: purpose-built metrics endpoints for the detail pages'
// Metrics tab, so the frontend never assembles PromQL. Built on the same
// already-configured Prometheus as Phase 16's proxy (PROMETHEUS_URL); this
// is read-only.

// MetricsSeries is one line on a chart. Points are [unixSeconds, value].
type MetricsSeries struct {
	Name   string       `json:"name"`
	Points [][2]float64 `json:"points"`
}

// ResourceMetricsResponse matches the frontend's ResourceMetrics type.
//
// Available=false means Prometheus couldn't be asked at all (not configured,
// unreachable, rejected the query) — Reason says why. Available=true with
// NoData=true means Prometheus answered but has no samples for this
// resource/metric (e.g. cAdvisor doesn't export it on this cluster). The two
// are distinct from "a chart with points" on purpose: an empty 200 must never
// be mistaken for "no data yet".
type ResourceMetricsResponse struct {
	Available   bool            `json:"available"`
	NoData      bool            `json:"noData"`
	Reason      string          `json:"reason,omitempty"`
	Type        string          `json:"type"`
	Unit        string          `json:"unit"`
	Range       string          `json:"range"`
	StepSeconds int             `json:"stepSeconds"`
	Start       int64           `json:"start"`
	End         int64           `json:"end"`
	Series      []MetricsSeries `json:"series"`
}

var metricRanges = map[string]time.Duration{
	"1h":  time.Hour,
	"6h":  6 * time.Hour,
	"24h": 24 * time.Hour,
	"7d":  7 * 24 * time.Hour,
}

// resolutionPoints is roughly how many points per series each resolution
// aims for; the step is derived from it (never below minStepSeconds, a
// typical scrape interval).
var resolutionPoints = map[string]int{"low": 60, "medium": 150, "high": 300}

const minStepSeconds = 15

func metricsStep(rng time.Duration, resolution string) int {
	step := int(rng.Seconds()) / resolutionPoints[resolution]
	if step < minStepSeconds {
		step = minStepSeconds
	}
	return step
}

// k8sNameRe is the shape of any Kubernetes namespace/pod/deployment/node name
// (DNS subdomain). Validating it before it goes into a PromQL selector means a
// crafted path segment can't inject PromQL.
var k8sNameRe = regexp.MustCompile(`^[a-z0-9]([-a-z0-9.]*[a-z0-9])?$`)

// promString renders s as a PromQL string literal.
func promString(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	return `"` + s + `"`
}

// metricQuery is one PromQL expression and how its result series are named.
// If splitBy is set, each returned series is named by that label's value
// (e.g. one line per disk device); otherwise the single series is `name`.
type metricQuery struct {
	name    string
	promql  string
	splitBy string
}

// metricPlan is what a (resource, type) pair resolves to.
type metricPlan struct {
	unit    string
	queries []metricQuery
}

const rateWindow = "2m"

// podSelector builds the label matcher for pods' cAdvisor series.
// containerFilter excludes the pod-level cgroup aggregate and the pause
// container so CPU/memory aren't double-counted; network series live on the
// pause container, so they must not use it.
func podSelector(namespace, podMatcher string, containerFilter bool) string {
	sel := fmt.Sprintf(`namespace=%s,%s`, promString(namespace), podMatcher)
	if containerFilter {
		sel += `,container!="",container!="POD"`
	}
	return sel
}

// nodeSelectors are the ways a node's cAdvisor series can name their node. Which one a cluster
// has depends on how Prometheus scrapes the kubelet: kube-prometheus-stack relabels a `node`
// label on, the community prometheus chart scrapes through the API server and leaves the node's
// name in `instance` (and its labels, like kubernetes_io_hostname), and a scrape straight at the
// kubelet has `instance` as address:port. Each is tried in turn (joined with `or`, so the first
// that has samples wins and the rest add nothing) — Roadmap Phase 61: with only `node`, a
// perfectly healthy Prometheus answered "no data".
func nodeSelectors(name string) []string {
	q := regexp.QuoteMeta(name)
	return []string{
		`id="/",node=` + promString(name),
		`id="/",instance=~` + promString(q+`(:[0-9]+)?`),
		`id="/",kubernetes_io_hostname=` + promString(name),
		`id="/",nodename=` + promString(name),
	}
}

// orJoin joins alternative queries: the left one's series, and the right ones' only where the
// left has none with the same labels.
func orJoin(qs []string) string {
	return strings.Join(qs, " or ")
}

func planFor(kind, namespace, name, metricType string) (metricPlan, bool) {
	var sels func(containerFilter bool) []string
	nodeLevel := false
	switch kind {
	case "pods":
		sels = func(cf bool) []string { return []string{podSelector(namespace, "pod="+promString(name), cf)} }
	case "deployments":
		// A Deployment's pods are <name>-<replicaset hash>-<pod hash>. Matching
		// the name pattern (rather than today's pod list) keeps pods from
		// earlier rollouts in the history.
		matcher := "pod=~" + promString("^"+regexp.QuoteMeta(name)+"-[a-z0-9]{6,10}-[a-z0-9]{5}$")
		sels = func(cf bool) []string { return []string{podSelector(namespace, matcher, cf)} }
	case "statefulsets":
		// the pods of a StatefulSet are named <name>-<ordinal>
		matcher := "pod=~" + promString("^"+regexp.QuoteMeta(name)+"-[0-9]+$")
		sels = func(cf bool) []string { return []string{podSelector(namespace, matcher, cf)} }
	case "daemonsets", "jobs":
		// the pods of a DaemonSet or a Job are named <name>-<5 random characters>
		matcher := "pod=~" + promString("^"+regexp.QuoteMeta(name)+"-[a-z0-9]{5}$")
		sels = func(cf bool) []string { return []string{podSelector(namespace, matcher, cf)} }
	case "nodes":
		nodeLevel = true
		sels = func(bool) []string { return nodeSelectors(name) }
	case "cluster":
		// the whole cluster: the root cgroup of every node added together
		nodeLevel = true
		sels = func(bool) []string { return []string{`id="/"`} }
	default:
		return metricPlan{}, false
	}
	// build renders one query per selector and joins them
	build := func(containerFilter bool, format string, extra ...any) string {
		var qs []string
		for _, sel := range sels(containerFilter) {
			qs = append(qs, fmt.Sprintf(format, append([]any{sel}, extra...)...))
		}
		return orJoin(qs)
	}

	switch metricType {
	case "cpu":
		return metricPlan{"cores", []metricQuery{{name: "cpu", promql: build(true, `sum(rate(container_cpu_usage_seconds_total{%s}[%s]))`, rateWindow)}}}, true
	case "memory":
		return metricPlan{"bytes", []metricQuery{{name: "memory", promql: build(true, `sum(container_memory_working_set_bytes{%s})`)}}}, true
	case "network":
		return metricPlan{"bytes/s", []metricQuery{
			{name: "receive", promql: build(false, `sum(rate(container_network_receive_bytes_total{%s}[%s]))`, rateWindow)},
			{name: "transmit", promql: build(false, `sum(rate(container_network_transmit_bytes_total{%s}[%s]))`, rateWindow)},
		}}, true
	case "filesystem":
		if nodeLevel {
			return metricPlan{"bytes", []metricQuery{{promql: build(false, `sum by (device) (container_fs_usage_bytes{%s})`), splitBy: "device"}}}, true
		}
		return metricPlan{"bytes", []metricQuery{{name: "filesystem", promql: build(true, `sum(container_fs_usage_bytes{%s})`)}}}, true
	}
	return metricPlan{}, false
}

// promMatrix is the part of Prometheus's query_range response this reads.
type promMatrix struct {
	Status string `json:"status"`
	Error  string `json:"error"`
	Data   struct {
		Result []struct {
			Metric map[string]string `json:"metric"`
			Values [][2]any          `json:"values"`
		} `json:"result"`
	} `json:"data"`
}

func runRangeQuery(ctx context.Context, fetch promFetch, q metricQuery, start, end time.Time, step int) ([]MetricsSeries, error) {
	params := url.Values{}
	params.Set("query", q.promql)
	params.Set("start", strconv.FormatInt(start.Unix(), 10))
	params.Set("end", strconv.FormatInt(end.Unix(), 10))
	params.Set("step", strconv.Itoa(step))

	body, err := fetch(ctx, "/api/v1/query_range", params)
	if err != nil {
		return nil, err
	}
	var m promMatrix
	if err := json.Unmarshal(body, &m); err != nil {
		return nil, fmt.Errorf("decoding prometheus response: %w", err)
	}
	if m.Status != "success" {
		return nil, fmt.Errorf("prometheus: %s", m.Error)
	}

	var out []MetricsSeries
	for _, r := range m.Data.Result {
		s := MetricsSeries{Name: q.name, Points: make([][2]float64, 0, len(r.Values))}
		if q.splitBy != "" {
			s.Name = r.Metric[q.splitBy]
			if s.Name == "" {
				s.Name = q.splitBy
			}
		}
		for _, v := range r.Values {
			ts, ok1 := v[0].(float64)
			str, ok2 := v[1].(string)
			if !ok1 || !ok2 {
				continue
			}
			val, err := strconv.ParseFloat(str, 64)
			if err != nil || math.IsNaN(val) || math.IsInf(val, 0) {
				continue
			}
			s.Points = append(s.Points, [2]float64{ts, val})
		}
		if len(s.Points) > 0 {
			out = append(out, s)
		}
	}
	return out, nil
}

func (h *Handler) serveResourceMetrics(w http.ResponseWriter, r *http.Request, kind, namespace, name string) {
	q := r.URL.Query()
	metricType := q.Get("type")
	rangeKey := q.Get("range")
	if rangeKey == "" {
		rangeKey = "1h"
	}
	resolution := q.Get("resolution")
	if resolution == "" {
		resolution = "medium"
	}

	rng, rangeOK := metricRanges[rangeKey]
	_, resOK := resolutionPoints[resolution]
	plan, planOK := planFor(kind, namespace, name, metricType)
	switch {
	case !rangeOK:
		respondError(w, http.StatusBadRequest, "range must be one of 1h, 6h, 24h, 7d")
		return
	case !resOK:
		respondError(w, http.StatusBadRequest, "resolution must be one of low, medium, high")
		return
	case !planOK:
		respondError(w, http.StatusBadRequest, "type must be one of cpu, memory, network, filesystem")
		return
	case !k8sNameRe.MatchString(name) || ((kind != "nodes" && kind != "cluster") && !k8sNameRe.MatchString(namespace)):
		respondError(w, http.StatusBadRequest, "invalid resource name")
		return
	}

	step := metricsStep(rng, resolution)
	end := time.Now()
	start := end.Add(-rng)
	resp := ResourceMetricsResponse{
		Type: metricType, Unit: plan.unit, Range: rangeKey, StepSeconds: step,
		Start: start.Unix(), End: end.Unix(), Series: []MetricsSeries{},
	}

	entry, cs, ok := h.resolveCluster(w, r)
	if !ok {
		return
	}
	fetch, reason := h.prometheusFetcher(r.Context(), entry.Name, cs)
	if fetch == nil {
		resp.Reason = reason
		respondJSON(w, resp)
		return
	}

	// Queries are independent; run them concurrently, keep a stable order.
	results := make([][]MetricsSeries, len(plan.queries))
	errs := make([]error, len(plan.queries))
	var wg sync.WaitGroup
	for i, mq := range plan.queries {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results[i], errs[i] = runRangeQuery(r.Context(), fetch, mq, start, end, step)
		}()
	}
	wg.Wait()

	for _, err := range errs {
		if err != nil {
			resp.Reason = err.Error()
			respondJSON(w, resp)
			return
		}
	}
	for _, s := range results {
		resp.Series = append(resp.Series, s...)
	}
	sort.SliceStable(resp.Series, func(i, j int) bool { return resp.Series[i].Name < resp.Series[j].Name })

	resp.Available = true
	if len(resp.Series) == 0 {
		resp.NoData = true
		resp.Reason = explainNoData(r.Context(), fetch, kind)
	}
	respondJSON(w, resp)
}

// explainNoData says why there is nothing to draw, by asking Prometheus whether it holds
// container metrics at all (Roadmap Phase 61): "your Prometheus doesn't scrape the kubelet" and
// "it does, but none of it names this node" call for different fixes.
func explainNoData(ctx context.Context, fetch promFetch, kind string) string {
	const generic = "Prometheus has no samples for this resource — the metric may not be exported on this cluster"
	n, err := promCount(ctx, fetch, `container_cpu_usage_seconds_total`)
	switch {
	case err != nil:
		return generic
	case n == 0:
		return "Prometheus is running but holds no container metrics — it isn't scraping the kubelet's cAdvisor endpoint. Check its scrape targets (the Metrics stack page can install a Prometheus that does)."
	case kind == "nodes":
		return "Prometheus holds container metrics, but none it can tie to this node: Vinaire looks for the node's name in the labels node, instance, kubernetes_io_hostname and nodename, and the scrape config here labels it differently."
	}
	return generic
}

// promCount returns how many series match the selector.
func promCount(ctx context.Context, fetch promFetch, selector string) (int, error) {
	params := url.Values{}
	params.Set("query", "count("+selector+")")
	body, err := fetch(ctx, "/api/v1/query", params)
	if err != nil {
		return 0, err
	}
	var m struct {
		Status string `json:"status"`
		Data   struct {
			Result []struct {
				Value [2]any `json:"value"`
			} `json:"result"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &m); err != nil || m.Status != "success" {
		return 0, fmt.Errorf("unreadable prometheus answer")
	}
	if len(m.Data.Result) == 0 {
		return 0, nil // count() of nothing is an empty result
	}
	str, _ := m.Data.Result[0].Value[1].(string)
	f, err := strconv.ParseFloat(str, 64)
	return int(f), err
}

// PodMetrics handles GET /api/v1/pods/{namespace}/{name}/metrics.
func (h *Handler) PodMetrics(w http.ResponseWriter, r *http.Request) {
	h.serveResourceMetrics(w, r, "pods", chi.URLParam(r, "namespace"), chi.URLParam(r, "name"))
}

// DeploymentMetrics handles GET /api/v1/deployments/{namespace}/{name}/metrics.
func (h *Handler) DeploymentMetrics(w http.ResponseWriter, r *http.Request) {
	h.serveResourceMetrics(w, r, "deployments", chi.URLParam(r, "namespace"), chi.URLParam(r, "name"))
}

// NodeMetrics handles GET /api/v1/nodes/{name}/metrics. Nodes are
// cluster-scoped, so there's no namespace segment.
func (h *Handler) NodeMetrics(w http.ResponseWriter, r *http.Request) {
	h.serveResourceMetrics(w, r, "nodes", "", chi.URLParam(r, "name"))
}

// ClusterMetrics handles GET /api/v1/cluster/metrics: the usage history of the whole cluster (every
// node's cAdvisor root cgroup added together), for the Overview page. Same sources and rules as the
// per-object metrics: Prometheus through the service proxy, as the caller.
func (h *Handler) ClusterMetrics(w http.ResponseWriter, r *http.Request) {
	h.serveResourceMetrics(w, r, "cluster", "", "cluster")
}

// StatefulSetMetrics handles GET /api/v1/statefulsets/{namespace}/{name}/metrics (Roadmap Phase 66).
func (h *Handler) StatefulSetMetrics(w http.ResponseWriter, r *http.Request) {
	h.serveResourceMetrics(w, r, "statefulsets", chi.URLParam(r, "namespace"), chi.URLParam(r, "name"))
}

// DaemonSetMetrics handles GET /api/v1/daemonsets/{namespace}/{name}/metrics.
func (h *Handler) DaemonSetMetrics(w http.ResponseWriter, r *http.Request) {
	h.serveResourceMetrics(w, r, "daemonsets", chi.URLParam(r, "namespace"), chi.URLParam(r, "name"))
}

// JobMetrics handles GET /api/v1/jobs/{namespace}/{name}/metrics.
func (h *Handler) JobMetrics(w http.ResponseWriter, r *http.Request) {
	h.serveResourceMetrics(w, r, "jobs", chi.URLParam(r, "namespace"), chi.URLParam(r, "name"))
}
