package gitops

import (
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func cm(data, rv string) *unstructured.Unstructured {
	return &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "v1", "kind": "ConfigMap",
		"metadata": map[string]any{"name": "x", "namespace": "n", "resourceVersion": rv, "generation": int64(3),
			"managedFields": []any{map[string]any{"manager": "a"}}},
		"data":   map[string]any{"k": data},
		"status": map[string]any{"phase": "whatever"},
	}}
}

// A dry-run keeps the live resourceVersion, so only the content can tell whether
// applying would change anything.
func TestSameContent(t *testing.T) {
	if !sameContent(cm("v1", "10"), cm("v1", "10")) {
		t.Error("identical content must be in sync")
	}
	dry := cm("v1", "10")
	dry.Object["metadata"].(map[string]any)["managedFields"] = []any{map[string]any{"manager": "vinaire-gitops"}}
	dry.Object["metadata"].(map[string]any)["generation"] = int64(4)
	dry.Object["status"] = map[string]any{"phase": "other"}
	if !sameContent(cm("v1", "10"), dry) {
		t.Error("bookkeeping metadata and status are not drift")
	}
	if sameContent(cm("v1", "10"), cm("v2", "10")) {
		t.Error("a changed value with the SAME resourceVersion must be out of sync — this is the dry-run case")
	}
	extra := cm("v1", "10")
	extra.Object["metadata"].(map[string]any)["labels"] = map[string]any{"a": "b"}
	if sameContent(cm("v1", "10"), extra) {
		t.Error("a label change is drift")
	}
}
