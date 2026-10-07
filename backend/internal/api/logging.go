package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"time"

	authzv1 "k8s.io/api/authorization/v1"
	storagev1 "k8s.io/api/storage/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"github.com/shiraku/vinaire/backend/internal/k8s"
	"github.com/shiraku/vinaire/backend/internal/logging"
)

// The logs stack (Roadmap Phase 45): find a Loki in the cluster, tell the UI what is
// and isn't there, describe the one-click install, and search logs through Loki.
//
// Nothing here installs anything: the plan is handed to the Marketplace's Helm
// install (as the signed-in user, preflight and progress included). And Loki is
// reached through the Kubernetes API's service proxy *as the caller* — so it works
// from anywhere (no ingress, no in-cluster DNS) and the cluster's RBAC decides who
// may query it — unless the operator configured LOKI_URL, which wins.

// LokiLocation is where a Loki Service lives.
type LokiLocation struct {
	Namespace string `json:"namespace"`
	Service   string `json:"service"`
	Port      int32  `json:"port"`
}

// LoggingStatus is GET /api/v1/logging/status.
type LoggingStatus struct {
	// State: "none" (nothing), "loki-only" (can store, nothing ships logs to it),
	// "ready" (Loki and a collector), "configured" (LOKI_URL points somewhere).
	State      string        `json:"state"`
	Loki       *LokiLocation `json:"loki,omitempty"`
	Collectors []string      `json:"collectors,omitempty"` // "namespace/name"
	// Flowing: Loki answers and holds logs with the labels the search needs.
	Flowing bool   `json:"flowing"`
	Detail  string `json:"detail,omitempty"`
	// ManagedByVinaire: the releases the one-click install names ("loki", "alloy").
	ManagedByVinaire bool `json:"managedByVinaire"`
	ConfiguredURL    bool `json:"configuredUrl"`
}

func findLoki(ctx context.Context, cs kubernetes.Interface) (*LokiLocation, error) {
	list, err := cs.CoreV1().Services("").List(ctx, metav1.ListOptions{LabelSelector: "app.kubernetes.io/name=loki"})
	if err != nil {
		return nil, err
	}
	var best *LokiLocation
	for _, s := range list.Items {
		ports := make([]int32, 0, len(s.Spec.Ports))
		for _, p := range s.Spec.Ports {
			ports = append(ports, p.Port)
		}
		port, ok := logging.IsLokiService(s.Labels, s.Spec.ClusterIP, ports)
		if !ok {
			continue
		}
		cand := &LokiLocation{Namespace: s.Namespace, Service: s.Name, Port: port}
		// prefer the direct query port over a gateway, and our own install
		if best == nil || (port == logging.LokiPort && best.Port != logging.LokiPort) {
			best = cand
		}
	}
	return best, nil
}

func findCollectors(ctx context.Context, cs kubernetes.Interface) ([]string, error) {
	list, err := cs.AppsV1().DaemonSets("").List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	var out []string
	for _, d := range list.Items {
		if logging.IsCollector(d.Labels) {
			out = append(out, d.Namespace+"/"+d.Name)
		}
	}
	sort.Strings(out)
	return out, nil
}

// lokiGet queries a Loki Service through the Kubernetes API's service proxy, with
// the caller's own identity.
func lokiGet(ctx context.Context, cs kubernetes.Interface, loc *LokiLocation, path string, params map[string]string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	return cs.CoreV1().Services(loc.Namespace).ProxyGet("http", loc.Service, strconv.Itoa(int(loc.Port)), path, params).DoRaw(ctx)
}

// LoggingStatus handles GET /api/v1/logging/status.
func (h *Handler) LoggingStatus(w http.ResponseWriter, r *http.Request) {
	_, cs, ok := h.resolveCluster(w, r)
	if !ok {
		return
	}
	ctx := r.Context()
	st := LoggingStatus{State: "none"}
	if h.LokiURL != "" {
		st.State, st.ConfiguredURL = "configured", true
		_, err := fetchUpstream(ctx, h.LokiURL, "/ready", nil)
		st.Flowing = err == nil
		if err != nil {
			st.Detail = "LOKI_URL is set but doesn't answer: " + err.Error()
		}
		respondJSON(w, st)
		return
	}
	loc, err := findLoki(ctx, cs)
	if err != nil {
		respondK8sError(w, err)
		return
	}
	collectors, err := findCollectors(ctx, cs)
	if err != nil {
		respondK8sError(w, err)
		return
	}
	st.Loki, st.Collectors = loc, collectors
	switch {
	case loc != nil && len(collectors) > 0:
		st.State = "ready"
	case loc != nil:
		st.State = "loki-only"
	}
	st.ManagedByVinaire = loc != nil && loc.Service == logging.LokiRelease && loc.Namespace != ""
	if loc != nil {
		body, err := lokiGet(ctx, cs, loc, "/loki/api/v1/labels", map[string]string{
			"start": strconv.FormatInt(time.Now().Add(-time.Hour).UnixNano(), 10),
		})
		switch {
		case err != nil:
			st.Detail = lokiErrorReason(err, loc)
		default:
			var lr struct {
				Data []string `json:"data"`
			}
			if json.Unmarshal(body, &lr) == nil {
				for _, l := range lr.Data {
					if l == "pod" {
						st.Flowing = true
					}
				}
			}
			if !st.Flowing {
				st.Detail = "Loki answers but holds no pod logs yet — a collector needs a minute after it starts."
			}
		}
	}
	respondJSON(w, st)
}

// LoggingPlan is GET /api/v1/logging/plan.
type LoggingPlan struct {
	Steps    []logging.InstallStep `json:"steps"`
	Notes    []string              `json:"notes"`
	Warnings []string              `json:"warnings"`
}

// LoggingPlan handles GET /api/v1/logging/plan?namespace=&retentionDays=&storageSize=&storageClass=:
// what the one-click install would install (the pinned charts and values), and
// what to know before pressing the button.
func (h *Handler) LoggingPlan(w http.ResponseWriter, r *http.Request) {
	_, cs, ok := h.resolveCluster(w, r)
	if !ok {
		return
	}
	q := r.URL.Query()
	days, _ := strconv.Atoi(q.Get("retentionDays"))
	opts, err := logging.Options{
		Namespace: q.Get("namespace"), RetentionDay: days,
		StorageSize: q.Get("storageSize"), StorageClass: q.Get("storageClass"),
	}.Normalize()
	if err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}
	ctx := r.Context()
	plan := LoggingPlan{Steps: logging.Plan(opts), Notes: []string{}, Warnings: []string{}}

	if loc, _ := findLoki(ctx, cs); loc != nil {
		plan.Warnings = append(plan.Warnings, fmt.Sprintf(
			"A Loki is already running (%s/%s). Installing a second one would split your logs — use it instead.", loc.Namespace, loc.Service))
	}
	if opts.StorageSize != "none" {
		scs, err := k8s.ListStorageClasses(ctx, cs)
		switch {
		case err != nil:
			plan.Notes = append(plan.Notes, "Couldn't read the storage classes ("+statusMessage(err)+") — the install will show the real problem if there is none.")
		case opts.StorageClass != "" && !hasStorageClass(scs, opts.StorageClass):
			plan.Warnings = append(plan.Warnings, fmt.Sprintf("Storage class %q doesn't exist in this cluster.", opts.StorageClass))
		case opts.StorageClass == "" && !hasDefaultStorageClass(scs):
			plan.Warnings = append(plan.Warnings, "The cluster has no default storage class, so Loki's volume would stay Pending. Pick a storage class, or choose “no volume” (logs are lost when the pod restarts).")
		}
	} else {
		plan.Warnings = append(plan.Warnings, "Without a volume, logs are lost whenever Loki's pod restarts.")
	}
	if nodes, err := k8s.ListNodes(ctx, cs); err == nil && len(nodes) > 0 {
		var mem, cpu int64
		for _, n := range nodes {
			mem += n.Status.Allocatable.Memory().Value()
			cpu += n.Status.Allocatable.Cpu().MilliValue()
		}
		plan.Notes = append(plan.Notes, fmt.Sprintf(
			"Needs about 0.5 CPU and 1 GiB of memory in total (Loki ≈ 0.1 CPU / 256 MiB–1 GiB, plus Alloy ≈ 50m / 128 MiB on every node). The cluster has %d CPU(s) and %d GiB allocatable.",
			cpu/1000, mem>>30))
	}
	plan.Notes = append(plan.Notes,
		fmt.Sprintf("Logs are kept for %d days; this is a single-pod Loki for small clusters, not a distributed one.", opts.RetentionDay))
	respondJSON(w, plan)
}

func hasStorageClass(scs []storagev1.StorageClass, name string) bool {
	for _, s := range scs {
		if s.Name == name {
			return true
		}
	}
	return false
}

func hasDefaultStorageClass(scs []storagev1.StorageClass) bool {
	for _, s := range scs {
		if s.Annotations["storageclass.kubernetes.io/is-default-class"] == "true" ||
			s.Annotations["storageclass.beta.kubernetes.io/is-default-class"] == "true" {
			return true
		}
	}
	return false
}

// ---- searching ----------------------------------------------------------------

// callerCan asks the cluster, as the caller, whether they may do verb on
// resource/subresource in ns — the check a log search owes before it reads logs
// out of a store the caller's own token never touches.
func callerCan(ctx context.Context, cs kubernetes.Interface, verb, group, resource, subresource, namespace, name string) (bool, error) {
	res, err := cs.AuthorizationV1().SelfSubjectAccessReviews().Create(ctx, &authzv1.SelfSubjectAccessReview{
		Spec: authzv1.SelfSubjectAccessReviewSpec{ResourceAttributes: &authzv1.ResourceAttributes{
			Verb: verb, Group: group, Resource: resource, Subresource: subresource, Namespace: namespace, Name: name,
		}},
	}, metav1.CreateOptions{})
	if err != nil {
		return false, err
	}
	return res.Status.Allowed, nil
}

// searchLoki runs one LogQL range query, through LOKI_URL if the operator set it,
// otherwise through the service proxy to the Loki found in the cluster.
func (h *Handler) searchLoki(ctx context.Context, cs kubernetes.Interface, params url.Values) ([]byte, string) {
	if h.LokiURL != "" {
		body, err := fetchUpstream(ctx, h.LokiURL, "/loki/api/v1/query_range", params)
		if err != nil {
			return nil, err.Error()
		}
		return body, ""
	}
	loc, err := findLoki(ctx, cs)
	if err != nil {
		return nil, "couldn't look for Loki: " + statusMessage(err)
	}
	if loc == nil {
		return nil, "no Loki in this cluster — install the logs stack (Logs stack page) or set LOKI_URL"
	}
	p := map[string]string{}
	for k := range params {
		p[k] = params.Get(k)
	}
	body, err := lokiGet(ctx, cs, loc, "/loki/api/v1/query_range", p)
	if err != nil {
		return nil, lokiErrorReason(err, loc)
	}
	return body, ""
}

// lokiErrorReason explains why Loki couldn't be reached. The common case is the
// caller lacking `get` on services/proxy for it, which the API server reports as a
// bare "unknown" for a proxy call.
func lokiErrorReason(err error, loc *LokiLocation) string {
	if apierrors.IsForbidden(err) {
		return fmt.Sprintf("You don't have permission to reach Loki through the API server — it needs `get` on services/proxy for %q in namespace %q (the chart's -viewer role grants it for the standard Loki names).", loc.Service, loc.Namespace)
	}
	return "Loki didn't answer: " + statusMessage(err)
}
