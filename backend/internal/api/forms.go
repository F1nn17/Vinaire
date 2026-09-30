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
	FormFieldNode     FormFieldType = "node" // dropdown fed by the nodes list (Phase 19)
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
