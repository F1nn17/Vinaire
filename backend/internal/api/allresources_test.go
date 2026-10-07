package api

import (
	"errors"
	"strings"
	"testing"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

func TestListPathIsBuiltOnlyFromValidatedParts(t *testing.T) {
	cases := []struct {
		gvr        schema.GroupVersionResource
		namespaced bool
		ns, want   string
	}{
		{schema.GroupVersionResource{Version: "v1", Resource: "events"}, true, "", "/api/v1/events"},
		{schema.GroupVersionResource{Version: "v1", Resource: "events"}, true, "kube-system", "/api/v1/namespaces/kube-system/events"},
		{schema.GroupVersionResource{Group: "batch", Version: "v1", Resource: "cronjobs"}, true, "a", "/apis/batch/v1/namespaces/a/cronjobs"},
		{schema.GroupVersionResource{Group: "rbac.authorization.k8s.io", Version: "v1", Resource: "clusterroles"}, false, "ignored", "/apis/rbac.authorization.k8s.io/v1/clusterroles"},
	}
	for _, c := range cases {
		if got := listPath(c.gvr, c.namespaced, c.ns); got != c.want {
			t.Errorf("listPath(%v,%v,%q) = %q, want %q", c.gvr, c.namespaced, c.ns, got, c.want)
		}
	}
	for _, bad := range []string{"../../etc", "a/b", "a b", "A", "-a", ""} {
		if bad != "" && nsRe.MatchString(bad) {
			t.Errorf("namespace %q was accepted", bad)
		}
	}
	for _, bad := range []string{"v1/../x", "1", "latest", "v1;x"} {
		if versionRe.MatchString(bad) {
			t.Errorf("version %q was accepted", bad)
		}
	}
	for _, ok := range []string{"system:node", "my.cm-1", "a_b"} {
		if !objNameRe.MatchString(ok) {
			t.Errorf("name %q was refused", ok)
		}
	}
	for _, bad := range []string{"../x", "a/b", "a b", "-x", strings.Repeat("a", 300)} {
		if objNameRe.MatchString(bad) {
			t.Errorf("name %q was accepted", bad)
		}
	}
}

func TestTableFromJSONKeepsKubectlColumnsAndNames(t *testing.T) {
	raw := []byte(`{"kind":"Table","apiVersion":"meta.k8s.io/v1","metadata":{"continue":"x"},
	 "columnDefinitions":[{"name":"Name","type":"string","priority":0},{"name":"Schedule","type":"string","priority":0},{"name":"IP","type":"string","priority":1}],
	 "rows":[{"cells":["nightly","0 3 * * *","10.0.0.1"],"object":{"kind":"PartialObjectMetadata","metadata":{"name":"nightly","namespace":"jobs"}}}]}`)
	tb, err := tableFromJSON(raw, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(tb.Columns) != 2 || tb.Columns[1].Name != "Schedule" {
		t.Errorf("columns = %+v (the -o wide column must be dropped)", tb.Columns)
	}
	if len(tb.Rows) != 1 || tb.Rows[0].Name != "nightly" || tb.Rows[0].Namespace != "jobs" || len(tb.Rows[0].Cells) != 2 {
		t.Errorf("rows = %+v", tb.Rows)
	}
	if !tb.Truncated {
		t.Error("a continue token should mark the list truncated")
	}
}

func TestSecretValuesAreRedactedAndCannotBeAppliedBack(t *testing.T) {
	secret := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "v1", "kind": "Secret",
		"metadata": map[string]any{"name": "s", "namespace": "n"},
		"data":     map[string]any{"password": "aHVudGVyMg=="},
	}}
	out := redactedYAML(secret)
	if strings.Contains(out, "aHVudGVyMg") || !strings.Contains(out, "<redacted>") {
		t.Errorf("secret value leaked or not redacted:\n%s", out)
	}
	if secret.Object["data"].(map[string]any)["password"] != "aHVudGVyMg==" {
		t.Error("redaction modified the original object")
	}
	// the redacted form must not be savable
	red := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "v1", "kind": "Secret",
		"metadata": map[string]any{"name": "s"}, "data": map[string]any{"password": "<redacted>"},
	}}
	if _, err := parseSingleManifest(redactedYAML(red)); err == nil {
		t.Error("a Secret with <redacted> placeholders was accepted by apply")
	}
	// other kinds are untouched
	cm := &unstructured.Unstructured{Object: map[string]any{"apiVersion": "v1", "kind": "ConfigMap",
		"metadata": map[string]any{"name": "c"}, "data": map[string]any{"k": "v"}}}
	if !strings.Contains(redactedYAML(cm), "k: v") {
		t.Error("a ConfigMap was redacted")
	}
}

func TestStatusFromBodyKeepsTheServersForbiddenSentence(t *testing.T) {
	body := []byte(`{"kind":"Status","apiVersion":"v1","status":"Failure","message":"cronjobs.batch is forbidden: User \"x\" cannot list resource \"cronjobs\" in API group \"batch\" at the cluster scope","reason":"Forbidden","code":403}`)
	err := statusFromBody(body, errors.New("unknown"))
	if !apierrors.IsForbidden(err) || !strings.Contains(forbiddenMessage(err), "cannot list resource") {
		t.Errorf("got %v", err)
	}
	if got := statusFromBody([]byte("<html>"), errors.New("boom")); got.Error() != "boom" {
		t.Errorf("a non-Status body replaced the error: %v", got)
	}
}
