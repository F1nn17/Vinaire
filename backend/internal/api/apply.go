package api

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	utilyaml "k8s.io/apimachinery/pkg/util/yaml"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/dynamic"
	"sigs.k8s.io/yaml"

	"github.com/f1nn17/vinaire/backend/internal/k8s"
)

// This file holds Roadmap Phase 25's generic YAML create/update — an
// explicit, deliberate extension to AGENTS.md's write rule (see
// internal/k8s/apply.go). The caller's own Kubernetes RBAC decides what's
// allowed; every real write is audited.

const maxApplyBodyBytes = 1 << 20 // 1 MiB — a manifest, not a data upload

type applyRequest struct {
	YAML string `json:"yaml"`
	Mode string `json:"mode"` // dry-run only: "create" or "update"
	// ResourceVersion is required for a real update: the live version the
	// dry-run diff was computed against (see ApplyDryRunResponse). The write
	// is rejected with a 409 if the object changed after that preview.
	ResourceVersion string `json:"resourceVersion"`
}

// ApplyDryRunResponse carries the before/after YAML for the diff modal.
// Before is empty for a create.
type ApplyDryRunResponse struct {
	Kind       string `json:"kind"`
	APIVersion string `json:"apiVersion"`
	Namespace  string `json:"namespace"`
	Name       string `json:"name"`
	Before     string `json:"before"`
	After      string `json:"after"`
	// ResourceVersion is the live object's version at dry-run time (update
	// only). The editor's own copy is ignored on purpose: status churn bumps
	// it constantly, which would make any edit of a busy resource fail with a
	// spurious conflict. What the user approves is the diff shown against
	// this version.
	ResourceVersion string `json:"resourceVersion,omitempty"`
}

type parsedManifest struct {
	obj    *unstructured.Unstructured
	target k8s.ApplyTarget
}

// decodeManifest reads the request body and turns its single YAML document
// into an object plus the REST resource it targets. It writes the error
// response itself and returns ok=false on any failure.
func (h *Handler) decodeManifest(w http.ResponseWriter, r *http.Request, disc discovery.DiscoveryInterface) (applyRequest, parsedManifest, bool) {
	var req applyRequest
	body := http.MaxBytesReader(w, r.Body, maxApplyBodyBytes)
	if err := json.NewDecoder(body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, `request body must be {"yaml": "<manifest>"} and under 1 MiB`)
		return req, parsedManifest{}, false
	}

	obj, err := parseSingleManifest(req.YAML)
	if err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return req, parsedManifest{}, false
	}

	target, err := k8s.ResolveApplyTarget(disc, obj.GroupVersionKind())
	if errors.Is(err, k8s.ErrUnknownKind) {
		respondError(w, http.StatusBadRequest, err.Error())
		return req, parsedManifest{}, false
	}
	if err != nil {
		respondK8sError(w, err)
		return req, parsedManifest{}, false
	}
	if target.Namespaced {
		if obj.GetNamespace() == "" {
			obj.SetNamespace("default")
		}
	} else {
		obj.SetNamespace("")
	}
	return req, parsedManifest{obj: obj, target: target}, true
}

func parseSingleManifest(text string) (*unstructured.Unstructured, error) {
	reader := utilyaml.NewYAMLReader(bufio.NewReader(strings.NewReader(text)))
	var docs [][]byte
	for {
		doc, err := reader.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("invalid YAML: %w", err)
		}
		if len(bytes.TrimSpace(doc)) > 0 {
			docs = append(docs, doc)
		}
	}
	if len(docs) == 0 {
		return nil, errors.New("YAML is empty")
	}
	if len(docs) > 1 {
		return nil, errors.New("exactly one YAML document is supported (found several separated by ---)")
	}

	var raw map[string]any
	if err := yaml.Unmarshal(docs[0], &raw); err != nil {
		return nil, fmt.Errorf("invalid YAML: %w", err)
	}
	obj := &unstructured.Unstructured{Object: raw}
	if obj.GetAPIVersion() == "" || obj.GetKind() == "" {
		return nil, errors.New("apiVersion and kind are required")
	}
	if strings.HasSuffix(obj.GetKind(), "List") {
		return nil, errors.New("List kinds are not supported — apply one object at a time")
	}
	if obj.GetName() == "" && obj.GetGenerateName() == "" {
		return nil, errors.New("metadata.name is required")
	}
	if hasRedactedSecretValue(obj) {
		return nil, errors.New("this Secret still contains <redacted> placeholders (the viewer hides values) — applying it would overwrite the real values; set the values yourself or remove those keys")
	}
	return obj, nil
}

// sanitizeForDiff drops server-churn fields that change on every write and
// would drown the real edit in the diff, and redacts Secret values — the
// diff modal must not become a second way to read a Secret (AGENTS.md:
// decoded values only via the audited reveal endpoint).
func sanitizeForDiff(obj *unstructured.Unstructured) string {
	if obj == nil {
		return ""
	}
	c := obj.DeepCopy()
	unstructured.RemoveNestedField(c.Object, "metadata", "managedFields")
	unstructured.RemoveNestedField(c.Object, "metadata", "resourceVersion")
	unstructured.RemoveNestedField(c.Object, "metadata", "generation")
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

func (h *Handler) resolveApplyClients(w http.ResponseWriter, r *http.Request) (dynamic.Interface, discovery.DiscoveryInterface, bool) {
	_, cfg, ok := h.resolveRESTConfig(w, r)
	if !ok {
		return nil, nil, false
	}
	dyn, err := dynamic.NewForConfig(cfg)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to build dynamic client: "+err.Error())
		return nil, nil, false
	}
	disc, err := discovery.NewDiscoveryClientForConfig(cfg)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to build discovery client: "+err.Error())
		return nil, nil, false
	}
	return dyn, disc, true
}

// runApply performs create (update=false) or update (update=true) against
// the API server, optionally as a dry-run, and returns the live "before"
// object (nil for a create) alongside the result.
func runApply(r *http.Request, dyn dynamic.Interface, m parsedManifest, update, dryRun bool, resourceVersion string) (before, after *unstructured.Unstructured, err error) {
	if !update {
		after, err = k8s.CreateObject(r.Context(), dyn, m.target, m.obj, dryRun)
		return nil, after, err
	}
	before, err = k8s.GetLiveObject(r.Context(), dyn, m.target, m.obj.GetNamespace(), m.obj.GetName())
	if err != nil {
		return nil, nil, err
	}
	if resourceVersion == "" {
		resourceVersion = before.GetResourceVersion()
	}
	m.obj.SetResourceVersion(resourceVersion)
	after, err = k8s.UpdateObject(r.Context(), dyn, m.target, m.obj, dryRun)
	return before, after, err
}

// ApplyDryRun handles POST /api/v1/resources/dry-run: validates the manifest
// through the API server itself (schema + admission, nothing persisted) and
// returns before/after YAML for the diff view.
func (h *Handler) ApplyDryRun(w http.ResponseWriter, r *http.Request) {
	dyn, disc, ok := h.resolveApplyClients(w, r)
	if !ok {
		return
	}
	req, m, ok := h.decodeManifest(w, r, disc)
	if !ok {
		return
	}
	if req.Mode != "create" && req.Mode != "update" {
		respondError(w, http.StatusBadRequest, `mode must be "create" or "update"`)
		return
	}

	before, after, err := runApply(r, dyn, m, req.Mode == "update", true, "")
	if err != nil {
		respondK8sError(w, err)
		return
	}
	resp := ApplyDryRunResponse{
		Kind:       m.obj.GetKind(),
		APIVersion: m.obj.GetAPIVersion(),
		Namespace:  m.obj.GetNamespace(),
		Name:       m.obj.GetName(),
		Before:     sanitizeForDiff(before),
		After:      sanitizeForDiff(after),
	}
	if before != nil {
		resp.ResourceVersion = before.GetResourceVersion()
	}
	respondJSON(w, resp)
}

// CreateResource handles POST /api/v1/resources.
func (h *Handler) CreateResource(w http.ResponseWriter, r *http.Request) {
	h.applyReal(w, r, false)
}

// UpdateResource handles PUT /api/v1/resources.
func (h *Handler) UpdateResource(w http.ResponseWriter, r *http.Request) {
	h.applyReal(w, r, true)
}

func (h *Handler) applyReal(w http.ResponseWriter, r *http.Request, update bool) {
	dyn, disc, ok := h.resolveApplyClients(w, r)
	if !ok {
		return
	}
	req, m, ok := h.decodeManifest(w, r, disc)
	if !ok {
		return
	}
	if update && req.ResourceVersion == "" {
		respondError(w, http.StatusBadRequest, "resourceVersion is required — review the change (dry-run) first")
		return
	}

	// Always dry-run first, server-side, so the real write can never skip
	// validation regardless of what the client did.
	if _, _, err := runApply(r, dyn, m, update, true, req.ResourceVersion); err != nil {
		respondK8sError(w, err)
		return
	}

	action := "apply-create"
	if update {
		action = "apply-update"
	}
	gvk := schema.FromAPIVersionAndKind(m.obj.GetAPIVersion(), m.obj.GetKind())
	h.auditLog(r, action, m.obj.GetNamespace(), m.obj.GetName(), "kind", gvk.Kind, "apiVersion", m.obj.GetAPIVersion())

	_, after, err := runApply(r, dyn, m, update, false, req.ResourceVersion)
	if err != nil {
		respondK8sError(w, err)
		return
	}
	respondJSON(w, map[string]string{
		"status":    "ok",
		"kind":      after.GetKind(),
		"namespace": after.GetNamespace(),
		"name":      after.GetName(),
	})
}

// hasRedactedSecretValue reports whether a core Secret carries the "<redacted>"
// placeholder Vinaire shows instead of values: saving that back would replace the
// real values with the placeholder.
func hasRedactedSecretValue(obj *unstructured.Unstructured) bool {
	if obj.GetKind() != "Secret" || obj.GroupVersionKind().Group != "" {
		return false
	}
	for _, field := range []string{"data", "stringData"} {
		m, found, _ := unstructured.NestedMap(obj.Object, field)
		if !found {
			continue
		}
		for _, v := range m {
			if v == "<redacted>" {
				return true
			}
		}
	}
	return false
}
