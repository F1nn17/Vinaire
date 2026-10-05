package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"
)

// Roadmap Phase 31: a small, hand-written field schema for the few resource
// kinds the Create screen offers as a form. Deliberately not derived from the
// Kubernetes OpenAPI schema — a form is a friendlier way to assemble the same
// YAML Phase 25's dry-run/apply already handles, so this endpoint is purely
// descriptive (no cluster access, no writes). The frontend serializes form
// state to YAML itself.

type FormFieldType string

const (
	FormFieldString   FormFieldType = "string"
	FormFieldNumber   FormFieldType = "number"
	FormFieldKeyValue FormFieldType = "keyvalue"
	FormFieldNode     FormFieldType = "node"   // dropdown fed by the nodes list (Phase 19)
	FormFieldSelect   FormFieldType = "select" // dropdown of a fixed list (Options)
)

type FormField struct {
	Key         string        `json:"key"`
	Label       string        `json:"label"`
	Type        FormFieldType `json:"type"`
	Required    bool          `json:"required,omitempty"`
	Default     any           `json:"default,omitempty"`
	Placeholder string        `json:"placeholder,omitempty"`
	Help        string        `json:"help,omitempty"`
	Secret      bool          `json:"secret,omitempty"` // mask values in the UI
	Group       string        `json:"group,omitempty"`
	// Options are the choices of a select field.
	Options []string `json:"options,omitempty"`
	// For a keyvalue field: what the two columns are called, instead of KEY / value.
	KeyPlaceholder   string `json:"keyPlaceholder,omitempty"`
	ValuePlaceholder string `json:"valuePlaceholder,omitempty"`
}

type FormSchema struct {
	ResourceType string      `json:"resourceType"`
	Kind         string      `json:"kind"`
	Label        string      `json:"label"`
	Fields       []FormField `json:"fields"`
}

var nameField = FormField{Key: "name", Label: "Name", Type: FormFieldString, Required: true, Placeholder: "my-app"}
var namespaceField = FormField{Key: "namespace", Label: "Namespace", Type: FormFieldString, Required: true, Default: "default"}

var formSchemas = map[string]FormSchema{
	"deployment": {
		ResourceType: "deployment", Kind: "Deployment", Label: "Deployment",
		Fields: []FormField{
			nameField,
			namespaceField,
			{Key: "image", Label: "Image", Type: FormFieldString, Required: true, Placeholder: "nginx:alpine"},
			{Key: "replicas", Label: "Replicas", Type: FormFieldNumber, Default: 1},
			{Key: "containerPort", Label: "Container port", Type: FormFieldNumber, Placeholder: "80", Help: "Optional."},
			{Key: "env", Label: "Environment variables", Type: FormFieldKeyValue},
			{Key: "cpuRequest", Label: "CPU request", Type: FormFieldString, Placeholder: "100m", Group: "Resources"},
			{Key: "memoryRequest", Label: "Memory request", Type: FormFieldString, Placeholder: "128Mi", Group: "Resources"},
			{Key: "cpuLimit", Label: "CPU limit", Type: FormFieldString, Placeholder: "500m", Group: "Resources"},
			{Key: "memoryLimit", Label: "Memory limit", Type: FormFieldString, Placeholder: "256Mi", Group: "Resources"},
			{Key: "node", Label: "Run on node", Type: FormFieldNode, Help: "Optional nodeSelector on the node's hostname label."},
		},
	},
	"configmap": {
		ResourceType: "configmap", Kind: "ConfigMap", Label: "ConfigMap",
		Fields: []FormField{
			nameField,
			namespaceField,
			{Key: "data", Label: "Data", Type: FormFieldKeyValue},
		},
	},
	"secret": {
		ResourceType: "secret", Kind: "Secret", Label: "Secret",
		Fields: []FormField{
			nameField,
			namespaceField,
			{Key: "data", Label: "Data", Type: FormFieldKeyValue, Secret: true, Help: "Sent as plain text; Kubernetes encodes it (stringData)."},
		},
	},
	// Roadmap backlog (Services & Ingress "+ Create"): the two network kinds people
	// create most. Anything beyond one rule/path/port set is YAML, and switching
	// back to the form is refused rather than dropping fields (see the converters).
	"service": {
		ResourceType: "service", Kind: "Service", Label: "Service",
		Fields: []FormField{
			nameField,
			namespaceField,
			{Key: "type", Label: "Type", Type: FormFieldSelect, Default: "ClusterIP", Options: []string{"ClusterIP", "NodePort", "LoadBalancer"},
				Help: "ClusterIP: reachable inside the cluster. NodePort: also on every node's IP. LoadBalancer: asks the cloud/ServiceLB for an external address."},
			{Key: "selector", Label: "Selector (which pods)", Type: FormFieldKeyValue, KeyPlaceholder: "label", ValuePlaceholder: "value",
				Help: "Pods carrying all of these labels receive the traffic — usually app = <your deployment>."},
			{Key: "ports", Label: "Ports", Type: FormFieldKeyValue, Required: true, KeyPlaceholder: "service port, e.g. 80", ValuePlaceholder: "container port (empty = same)",
				Help: "The container port may also be a named port."},
		},
	},
	"ingress": {
		ResourceType: "ingress", Kind: "Ingress", Label: "Ingress",
		Fields: []FormField{
			nameField,
			namespaceField,
			{Key: "className", Label: "Ingress class", Type: FormFieldString, Placeholder: "traefik", Help: "Which controller handles it; empty uses the cluster default."},
			{Key: "host", Label: "Host", Type: FormFieldString, Placeholder: "app.example.com", Help: "Empty matches any host."},
			{Key: "path", Label: "Path", Type: FormFieldString, Default: "/"},
			{Key: "pathType", Label: "Path type", Type: FormFieldSelect, Default: "Prefix", Options: []string{"Prefix", "Exact", "ImplementationSpecific"}},
			{Key: "serviceName", Label: "Backend Service", Type: FormFieldString, Required: true, Placeholder: "my-app"},
			{Key: "servicePort", Label: "Backend port", Type: FormFieldNumber, Required: true, Placeholder: "80"},
			{Key: "tlsSecret", Label: "TLS Secret", Type: FormFieldString, Placeholder: "app-tls", Help: "Name of a kubernetes.io/tls Secret in this namespace; needs a host. Empty serves plain HTTP."},
			{Key: "annotations", Label: "Annotations", Type: FormFieldKeyValue, KeyPlaceholder: "annotation", ValuePlaceholder: "value"},
		},
	},
	// The optional ResourceQuota the roadmap mentions is a second object, which
	// Phase 25's single-document apply can't create atomically — it belongs
	// with Phase 27's namespace/quota management instead.
	"namespace": {
		ResourceType: "namespace", Kind: "Namespace", Label: "Namespace",
		Fields: []FormField{nameField},
	},
}

// FormSchemaByType handles GET /api/v1/forms/{resourceType}.
func (h *Handler) FormSchemaByType(w http.ResponseWriter, r *http.Request) {
	schema, ok := formSchemas[chi.URLParam(r, "resourceType")]
	if !ok {
		respondError(w, http.StatusNotFound, "no form for this resource type")
		return
	}
	respondJSON(w, schema)
}
