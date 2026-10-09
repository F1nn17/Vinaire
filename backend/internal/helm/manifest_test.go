package helm

import (
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func nestedString(obj map[string]any, path ...string) (string, bool, error) {
	return unstructured.NestedString(obj, path...)
}

func TestManifestRedactsSecretValuesAndKeepsTheRest(t *testing.T) {
	objs, err := decodeObjects(`
apiVersion: v1
kind: Secret
metadata: {name: s, namespace: ns}
data: {password: aHVudGVyMg==}
stringData: {token: topsecret}
---
apiVersion: v1
kind: ConfigMap
metadata: {name: c, namespace: ns}
data: {mode: fast}
`)
	if err != nil {
		t.Fatal(err)
	}
	m, truncated, err := manifestOf(objs)
	if err != nil || truncated {
		t.Fatalf("err=%v truncated=%v", err, truncated)
	}
	for _, leaked := range []string{"aHVudGVyMg==", "topsecret"} {
		if strings.Contains(m, leaked) {
			t.Errorf("the preview leaks %q:\n%s", leaked, m)
		}
	}
	for _, kept := range []string{"password: <redacted>", "token: <redacted>", "mode: fast", "kind: ConfigMap", "---"} {
		if !strings.Contains(m, kept) {
			t.Errorf("the preview lacks %q:\n%s", kept, m)
		}
	}
	// the source objects are untouched (the preview works on copies)
	if v, _, _ := nestedString(objs[0].Object, "stringData", "token"); v != "topsecret" {
		t.Error("the redaction changed the rendered object itself")
	}
}

func TestManifestIsCapped(t *testing.T) {
	big := strings.Repeat("x", 300<<10)
	var docs []string
	for _, n := range []string{"a", "b", "c"} {
		docs = append(docs, "apiVersion: v1\nkind: ConfigMap\nmetadata: {name: "+n+", namespace: ns}\ndata: {blob: "+big+"}\n")
	}
	objs, err := decodeObjects(strings.Join(docs, "---\n"))
	if err != nil {
		t.Fatal(err)
	}
	m, truncated, err := manifestOf(objs)
	if err != nil || !truncated || len(m) > maxManifestBytes {
		t.Errorf("err=%v truncated=%v len=%d", err, truncated, len(m))
	}
}
