package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"github.com/shiraku/vinaire/backend/internal/k8s"
	"github.com/shiraku/vinaire/backend/internal/metricsstack"
)

// The metrics stack (Roadmap Phase 37): find a Prometheus in the cluster, tell the
// UI what is and isn't there, describe the one-click install, and read metrics
// through it.
//
// Nothing here installs anything: the plan is handed to the Marketplace's Helm
// install (as the signed-in user, preflight and progress included). And Prometheus
// is reached through the Kubernetes API's service proxy *as the caller* — so it
// works from anywhere (no ingress, no in-cluster DNS) and the cluster's RBAC
// decides who may query it — unless the operator configured PROMETHEUS_URL, which
// wins (and, being the operator's own choice, isn't checked per caller).

// PrometheusLocation is where a Prometheus Service lives.
type PrometheusLocation struct {
	Namespace string `json:"namespace"`
	Service   string `json:"service"`
	Port      int32  `json:"port"`
}

// MetricsStatus is GET /api/v1/metrics/status.
type MetricsStatus struct {
	// State: "none" (nothing), "metrics-server-only" (current values, no history),
	// "partial" (a Prometheus, but an exporter the charts need is missing),
	// "prometheus-ready" (Prometheus with kube-state-metrics and node-exporter),
	// "configured" (PROMETHEUS_URL points somewhere).
	State            string              `json:"state"`
	MetricsServer    bool                `json:"metricsServer"`
	Prometheus       *PrometheusLocation `json:"prometheus,omitempty"`
	KubeStateMetrics bool                `json:"kubeStateMetrics"`
	NodeExporter     bool                `json:"nodeExporter"`
	// OperatorFound: prometheus-operator's CRDs exist, so a second Prometheus is
	// probably not wanted.
	OperatorFound bool `json:"operatorFound"`
	// Alertmanager: where one was found (the bell in the top bar reads it).
	Alertmanager *PrometheusLocation `json:"alertmanager,omitempty"`
	// Profile / Release: set when the Prometheus is one Vinaire installed, so the
	// page can change its settings (an upgrade of that release).
	Profile string   `json:"profile,omitempty"`
	Release string   `json:"release,omitempty"`
	Missing []string `json:"missing,omitempty"`
	// Flowing: Prometheus answers and holds container metrics.
	Flowing          bool   `json:"flowing"`
	Detail           string `json:"detail,omitempty"`
	ManagedByVinaire bool   `json:"managedByVinaire"`
	ConfiguredURL    bool   `json:"configuredUrl"`
}

func findPrometheus(ctx context.Context, cs kubernetes.Interface) (*PrometheusLocation, error) {
	return findService(ctx, cs, metricsstack.IsPrometheusService, metricsstack.ServerService, metricsstack.StackService)
}

func findAlertmanager(ctx context.Context, cs kubernetes.Interface) (*PrometheusLocation, error) {
	return findService(ctx, cs, metricsstack.IsAlertmanagerService)
}

// findService lists the Services the label test accepts and picks one, preferring
// the names Vinaire's own installs create. The label tests are in metricsstack.
func findService(ctx context.Context, cs kubernetes.Interface, is func(map[string]string, string, []int32) (int32, bool), prefer ...string) (*PrometheusLocation, error) {
	list, err := cs.CoreV1().Services("").List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	var best *PrometheusLocation
	for _, s := range list.Items {
		ports := make([]int32, 0, len(s.Spec.Ports))
		for _, p := range s.Spec.Ports {
			ports = append(ports, p.Port)
		}
		port, ok := is(s.Labels, s.Spec.ClusterIP, ports)
		if !ok {
			continue
		}
		cand := &PrometheusLocation{Namespace: s.Namespace, Service: s.Name, Port: port}
		if best == nil {
			best = cand
		}
		for _, name := range prefer {
			if s.Name == name {
				best = cand
			}
		}
	}
	return best, nil
}

// promLocCache remembers where Prometheus is for a short while: the dashboards ask
// every few seconds and listing every Service each time would be wasteful. It holds
// a location, not data — the query itself is still the caller's own through the
// service proxy, so a caller who may not reach it gets the API server's refusal.
var promLocCache = struct {
	sync.Mutex
	m map[string]promLocEntry
}{m: map[string]promLocEntry{}}

type promLocEntry struct {
	loc *PrometheusLocation
	at  time.Time
}

const promLocTTL = 30 * time.Second

func cachedPrometheus(ctx context.Context, cluster string, cs kubernetes.Interface) (*PrometheusLocation, error) {
	promLocCache.Lock()
	e, ok := promLocCache.m[cluster]
	promLocCache.Unlock()
	if ok && time.Since(e.at) < promLocTTL {
		return e.loc, nil
	}
	loc, err := findPrometheus(ctx, cs)
	if err != nil {
		return nil, err
	}
	promLocCache.Lock()
	promLocCache.m[cluster] = promLocEntry{loc: loc, at: time.Now()}
	promLocCache.Unlock()
	return loc, nil
}

// promFetch runs one Prometheus HTTP GET (path like /api/v1/query_range).
type promFetch func(ctx context.Context, path string, params url.Values) ([]byte, error)

// prometheusFetcher returns how to query Prometheus for this request, or a reason
// there is nothing to query.
func (h *Handler) prometheusFetcher(ctx context.Context, cluster string, cs kubernetes.Interface) (promFetch, string) {
	if h.PrometheusURL != "" {
		return func(ctx context.Context, path string, params url.Values) ([]byte, error) {
			return fetchUpstream(ctx, h.PrometheusURL, path, params)
		}, ""
	}
	loc, err := cachedPrometheus(ctx, cluster, cs)
	if err != nil {
		return nil, "couldn't look for Prometheus: " + statusMessage(err)
	}
	if loc == nil {
		return nil, "no Prometheus in this cluster — install the metrics stack (Metrics stack page) or set PROMETHEUS_URL"
	}
	return proxyFetch(cs, loc, "Prometheus"), ""
}

// proxyFetch queries a Service through the API server's service proxy, as whoever
// the clientset belongs to.
func proxyFetch(cs kubernetes.Interface, loc *PrometheusLocation, what string) promFetch {
	return func(ctx context.Context, path string, params url.Values) ([]byte, error) {
		p := map[string]string{}
		for k := range params {
			p[k] = params.Get(k)
		}
		ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
		defer cancel()
		body, err := cs.CoreV1().Services(loc.Namespace).ProxyGet("http", loc.Service, strconv.Itoa(int(loc.Port)), path, p).DoRaw(ctx)
		if err != nil {
			return nil, fmt.Errorf("%s", promErrorReason(err, loc, what))
		}
		return body, nil
	}
}

// alertmanagerFetcher is prometheusFetcher's counterpart for Alertmanager:
// ALERTMANAGER_URL if the operator set it, else the Alertmanager found in the cluster.
func (h *Handler) alertmanagerFetcher(ctx context.Context, cs kubernetes.Interface) (promFetch, string) {
	if h.AlertmanagerURL != "" {
		return func(ctx context.Context, path string, params url.Values) ([]byte, error) {
			return fetchUpstream(ctx, h.AlertmanagerURL, path, params)
		}, ""
	}
	loc, err := findAlertmanager(ctx, cs)
	if err != nil {
		return nil, "couldn't look for Alertmanager: " + statusMessage(err)
	}
	if loc == nil {
		return nil, "no Alertmanager in this cluster — install the metrics stack with Alertmanager (Metrics stack page) or set ALERTMANAGER_URL"
	}
	return proxyFetch(cs, loc, "Alertmanager"), ""
}

// promErrorReason explains why Prometheus couldn't be reached. The common case is
// the caller lacking `get` on services/proxy for it.
func promErrorReason(err error, loc *PrometheusLocation, what string) string {
	if apierrors.IsForbidden(err) {
		return fmt.Sprintf("You don't have permission to reach %s through the API server — it needs `get` on services/proxy for %q in namespace %q (the chart's -viewer role grants it for the standard names).", what, loc.Service, loc.Namespace)
	}
	return what + " didn't answer: " + statusMessage(err)
}

// MetricsStatus handles GET /api/v1/metrics/status.
func (h *Handler) MetricsStatus(w http.ResponseWriter, r *http.Request) {
	entry, cs, ok := h.resolveCluster(w, r)
	if !ok {
		return
	}
	ctx := r.Context()
	st := MetricsStatus{State: "none"}

	if _, err := cs.Discovery().ServerResourcesForGroupVersion("metrics.k8s.io/v1beta1"); err == nil {
		st.MetricsServer = true
	}
	if _, err := cs.Discovery().ServerResourcesForGroupVersion("monitoring.coreos.com/v1"); err == nil {
		st.OperatorFound = true
	}

	if h.PrometheusURL != "" {
		st.State, st.ConfiguredURL = "configured", true
		_, err := fetchUpstream(ctx, h.PrometheusURL, "/-/ready", nil)
		st.Flowing = err == nil
		if err != nil {
			st.Detail = "PROMETHEUS_URL is set but doesn't answer: " + err.Error()
		}
		respondJSON(w, st)
		return
	}

	loc, err := findPrometheus(ctx, cs)
	if err != nil {
		respondK8sError(w, err)
		return
	}
	// exporters: best effort — a caller who can't list them just sees them as missing
	if l, err := cs.AppsV1().Deployments("").List(ctx, metav1.ListOptions{LabelSelector: "app.kubernetes.io/name=kube-state-metrics"}); err == nil {
		for _, d := range l.Items {
			if metricsstack.IsKubeStateMetrics(d.Labels) {
				st.KubeStateMetrics = true
			}
		}
	}
	if l, err := cs.AppsV1().DaemonSets("").List(ctx, metav1.ListOptions{}); err == nil {
		for _, d := range l.Items {
			if metricsstack.IsNodeExporter(d.Labels) {
				st.NodeExporter = true
			}
		}
	}
	st.Prometheus = loc
	switch {
	case loc != nil:
		st.State = "prometheus-ready"
		if !st.KubeStateMetrics {
			st.Missing = append(st.Missing, "kube-state-metrics (deployment and pod state)")
		}
		if !st.NodeExporter {
			st.Missing = append(st.Missing, "node-exporter (node CPU, memory, network and disk)")
		}
		if len(st.Missing) > 0 {
			st.State = "partial"
		}
	case st.MetricsServer:
		st.State = "metrics-server-only"
	}
	if p := metricsstack.ProfileOf(serviceOf(loc)); p != "" {
		st.ManagedByVinaire, st.Profile, st.Release = true, string(p), metricsstack.ReleaseOf(p)
	}
	if am, err := findAlertmanager(ctx, cs); err == nil {
		st.Alertmanager = am
	}

	if loc != nil {
		fetch, reason := h.prometheusFetcher(ctx, entry.Name, cs)
		if fetch == nil {
			st.Detail = reason
		} else {
			body, err := fetch(ctx, "/api/v1/query", url.Values{"query": {"count(container_cpu_usage_seconds_total)"}})
			if err != nil {
				st.Detail = err.Error()
			} else {
				var pr struct {
					Data struct {
						Result []struct {
							Value []any `json:"value"`
						} `json:"result"`
					} `json:"data"`
				}
				if json.Unmarshal(body, &pr) == nil && len(pr.Data.Result) > 0 && len(pr.Data.Result[0].Value) == 2 {
					if s, _ := pr.Data.Result[0].Value[1].(string); s != "" && s != "0" {
						st.Flowing = true
					}
				}
				if !st.Flowing {
					st.Detail = "Prometheus answers but holds no container metrics yet — the first scrape takes a minute or two after it starts."
				}
			}
		}
	}
	respondJSON(w, st)
}

// MetricsPlan is GET /api/v1/metrics/plan.
type MetricsPlan struct {
	Steps    []metricsstack.InstallStep `json:"steps"`
	Notes    []string                   `json:"notes"`
	Warnings []string                   `json:"warnings"`
}

// MetricsPlan handles GET /api/v1/metrics/plan?namespace=&retentionDays=&storageSize=&storageClass=:
// what the one-click install would install (the pinned chart and values), and what
// to know before pressing the button.
func (h *Handler) MetricsPlan(w http.ResponseWriter, r *http.Request) {
	_, cs, ok := h.resolveCluster(w, r)
	if !ok {
		return
	}
	q := r.URL.Query()
	days, _ := strconv.Atoi(q.Get("retentionDays"))
	opts, err := metricsstack.Options{
		Profile:   metricsstack.Profile(q.Get("profile")),
		Namespace: q.Get("namespace"), RetentionDay: days,
		StorageSize: q.Get("storageSize"), StorageClass: q.Get("storageClass"),
		NodeSelector: q.Get("nodeSelector"), Alertmanager: q.Get("alertmanager") == "true",
	}.Normalize()
	if err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}
	ctx := r.Context()
	plan := MetricsPlan{Steps: metricsstack.Plan(opts), Notes: []string{}, Warnings: []string{}}

	if opts.Profile == metricsstack.ProfileLite {
		if _, err := cs.Discovery().ServerResourcesForGroupVersion("metrics.k8s.io/v1beta1"); err == nil {
			plan.Warnings = append(plan.Warnings, "metrics-server is already running in this cluster (k3s ships it). A second one would fight over the same API — don't install it.")
		}
		plan.Notes = append(plan.Notes, "metrics-server gives the current CPU and memory only — no history, no network or disk charts. On clusters whose kubelets use self-signed serving certificates (kubeadm), it needs the kubelets to serve a certificate the cluster trusts; Vinaire doesn't switch certificate checks off for it.")
	} else if loc, _ := findPrometheus(ctx, cs); loc != nil {
		plan.Warnings = append(plan.Warnings, fmt.Sprintf(
			"A Prometheus is already running (%s/%s). Installing a second one would split your metrics — use it instead.", loc.Namespace, loc.Service))
	}
	if opts.Profile == metricsstack.ProfileFull {
		plan.Warnings = append(plan.Warnings, "kube-prometheus-stack is the heavy option: the operator, Prometheus, Alertmanager, exporters and about 100 alert rules, and it adds cluster-wide CRDs (so it needs rights a plain namespace admin doesn't have). Grafana is left off. Prefer “standard” on a small cluster.")
	}
	if opts.NodeSelector != "" {
		k, v, _ := strings.Cut(opts.NodeSelector, "=")
		found := false
		if nodes, err := k8s.ListNodes(ctx, cs); err == nil {
			for _, n := range nodes {
				if got, ok := n.Labels[k]; ok && got == v {
					found = true
				}
			}
			if !found {
				plan.Warnings = append(plan.Warnings, fmt.Sprintf("No node carries the label %s — the pods would stay Pending. Label a node first (Nodes → a node → Labels), or clear the node selector.", opts.NodeSelector))
			}
		}
	}
	if opts.Profile == metricsstack.ProfileStandard {
		if _, err := cs.Discovery().ServerResourcesForGroupVersion("monitoring.coreos.com/v1"); err == nil {
			plan.Warnings = append(plan.Warnings, "prometheus-operator's CRDs are installed in this cluster — an operator-managed Prometheus probably exists or is expected. Check before adding a second one.")
		}
	}
	if opts.Profile == metricsstack.ProfileLite {
		respondJSON(w, plan)
		return
	}
	if opts.StorageSize != "none" {
		scs, err := k8s.ListStorageClasses(ctx, cs)
		switch {
		case err != nil:
			plan.Notes = append(plan.Notes, "Couldn't read the storage classes ("+statusMessage(err)+") — the install will show the real problem if there is none.")
		case opts.StorageClass != "" && !hasStorageClass(scs, opts.StorageClass):
			plan.Warnings = append(plan.Warnings, fmt.Sprintf("Storage class %q doesn't exist in this cluster.", opts.StorageClass))
		case opts.StorageClass == "" && !hasDefaultStorageClass(scs):
			plan.Warnings = append(plan.Warnings, "The cluster has no default storage class, so Prometheus's volume would stay Pending. Pick a storage class, or choose “no volume” (metrics are lost when the pod restarts).")
		}
	} else {
		plan.Warnings = append(plan.Warnings, "Without a volume, metrics history is lost whenever Prometheus's pod restarts.")
	}
	if nodes, err := k8s.ListNodes(ctx, cs); err == nil && len(nodes) > 0 {
		var mem, cpu int64
		for _, n := range nodes {
			mem += n.Status.Allocatable.Memory().Value()
			cpu += n.Status.Allocatable.Cpu().MilliValue()
		}
		plan.Notes = append(plan.Notes, fmt.Sprintf(
			"Needs about 0.2 CPU and 0.5–1.5 GiB of memory in total (Prometheus ≈ 0.1 CPU / 256 MiB–1 GiB, kube-state-metrics, plus node-exporter ≈ 20m / 32 MiB on every node). The cluster has %d CPU(s) and %d GiB allocatable.",
			cpu/1000, mem>>30))
	}
	switch opts.Profile {
	case metricsstack.ProfileFull:
		plan.Notes = append(plan.Notes,
			fmt.Sprintf("Metrics are kept for %d days. Installs the operator, Prometheus, Alertmanager, kube-state-metrics, node-exporter and the default alert rules — no Grafana, and the control-plane monitors (etcd, scheduler, controller-manager, kube-proxy) are off because k3s and managed clusters don't expose them.", opts.RetentionDay))
	default:
		am := "no Alertmanager"
		if opts.Alertmanager {
			am = "with Alertmanager (its alerts feed the bell in the top bar; no alert rules are defined — add your own, or use “full”)"
		}
		plan.Notes = append(plan.Notes,
			fmt.Sprintf("Metrics are kept for %d days. This installs Prometheus, kube-state-metrics and node-exporter, %s, and no Grafana.", opts.RetentionDay, am))
	}
	respondJSON(w, plan)
}

func serviceOf(l *PrometheusLocation) string {
	if l == nil {
		return ""
	}
	return l.Service
}
