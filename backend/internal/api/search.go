package api

import (
	"net/http"
	"strings"

	"github.com/shiraku/vinaire/backend/internal/k8s"
)

// searchResultLimit caps the total number of results across every resource
// kind, so a broad/common substring (e.g. a single letter) on a large
// cluster doesn't return an unbounded list.
const searchResultLimit = 50

// SearchResult matches the frontend's SearchResult type — a resource kind
// tag plus enough to render a row and navigate to it. Href points at the
// resource's own detail page when one exists; for kinds without a
// per-object detail page (ConfigMaps, Secrets, Ingresses) it points at
// their list page instead.
type SearchResult struct {
	Kind      string `json:"kind"`
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
	Href      string `json:"href"`
}

// Search handles GET /api/v1/search?q=... — a substring match on name
// across every already-implemented, browsable resource kind. Each kind is
// listed live (the same List() call its own list endpoint already makes),
// not read from a shared cache, so results never lag behind the cluster's
// actual state.
func (h *Handler) Search(w http.ResponseWriter, r *http.Request) {
	_, clientset, ok := h.resolveCluster(w, r)
	if !ok {
		return
	}

	query := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("q")))
	if query == "" {
		respondJSON(w, []SearchResult{})
		return
	}

	ctx := r.Context()
	results := make([]SearchResult, 0, searchResultLimit)

	add := func(kind, name, namespace, href string) bool {
		if !strings.Contains(strings.ToLower(name), query) {
			return len(results) < searchResultLimit
		}
		results = append(results, SearchResult{Kind: kind, Name: name, Namespace: namespace, Href: href})
		return len(results) < searchResultLimit
	}

	if pods, err := k8s.ListPods(ctx, clientset, ""); err == nil {
		for _, o := range pods {
			if !add("Pod", o.Name, o.Namespace, detailHref("/workloads/pods/detail", o.Namespace, o.Name)) {
				respondJSON(w, results)
				return
			}
		}
	}
	if deployments, err := k8s.ListDeployments(ctx, clientset, ""); err == nil {
		for _, o := range deployments {
			if !add("Deployment", o.Name, o.Namespace, detailHref("/workloads/deployments/detail", o.Namespace, o.Name)) {
				respondJSON(w, results)
				return
			}
		}
	}
	if statefulSets, err := k8s.ListStatefulSets(ctx, clientset, ""); err == nil {
		for _, o := range statefulSets {
			if !add("StatefulSet", o.Name, o.Namespace, detailHref("/workloads/statefulsets/detail", o.Namespace, o.Name)) {
				respondJSON(w, results)
				return
			}
		}
	}
	if daemonSets, err := k8s.ListDaemonSets(ctx, clientset, ""); err == nil {
		for _, o := range daemonSets {
			if !add("DaemonSet", o.Name, o.Namespace, detailHref("/workloads/daemonsets/detail", o.Namespace, o.Name)) {
				respondJSON(w, results)
				return
			}
		}
	}
	if jobs, err := k8s.ListJobs(ctx, clientset, ""); err == nil {
		for _, o := range jobs {
			if !add("Job", o.Name, o.Namespace, detailHref("/workloads/jobs/detail", o.Namespace, o.Name)) {
				respondJSON(w, results)
				return
			}
		}
	}
	if nodes, err := k8s.ListNodes(ctx, clientset); err == nil {
		for _, o := range nodes {
			if !add("Node", o.Name, "", "/nodes/detail?name="+o.Name) {
				respondJSON(w, results)
				return
			}
		}
	}
	if pvcs, err := k8s.ListPersistentVolumeClaims(ctx, clientset, ""); err == nil {
		for _, o := range pvcs {
			if !add("PVC", o.Name, o.Namespace, detailHref("/storage/pvcs/detail", o.Namespace, o.Name)) {
				respondJSON(w, results)
				return
			}
		}
	}
	if configMaps, err := k8s.ListConfigMaps(ctx, clientset, ""); err == nil {
		for _, o := range configMaps {
			if !add("ConfigMap", o.Name, o.Namespace, "/configmaps") {
				respondJSON(w, results)
				return
			}
		}
	}
	if secrets, err := k8s.ListSecrets(ctx, clientset, ""); err == nil {
		for _, o := range secrets {
			if !add("Secret", o.Name, o.Namespace, "/secrets") {
				respondJSON(w, results)
				return
			}
		}
	}
	if ingresses, err := k8s.ListIngresses(ctx, clientset, ""); err == nil {
		for _, o := range ingresses {
			if !add("Ingress", o.Name, o.Namespace, "/network") {
				respondJSON(w, results)
				return
			}
		}
	}

	respondJSON(w, results)
}
