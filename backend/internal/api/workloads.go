// Package api holds HTTP handlers that shape internal/k8s's raw listers
// into the JSON responses the frontend expects. Response mapping and
// status/color derivation (business logic) lives here; internal/k8s stays
// a thin, read-only wrapper around the Kubernetes API itself.
package api

import (
	"fmt"
	"net/http"
	"slices"

	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"

	"github.com/shiraku/vinaire/backend/internal/authz"
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
	Policy   *authz.Policy
}

func NewHandler(registry *clusters.Registry, policy *authz.Policy) *Handler {
	return &Handler{Clusters: registry, Policy: policy}
}

// clusterHeader is how the frontend picks which configured cluster a
// request targets (Roadmap Phase 11). Empty/absent means "the default
// cluster" (the first one configured).
const clusterHeader = "X-Cluster"

// resolveCluster picks the cluster.Entry this request targets. It writes
// the response itself and returns ok=false if the named cluster doesn't
// exist, or no cluster is configured/reachable at all.
func (h *Handler) resolveCluster(w http.ResponseWriter, r *http.Request) (*clusters.Entry, bool) {
	name := r.Header.Get(clusterHeader)
	entry, ok := h.Clusters.Resolve(name)
	if !ok {
		respondError(w, http.StatusBadRequest, fmt.Sprintf("unknown cluster %q", name))
		return nil, false
	}
	if entry == nil || entry.Clientset == nil {
		respondError(w, http.StatusServiceUnavailable, "kubernetes client unavailable")
		return nil, false
	}
	return entry, true
}

// authorize checks the authenticated caller (Roadmap Phase 10) may perform
// `action` given the requested namespace ("" = all namespaces). For a
// specific namespace it enforces access directly (403 if denied). For an
// all-namespaces request it doesn't deny outright — it returns the
// caller's allowed namespace set so the handler can filter results down to
// just those, per ROADMAP.md's "scoped view, not an error" Phase 10 design.
// ok is false (response already written) only when the request should stop.
func (h *Handler) authorize(w http.ResponseWriter, r *http.Request, namespace, action string) (allowed []string, allowAll bool, ok bool) {
	claims, authenticated := authz.FromContext(r.Context())
	if !authenticated {
		respondError(w, http.StatusUnauthorized, "unauthenticated")
		return nil, false, false
	}
	allowed, allowAll = h.Policy.AllowedNamespaces(claims, action)
	if namespace != "" && !allowAll && !slices.Contains(allowed, namespace) {
		respondError(w, http.StatusForbidden, fmt.Sprintf("not authorized for namespace %q", namespace))
		return nil, false, false
	}
	return allowed, allowAll, true
}

// filterByAllowedNamespace narrows rows down to the caller's allowed
// namespaces when the request didn't pin a specific one (namespace == ""),
// so an all-namespaces query still only returns what the caller's groups
// permit — mirrors authorize's "scoped view" behavior for list endpoints.
func filterByAllowedNamespace[T any](rows []T, namespace string, allowed []string, allowAll bool, namespaceOf func(T) string) []T {
	if namespace != "" || allowAll {
		return rows
	}
	filtered := make([]T, 0, len(rows))
	for _, row := range rows {
		if slices.Contains(allowed, namespaceOf(row)) {
			filtered = append(filtered, row)
		}
	}
	return filtered
}

func (h *Handler) Pods(w http.ResponseWriter, r *http.Request) {
	cluster, ok := h.resolveCluster(w, r)
	if !ok {
		return
	}
	namespace := r.URL.Query().Get("namespace")
	allowed, allowAll, ok := h.authorize(w, r, namespace, "read")
	if !ok {
		return
	}

	pods, err := k8s.ListPods(r.Context(), cluster.Clientset, namespace)
	if err != nil {
		respondError(w, http.StatusBadGateway, err.Error())
		return
	}

	rows := make([]WorkloadRow, 0, len(pods))
	for _, pod := range pods {
		rows = append(rows, PodRow(pod))
	}
	rows = filterByAllowedNamespace(rows, namespace, allowed, allowAll, func(row WorkloadRow) string { return row.Namespace })
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
		Href:        fmt.Sprintf("/workloads/pods/%s/%s", pod.Namespace, pod.Name),
	}
}

func (h *Handler) Deployments(w http.ResponseWriter, r *http.Request) {
	cluster, ok := h.resolveCluster(w, r)
	if !ok {
		return
	}
	namespace := r.URL.Query().Get("namespace")
	allowed, allowAll, ok := h.authorize(w, r, namespace, "read")
	if !ok {
		return
	}

	deployments, err := k8s.ListDeployments(r.Context(), cluster.Clientset, namespace)
	if err != nil {
		respondError(w, http.StatusBadGateway, err.Error())
		return
	}

	rows := make([]WorkloadRow, 0, len(deployments))
	for _, d := range deployments {
		rows = append(rows, DeploymentRow(d))
	}
	rows = filterByAllowedNamespace(rows, namespace, allowed, allowAll, func(row WorkloadRow) string { return row.Namespace })
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
		Href:        fmt.Sprintf("/workloads/deployments/%s/%s", d.Namespace, d.Name),
	}
}

func (h *Handler) StatefulSets(w http.ResponseWriter, r *http.Request) {
	cluster, ok := h.resolveCluster(w, r)
	if !ok {
		return
	}
	namespace := r.URL.Query().Get("namespace")
	allowed, allowAll, ok := h.authorize(w, r, namespace, "read")
	if !ok {
		return
	}

	sets, err := k8s.ListStatefulSets(r.Context(), cluster.Clientset, namespace)
	if err != nil {
		respondError(w, http.StatusBadGateway, err.Error())
		return
	}

	rows := make([]WorkloadRow, 0, len(sets))
	for _, s := range sets {
		rows = append(rows, StatefulSetRow(s))
	}
	rows = filterByAllowedNamespace(rows, namespace, allowed, allowAll, func(row WorkloadRow) string { return row.Namespace })
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
		// No StatefulSet detail screen yet — link back to the list.
		Href: "/workloads",
	}
}

func (h *Handler) DaemonSets(w http.ResponseWriter, r *http.Request) {
	cluster, ok := h.resolveCluster(w, r)
	if !ok {
		return
	}
	namespace := r.URL.Query().Get("namespace")
	allowed, allowAll, ok := h.authorize(w, r, namespace, "read")
	if !ok {
		return
	}

	sets, err := k8s.ListDaemonSets(r.Context(), cluster.Clientset, namespace)
	if err != nil {
		respondError(w, http.StatusBadGateway, err.Error())
		return
	}

	rows := make([]WorkloadRow, 0, len(sets))
	for _, ds := range sets {
		rows = append(rows, DaemonSetRow(ds))
	}
	rows = filterByAllowedNamespace(rows, namespace, allowed, allowAll, func(row WorkloadRow) string { return row.Namespace })
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
		// No DaemonSet detail screen yet — link back to the list.
		Href: "/workloads",
	}
}

func (h *Handler) Jobs(w http.ResponseWriter, r *http.Request) {
	cluster, ok := h.resolveCluster(w, r)
	if !ok {
		return
	}
	namespace := r.URL.Query().Get("namespace")
	allowed, allowAll, ok := h.authorize(w, r, namespace, "read")
	if !ok {
		return
	}

	jobs, err := k8s.ListJobs(r.Context(), cluster.Clientset, namespace)
	if err != nil {
		respondError(w, http.StatusBadGateway, err.Error())
		return
	}

	rows := make([]WorkloadRow, 0, len(jobs))
	for _, j := range jobs {
		rows = append(rows, JobRow(j))
	}
	rows = filterByAllowedNamespace(rows, namespace, allowed, allowAll, func(row WorkloadRow) string { return row.Namespace })
	respondJSON(w, rows)
}

// JobRow maps a job to the frontend's row shape. Exported so internal/stream
// can reuse the exact same mapping for live WebSocket diffs.
func JobRow(j batchv1.Job) WorkloadRow {
	completions := int32(1)
	if j.Spec.Completions != nil {
		completions = *j.Spec.Completions
	}

	var status string
	var color StatusColor
	switch {
	case j.Status.Failed > 0:
		status, color = "Failed", StatusErr
	case j.Status.Succeeded >= completions:
		status, color = "Complete", StatusOK
	default:
		status, color = "Running", StatusWarn
	}

	return WorkloadRow{
		Name:        j.Name,
		Namespace:   j.Namespace,
		Ready:       fmt.Sprintf("%d/%d", j.Status.Succeeded, completions),
		Status:      status,
		StatusColor: string(color),
		Restarts:    "—",
		Age:         formatAge(j.CreationTimestamp.Time),
		// No Job detail screen yet — link back to the list.
		Href: "/workloads",
	}
}
