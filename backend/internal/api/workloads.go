// Package api holds HTTP handlers that shape internal/k8s's raw listers
// into the JSON responses the frontend expects. Response mapping and
// status/color derivation (business logic) lives here; internal/k8s stays
// a thin, read-only wrapper around the Kubernetes API itself.
package api

import (
	"fmt"
	"net/http"
	"net/url"

	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apiextensionsclientset "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

	"github.com/shiraku/vinaire/backend/internal/audit"
	"github.com/shiraku/vinaire/backend/internal/authn"
	"github.com/shiraku/vinaire/backend/internal/clusters"
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

// Handler holds the dependencies HTTP handlers need. Clusters may be empty
// if the backend booted without a single reachable cluster (see main.go) —
// every handler must go through resolveCluster before using one. A given
// Entry's MetricsClientset/GatewayClientset may still be nil even when its
// Clientset isn't (no metrics-server / no Gateway API CRDs) — handlers
// using them must degrade gracefully rather than fail the whole request.
type Handler struct {
	Clusters *clusters.Registry
	Auth     *authn.Manager

	// PrometheusURL/LokiURL/AlertmanagerURL back the Roadmap Phase 16
	// observability proxies (internal/api/observability.go) — empty means
	// "not configured," and every handler using one degrades to an
	// explicit `available: false` response rather than failing.
	PrometheusURL   string
	LokiURL         string
	AlertmanagerURL string

	// Audit is the durable sink for write actions (Phase 9) and secret
	// reveals (Phase 6) — see internal/audit and Handler.auditLog. Set
	// directly on the Handler after construction (main.go), since it's a
	// single additional piece of optional infrastructure, not part of the
	// per-cluster/per-request resolution the constructor params above are.
	Audit *audit.Logger
}

// ObservabilityConfig groups the optional observability-stack URLs
// (Roadmap Phase 16) NewHandler takes, since they're conceptually one unit
// distinct from the cluster registry/auth manager.
type ObservabilityConfig struct {
	PrometheusURL   string
	LokiURL         string
	AlertmanagerURL string
}

func NewHandler(registry *clusters.Registry, auth *authn.Manager, obs ObservabilityConfig) *Handler {
	return &Handler{
		Clusters:        registry,
		Auth:            auth,
		PrometheusURL:   obs.PrometheusURL,
		LokiURL:         obs.LokiURL,
		AlertmanagerURL: obs.AlertmanagerURL,
	}
}

// clusterHeader is how the frontend picks which configured cluster a
// request targets (Roadmap Phase 11). Empty/absent means "the default
// cluster" (the first one configured).
const clusterHeader = "X-Cluster"

// resolveRESTConfig picks the cluster.Entry this request targets and
// returns the *rest.Config the caller should use against it — authenticated
// as the caller's own logged-in identity outside ModeDisabled (Roadmap
// Phase 10 — see internal/clusters.Entry.RESTConfigForToken), not as the
// backend's own service account: authorization is therefore whatever the
// real Kubernetes API/RBAC decides on each call, not a separate app-level
// check here. It writes the response itself and returns ok=false on any
// failure (unknown cluster, unreachable cluster, or no session).
// resolveCluster and resolveDynamic both build their specific client type
// from this shared resolution so the cluster/auth logic lives in one place.
func (h *Handler) resolveRESTConfig(w http.ResponseWriter, r *http.Request) (*clusters.Entry, *rest.Config, bool) {
	name := r.Header.Get(clusterHeader)
	entry, ok := h.Clusters.Resolve(name)
	if !ok {
		respondError(w, http.StatusBadRequest, fmt.Sprintf("unknown cluster %q", name))
		return nil, nil, false
	}
	if entry == nil || entry.Clientset == nil {
		respondError(w, http.StatusServiceUnavailable, "kubernetes client unavailable")
		return nil, nil, false
	}

	sess, authenticated := authn.FromContext(r.Context())
	if !authenticated {
		respondError(w, http.StatusUnauthorized, "unauthenticated")
		return nil, nil, false
	}
	if h.Auth.Mode == authn.ModeDisabled {
		return entry, entry.RESTConfig, true
	}
	return entry, entry.RESTConfigForToken(sess.K8sToken), true
}

// resolveCluster is resolveRESTConfig plus building a typed Kubernetes
// clientset from it. In ModeDisabled it reuses the Entry's own pre-built
// Clientset rather than constructing an equivalent new one.
func (h *Handler) resolveCluster(w http.ResponseWriter, r *http.Request) (*clusters.Entry, *kubernetes.Clientset, bool) {
	entry, cfg, ok := h.resolveRESTConfig(w, r)
	if !ok {
		return nil, nil, false
	}
	if h.Auth.Mode == authn.ModeDisabled {
		return entry, entry.Clientset, true
	}

	clientset, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to build kubernetes client: "+err.Error())
		return nil, nil, false
	}
	return entry, clientset, true
}

// resolveDynamic is resolveRESTConfig plus building the dynamic and
// apiextensions clients Phase 15's generic CRD viewer needs — there's no
// typed Go struct for an arbitrary CRD instance, so these clients work in
// unstructured data instead of the typed clientset every other handler
// uses.
func (h *Handler) resolveDynamic(w http.ResponseWriter, r *http.Request) (dynamic.Interface, *apiextensionsclientset.Clientset, bool) {
	_, cfg, ok := h.resolveRESTConfig(w, r)
	if !ok {
		return nil, nil, false
	}

	dyn, err := dynamic.NewForConfig(cfg)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to build dynamic client: "+err.Error())
		return nil, nil, false
	}
	apiext, err := apiextensionsclientset.NewForConfig(cfg)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to build apiextensions client: "+err.Error())
		return nil, nil, false
	}
	return dyn, apiext, true
}

// detailHref builds a link to a resource's static detail page as
// `<base>?namespace=&name=` rather than a `[namespace]/[name]` dynamic
// route segment — the frontend is a static export (see AGENTS.md's
// "Rendering model"), which can't pre-render a path for a resource name it
// doesn't know at build time.
func detailHref(base, namespace, name string) string {
	return fmt.Sprintf("%s?namespace=%s&name=%s", base, url.QueryEscape(namespace), url.QueryEscape(name))
}

func (h *Handler) Pods(w http.ResponseWriter, r *http.Request) {
	_, clientset, ok := h.resolveCluster(w, r)
	if !ok {
		return
	}
	namespace := r.URL.Query().Get("namespace")

	pods, err := k8s.ListPods(r.Context(), clientset, namespace)
	if err != nil {
		respondK8sError(w, err)
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
		Href:        detailHref("/workloads/pods/detail", pod.Namespace, pod.Name),
	}
}

func (h *Handler) Deployments(w http.ResponseWriter, r *http.Request) {
	_, clientset, ok := h.resolveCluster(w, r)
	if !ok {
		return
	}
	namespace := r.URL.Query().Get("namespace")

	deployments, err := k8s.ListDeployments(r.Context(), clientset, namespace)
	if err != nil {
		respondK8sError(w, err)
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
		Href:        detailHref("/workloads/deployments/detail", d.Namespace, d.Name),
	}
}

func (h *Handler) StatefulSets(w http.ResponseWriter, r *http.Request) {
	_, clientset, ok := h.resolveCluster(w, r)
	if !ok {
		return
	}
	namespace := r.URL.Query().Get("namespace")

	sets, err := k8s.ListStatefulSets(r.Context(), clientset, namespace)
	if err != nil {
		respondK8sError(w, err)
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
		Href:        detailHref("/workloads/statefulsets/detail", s.Namespace, s.Name),
	}
}

func (h *Handler) DaemonSets(w http.ResponseWriter, r *http.Request) {
	_, clientset, ok := h.resolveCluster(w, r)
	if !ok {
		return
	}
	namespace := r.URL.Query().Get("namespace")

	sets, err := k8s.ListDaemonSets(r.Context(), clientset, namespace)
	if err != nil {
		respondK8sError(w, err)
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
		Href:        detailHref("/workloads/daemonsets/detail", ds.Namespace, ds.Name),
	}
}

func (h *Handler) Jobs(w http.ResponseWriter, r *http.Request) {
	_, clientset, ok := h.resolveCluster(w, r)
	if !ok {
		return
	}
	namespace := r.URL.Query().Get("namespace")

	jobs, err := k8s.ListJobs(r.Context(), clientset, namespace)
	if err != nil {
		respondK8sError(w, err)
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
	completions := jobCompletions(j)
	status, color := jobStatus(j, completions)

	return WorkloadRow{
		Name:        j.Name,
		Namespace:   j.Namespace,
		Ready:       fmt.Sprintf("%d/%d", j.Status.Succeeded, completions),
		Status:      status,
		StatusColor: string(color),
		Restarts:    "—",
		Age:         formatAge(j.CreationTimestamp.Time),
		Href:        detailHref("/workloads/jobs/detail", j.Namespace, j.Name),
	}
}

// jobCompletions returns a Job's desired completion count, defaulting to 1
// the same way the Kubernetes API itself does when spec.completions is unset.
func jobCompletions(j batchv1.Job) int32 {
	if j.Spec.Completions != nil {
		return *j.Spec.Completions
	}
	return 1
}

// jobStatus mirrors `kubectl get jobs`' STATUS column: Failed takes priority
// over a partial success count, then Complete once every completion has
// succeeded, otherwise still Running.
func jobStatus(j batchv1.Job, completions int32) (status string, color StatusColor) {
	switch {
	case j.Status.Failed > 0:
		return "Failed", StatusErr
	case j.Status.Succeeded >= completions:
		return "Complete", StatusOK
	default:
		return "Running", StatusWarn
	}
}
