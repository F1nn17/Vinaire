package helm

import "testing"

func TestDecodeObjectsSplitsDocumentsAndLists(t *testing.T) {
	objs, err := decodeObjects(`
apiVersion: v1
kind: ConfigMap
metadata: {name: a}
---
# only a comment

---
apiVersion: v1
kind: List
items:
  - {apiVersion: networking.k8s.io/v1, kind: NetworkPolicy, metadata: {name: np}}
  - {apiVersion: v1, kind: Service, metadata: {name: svc}}
`)
	if err != nil {
		t.Fatal(err)
	}
	var kinds []string
	for _, o := range objs {
		kinds = append(kinds, o.GetKind())
	}
	if len(kinds) != 3 || kinds[0] != "ConfigMap" || kinds[1] != "NetworkPolicy" || kinds[2] != "Service" {
		t.Errorf("kinds = %v, want [ConfigMap NetworkPolicy Service]", kinds)
	}
}

func TestPreflightResultWordsTheGaps(t *testing.T) {
	p := &PreflightResult{Missing: []Needed{
		{Group: "networking.k8s.io", Resource: "networkpolicies", Namespace: "db", Verbs: []string{"create", "get"}},
		{Resource: "namespaces", Verbs: []string{"create"}},
	}}
	if p.OK() {
		t.Fatal("a result with gaps reported OK")
	}
	want := "create/get networkpolicies.networking.k8s.io (in namespace db); create namespaces (cluster-wide)"
	if got := p.String(); got != want {
		t.Errorf("String() = %q, want %q", got, want)
	}
	if !(&PreflightResult{}).OK() {
		t.Error("an empty result isn't OK")
	}
}
