package api

import (
	"log/slog"

	"sigs.k8s.io/yaml"
)

// marshalYAML renders v (any Kubernetes API object) as YAML, the same shape
// `kubectl get <resource> -o yaml` would print. Marshaling failures are
// logged rather than propagated — a broken YAML tab shouldn't take down the
// rest of a detail page that already has good data.
func marshalYAML(v any) string {
	out, err := yaml.Marshal(v)
	if err != nil {
		slog.Error("marshal yaml", "error", err)
		return ""
	}
	return string(out)
}
