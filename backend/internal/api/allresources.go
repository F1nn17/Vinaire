package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"sort"
	"strings"

	"github.com/go-chi/chi/v5"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/dynamic"
)

// "All resources": browse any kind the cluster serves — CronJobs, ReplicaSets,
// HPAs, NetworkPolicies, Events, custom resources, whatever discovery lists —
// without a hand-written page for each. Read-only, and as the caller: the list of
// kinds is what *their* discovery returns, every list/get is made with *their*
// token, so RBAC decides what appears. Changing an object goes through the
// existing generic YAML apply (Phase 25), not through anything here.
//
// Columns come from the API server's own Table representation — the same columns
// `kubectl get` prints — so each kind shows what its authors consider useful.
//
// Secrets: the list carries only what kubectl shows (name, type, number of keys,
// age); a single Secret's YAML has its values redacted. Decoded values stay behind
// the audited reveal endpoint (AGENTS.md).

const maxTableRows = 500

var (
	groupRe    = regexp.MustCompile(`^[a-z0-9]([-a-z0-9.]{0,251}[a-z0-9])?$`)
	versionRe  = regexp.MustCompile(`^v[0-9]{1,3}((alpha|beta)[0-9]{1,3})?$`)
	resourceRe = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]{0,62}[a-z0-9])?$`)
	nsRe       = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]{0,61}[a-z0-9])?$`)
	objNameRe  = regexp.MustCompile(`^[A-Za-z0-9][-A-Za-z0-9_.:]{0,252}$`)
)

// APIResourceInfo is one kind in the picker.
type APIResourceInfo struct {
	Group      string   `json:"group"` // "" for the core group
	Version    string   `json:"version"`
	Resource   string   `json:"resource"` // plural, as in the URL
	Kind       string   `json:"kind"`
	Namespaced bool     `json:"namespaced"`
	ShortNames []string `json:"shortNames,omitempty"`
}

// APIResources handles GET /api/v1/api-resources.
func (h *Handler) APIResources(w http.ResponseWriter, r *http.Request) {
	_, disc, ok := h.resolveApplyClients(w, r)
	if !ok {
		return
	}
	lists, err := disc.ServerPreferredResources()
	// A broken aggregated API (metrics-server down, say) makes discovery return the
	// rest *and* an error; show what it did return.
	if err != nil && len(lists) == 0 {
		respondK8sError(w, err)
		return
	}
	out := []APIResourceInfo{}
	for _, l := range lists {
		gv, perr := schema.ParseGroupVersion(l.GroupVersion)
		if perr != nil {
			continue
		}
		for _, res := range l.APIResources {
			if strings.Contains(res.Name, "/") || !hasVerbs(res.Verbs, "list", "get") {
				continue
			}
			out = append(out, APIResourceInfo{
				Group: gv.Group, Version: gv.Version, Resource: res.Name, Kind: res.Kind,
				Namespaced: res.Namespaced, ShortNames: res.ShortNames,
			})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Group != out[j].Group {
			// the core group first, then alphabetical
			if out[i].Group == "" || out[j].Group == "" {
				return out[i].Group == ""
			}
			return out[i].Group < out[j].Group
		}
		return out[i].Resource < out[j].Resource
	})
	respondJSON(w, out)
}

func hasVerbs(have metav1.Verbs, want ...string) bool {
	set := map[string]bool{}
	for _, v := range have {
		set[v] = true
	}
	for _, w := range want {
		if !set[w] {
			return false
		}
	}
	return true
}

// TableColumn / TableRow are the response of a kind's list.
type TableColumn struct {
	Name        string `json:"name"`
	Type        string `json:"type"`
	Description string `json:"description,omitempty"`
}

type TableRow struct {
	Name      string   `json:"name"`
	Namespace string   `json:"namespace,omitempty"`
	Cells     []string `json:"cells"`
}

type TableResponse struct {
	Columns    []TableColumn `json:"columns"`
	Rows       []TableRow    `json:"rows"`
	Namespaced bool          `json:"namespaced"`
	Truncated  bool          `json:"truncated,omitempty"`
}

// gvrFromRequest validates the URL's group/version/resource and resolves the kind
// through discovery (so an unknown or non-listable kind is a 404, not a request
// to a made-up path).
func gvrFromRequest(w http.ResponseWriter, r *http.Request, disc discovery.DiscoveryInterface) (schema.GroupVersionResource, bool, bool) {
	group, version, resource := chi.URLParam(r, "group"), chi.URLParam(r, "version"), chi.URLParam(r, "resource")
	if group == "core" {
		group = ""
	}
	if (group != "" && !groupRe.MatchString(group)) || !versionRe.MatchString(version) || !resourceRe.MatchString(resource) {
		respondError(w, http.StatusBadRequest, "invalid group, version or resource")
		return schema.GroupVersionResource{}, false, false
	}
	gv := schema.GroupVersion{Group: group, Version: version}
	list, err := disc.ServerResourcesForGroupVersion(gv.String())
	if apierrors.IsNotFound(err) {
		respondError(w, http.StatusNotFound, "this cluster doesn't serve "+gv.String())
		return schema.GroupVersionResource{}, false, false
	}
	if err != nil {
		respondK8sError(w, err)
		return schema.GroupVersionResource{}, false, false
	}
	for _, res := range list.APIResources {
		if res.Name == resource {
			return gv.WithResource(resource), res.Namespaced, true
		}
	}
	respondError(w, http.StatusNotFound, fmt.Sprintf("no resource %q in %s", resource, gv))
	return schema.GroupVersionResource{}, false, false
}

// listPath builds the REST path of a kind's collection from validated parts.
func listPath(gvr schema.GroupVersionResource, namespaced bool, namespace string) string {
	var b strings.Builder
	if gvr.Group == "" {
		b.WriteString("/api/" + gvr.Version)
	} else {
		b.WriteString("/apis/" + gvr.Group + "/" + gvr.Version)
	}
	if namespaced && namespace != "" {
		b.WriteString("/namespaces/" + namespace)
	}
	b.WriteString("/" + gvr.Resource)
	return b.String()
}

// ResourceObjects handles GET /api/v1/api-resources/{group}/{version}/{resource}/objects?namespace=.
func (h *Handler) ResourceObjects(w http.ResponseWriter, r *http.Request) {
	_, disc, ok := h.resolveApplyClients(w, r)
	if !ok {
		return
	}
	gvr, namespaced, ok := gvrFromRequest(w, r, disc)
	if !ok {
		return
	}
	namespace := r.URL.Query().Get("namespace")
	if namespace != "" && !nsRe.MatchString(namespace) {
		respondError(w, http.StatusBadRequest, "invalid namespace")
		return
	}
	raw, err := disc.RESTClient().Get().AbsPath(listPath(gvr, namespaced, namespace)).
		SetHeader("Accept", "application/json;as=Table;v=v1;g=meta.k8s.io").
		Param("limit", fmt.Sprint(maxTableRows)).
		Do(r.Context()).Raw()
	if err != nil {
		respondK8sError(w, statusFromBody(raw, err))
		return
	}
	resp, err := tableFromJSON(raw, namespaced)
	if err != nil {
		respondError(w, http.StatusBadGateway, err.Error())
		return
	}
	respondJSON(w, resp)
}

func tableFromJSON(raw []byte, namespaced bool) (*TableResponse, error) {
	var t metav1.Table
	if err := json.Unmarshal(raw, &t); err != nil {
		return nil, fmt.Errorf("reading the table: %w", err)
	}
	resp := &TableResponse{Columns: []TableColumn{}, Rows: []TableRow{}, Namespaced: namespaced, Truncated: t.Continue != ""}
	// Priority 0 are the columns `kubectl get` shows; the rest are `-o wide`.
	keep := []int{}
	for i, c := range t.ColumnDefinitions {
		if c.Priority == 0 {
			keep = append(keep, i)
			resp.Columns = append(resp.Columns, TableColumn{Name: c.Name, Type: c.Type, Description: c.Description})
		}
	}
	for _, row := range t.Rows {
		tr := TableRow{Cells: make([]string, 0, len(keep))}
		var meta struct {
			Metadata struct {
				Name      string `json:"name"`
				Namespace string `json:"namespace"`
			} `json:"metadata"`
		}
		_ = json.Unmarshal(row.Object.Raw, &meta)
		tr.Name, tr.Namespace = meta.Metadata.Name, meta.Metadata.Namespace
		for _, i := range keep {
			if i < len(row.Cells) {
				tr.Cells = append(tr.Cells, cellString(row.Cells[i]))
			} else {
				tr.Cells = append(tr.Cells, "")
			}
		}
		if tr.Name == "" && len(tr.Cells) > 0 {
			tr.Name = tr.Cells[0]
		}
		resp.Rows = append(resp.Rows, tr)
	}
	return resp, nil
}

func cellString(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return x
	case float64:
		if x == float64(int64(x)) {
			return fmt.Sprintf("%d", int64(x))
		}
		return fmt.Sprint(x)
	default:
		b, err := json.Marshal(x)
		if err != nil {
			return fmt.Sprint(x)
		}
		return string(b)
	}
}

// ResourceObjectDetail is one object, for the detail page.
type ResourceObjectDetail struct {
	Name      string            `json:"name"`
	Namespace string            `json:"namespace,omitempty"`
	Kind      string            `json:"kind"`
	Age       string            `json:"age"`
	Labels    map[string]string `json:"labels"`
	YAML      string            `json:"yaml"`
}

// ResourceObject handles
// GET /api/v1/api-resources/{group}/{version}/{resource}/objects/{name}?namespace=.
func (h *Handler) ResourceObject(w http.ResponseWriter, r *http.Request) {
	dyn, disc, ok := h.resolveApplyClients(w, r)
	if !ok {
		return
	}
	gvr, namespaced, ok := gvrFromRequest(w, r, disc)
	if !ok {
		return
	}
	name := chi.URLParam(r, "name")
	namespace := r.URL.Query().Get("namespace")
	if !objNameRe.MatchString(name) || (namespace != "" && !nsRe.MatchString(namespace)) {
		respondError(w, http.StatusBadRequest, "invalid name or namespace")
		return
	}
	if namespaced && namespace == "" {
		respondError(w, http.StatusBadRequest, "this kind is namespaced — pass ?namespace=")
		return
	}
	obj, err := getObject(r.Context(), dyn, gvr, namespaced, namespace, name)
	if err != nil {
		respondK8sError(w, err)
		return
	}
	respondJSON(w, ResourceObjectDetail{
		Name: obj.GetName(), Namespace: obj.GetNamespace(), Kind: obj.GetKind(),
		Age: formatAge(obj.GetCreationTimestamp().Time), Labels: obj.GetLabels(),
		YAML: redactedYAML(obj),
	})
}

func getObject(ctx context.Context, dyn dynamic.Interface, gvr schema.GroupVersionResource, namespaced bool, namespace, name string) (*unstructured.Unstructured, error) {
	ri := dyn.Resource(gvr)
	if namespaced {
		return ri.Namespace(namespace).Get(ctx, name, metav1.GetOptions{})
	}
	return ri.Get(ctx, name, metav1.GetOptions{})
}

// redactedYAML is the object as YAML with a core Secret's values replaced — the
// same rule the apply diff follows (sanitizeForDiff) — but, unlike the diff, with
// metadata kept intact so the editor can show and update it.
func redactedYAML(obj *unstructured.Unstructured) string {
	c := obj.DeepCopy()
	if c.GetKind() == "Secret" && c.GroupVersionKind().Group == "" {
		for _, field := range []string{"data", "stringData"} {
			if m, found, _ := unstructured.NestedMap(c.Object, field); found {
				for k := range m {
					m[k] = "<redacted>"
				}
				_ = unstructured.SetNestedMap(c.Object, m, field)
			}
		}
	}
	return marshalYAML(c.Object)
}

// statusFromBody recovers the API server's own Status from an error response. The
// Table request has a non-standard Accept header, so client-go can't always decode
// the error body and reports a bare "unknown"; the body itself still says
// `cronjobs.batch is forbidden: User "x" cannot list resource …`.
func statusFromBody(body []byte, err error) error {
	var st metav1.Status
	if len(body) > 0 && json.Unmarshal(body, &st) == nil && st.Kind == "Status" && st.Code != 0 {
		return &apierrors.StatusError{ErrStatus: st}
	}
	return err
}
