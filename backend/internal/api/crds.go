package api

import (
	"net/http"
	"sort"

	"github.com/go-chi/chi/v5"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"

	"github.com/shiraku/vinaire/backend/internal/k8s"
)

// CRDRow matches the frontend's CRDRow type (frontend/src/lib/types.ts).
type CRDRow struct {
	Name       string `json:"name"`
	Group      string `json:"group"`
	Version    string `json:"version"`
	Kind       string `json:"kind"`
	Plural     string `json:"plural"`
	Namespaced bool   `json:"namespaced"`
	Age        string `json:"age"`
}

// CRDs handles GET /api/v1/crds — discovers every CustomResourceDefinition
// installed on the cluster. No hardcoded per-CRD Go code: the dynamic
// client (see resolveDynamic) can list/get any of them once the frontend
// picks one from this list.
func (h *Handler) CRDs(w http.ResponseWriter, r *http.Request) {
	_, apiext, ok := h.resolveDynamic(w, r)
	if !ok {
		return
	}

	crds, err := k8s.ListCRDs(r.Context(), apiext)
	if err != nil {
		respondK8sError(w, err)
		return
	}

	rows := make([]CRDRow, 0, len(crds))
	for _, crd := range crds {
		version := k8s.CRDStorageVersion(crd)
		if version == "" {
			// Should never happen (every CRD has exactly one storage
			// version) — skip rather than advertise a CRD this backend
			// can't actually build a GVR for.
			continue
		}
		rows = append(rows, CRDRow{
			Name:       crd.Name,
			Group:      crd.Spec.Group,
			Version:    version,
			Kind:       crd.Spec.Names.Kind,
			Plural:     crd.Spec.Names.Plural,
			Namespaced: crd.Spec.Scope == apiextensionsv1.NamespaceScoped,
			Age:        formatAge(crd.CreationTimestamp.Time),
		})
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].Name < rows[j].Name })
	respondJSON(w, rows)
}

// CRDInstanceRow matches the frontend's CRDInstanceRow type — the generic
// fallback shape (name/namespace/age) this phase deliberately uses instead
// of trying to honor each CRD's own printer columns (see this phase's own
// risk note in ROADMAP.md).
type CRDInstanceRow struct {
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
	Age       string `json:"age"`
}

// CRDInstances handles GET /api/v1/crds/{crdName}/instances?namespace=.
func (h *Handler) CRDInstances(w http.ResponseWriter, r *http.Request) {
	dyn, apiext, ok := h.resolveDynamic(w, r)
	if !ok {
		return
	}

	crdName := chi.URLParam(r, "crdName")
	namespace := r.URL.Query().Get("namespace")

	crd, err := k8s.GetCRD(r.Context(), apiext, crdName)
	if err != nil {
		respondK8sError(w, err)
		return
	}

	instances, err := k8s.ListCRDInstances(r.Context(), dyn, *crd, namespace)
	if err != nil {
		respondK8sError(w, err)
		return
	}

	rows := make([]CRDInstanceRow, 0, len(instances))
	for _, item := range instances {
		rows = append(rows, CRDInstanceRow{
			Name:      item.GetName(),
			Namespace: item.GetNamespace(),
			Age:       formatAge(item.GetCreationTimestamp().Time),
		})
	}
	respondJSON(w, rows)
}

// CRDInstanceDetailResponse matches the frontend's CRDInstanceDetail type.
type CRDInstanceDetailResponse struct {
	Name      string            `json:"name"`
	Namespace string            `json:"namespace"`
	Kind      string            `json:"kind"`
	Age       string            `json:"age"`
	Labels    map[string]string `json:"labels"`
	YAML      string            `json:"yaml"`
}

// CRDInstanceDetail handles
// GET /api/v1/crds/{crdName}/instances/{name}?namespace=.
func (h *Handler) CRDInstanceDetail(w http.ResponseWriter, r *http.Request) {
	dyn, apiext, ok := h.resolveDynamic(w, r)
	if !ok {
		return
	}

	crdName := chi.URLParam(r, "crdName")
	name := chi.URLParam(r, "name")
	namespace := r.URL.Query().Get("namespace")

	crd, err := k8s.GetCRD(r.Context(), apiext, crdName)
	if err != nil {
		respondK8sError(w, err)
		return
	}

	obj, err := k8s.GetCRDInstance(r.Context(), dyn, *crd, namespace, name)
	if err != nil {
		respondK8sError(w, err)
		return
	}

	labels := obj.GetLabels()
	if labels == nil {
		labels = map[string]string{}
	}

	respondJSON(w, CRDInstanceDetailResponse{
		Name:      obj.GetName(),
		Namespace: obj.GetNamespace(),
		Kind:      obj.GetKind(),
		Age:       formatAge(obj.GetCreationTimestamp().Time),
		Labels:    labels,
		YAML:      marshalYAML(obj.Object),
	})
}
