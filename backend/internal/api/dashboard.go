package api

import (
	"context"
	"net/http"
	"sort"
	"strconv"

	apiextensionsclientset "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"

	"github.com/shiraku/vinaire/backend/internal/k8s"
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
// PVC-near-capacity and CPU/Memory deliberately aren't included here —
// they stay explicit frontend-side "unavailable" stubs at this stage (see
// this phase's own scope note in ROADMAP.md): Phase 13/16 built the real
// data sources, but wiring them into the Dashboard isn't part of this
// phase.
type DashboardSummaryResponse struct {
	PodsDown        int                 `json:"podsDown"`
	PodsDownSample  []string            `json:"podsDownSample"`
	HealthyPods     int                 `json:"healthyPods"`
	Namespaces      []NamespacePodCount `json:"namespaces"`
	ArgoCDAvailable bool                `json:"argoCDAvailable"`
	DriftedCount    int                 `json:"driftedCount"`
	DriftedSample   []string            `json:"driftedSample"`
}

// DashboardSummary handles GET /api/v1/dashboard/summary — aggregates
// pods-down/healthy-pods counts and per-namespace pod counts from the same
// pod list Phase 1 already fetches (no new Kubernetes API surface for
// that), plus an ArgoCD Application-drift check.
func (h *Handler) DashboardSummary(w http.ResponseWriter, r *http.Request) {
	_, clientset, ok := h.resolveCluster(w, r)
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

	// A discarding ResponseWriter: resolveDynamic writes its own HTTP error
	// on failure, but an ArgoCD-detection hiccup should only make this one
	// field "unavailable," never fail the whole dashboard summary.
	if dyn, apiext, ok := h.resolveDynamic(&discardResponseWriter{}, r); ok {
		resp.ArgoCDAvailable, resp.DriftedCount, resp.DriftedSample = argoCDDrift(r.Context(), dyn, apiext)
	}

	respondJSON(w, resp)
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
