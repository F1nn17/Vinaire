package api

import (
	"strings"
	"testing"
)

// Roadmap Phase 50: fuzzing what the API parses from a request body.

func FuzzParseSingleManifest(f *testing.F) {
	for _, s := range []string{
		"", "---", "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: a\n",
		"apiVersion: v1\nkind: ConfigMap\nmetadata: {name: a}\n---\napiVersion: v1\nkind: ConfigMap\nmetadata: {name: b}\n",
		"apiVersion: v1\nkind: List\nitems: []\n", "kind: Pod", "[1,2,3]", "a: &x [*x]", "apiVersion: v1\nkind: Secret\nmetadata: {generateName: x-}\n",
		strings.Repeat("a: ", 2000),
	} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, text string) {
		obj, err := parseSingleManifest(text)
		if err != nil {
			return
		}
		if obj.GetAPIVersion() == "" || obj.GetKind() == "" || strings.HasSuffix(obj.GetKind(), "List") {
			t.Fatalf("accepted an object with apiVersion=%q kind=%q", obj.GetAPIVersion(), obj.GetKind())
		}
		if obj.GetName() == "" && obj.GetGenerateName() == "" {
			t.Fatal("accepted an object with no name")
		}
	})
}

func FuzzParseProxyTarget(f *testing.F) {
	for _, s := range []string{"8080", "https:8443", "http:80", "0", "65536", "-1", "ftp:21", ":", "https:", "https:https:1", "1e3", " 80", "０８０"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		scheme, port, ok := parseProxyTarget(s)
		if !ok {
			return
		}
		if (scheme != "http" && scheme != "https") || port < 1 || port > 65535 {
			t.Fatalf("accepted %q as %s:%d", s, scheme, port)
		}
	})
}

func FuzzNodeLabelAndTaintValidation(f *testing.F) {
	f.Add("disktype", "ssd", "NoSchedule")
	f.Add("kubernetes.io/hostname", "x", "NoExecute")
	f.Add("a b", "c\nd", "Evict")
	f.Add("", "", "")
	f.Add(strings.Repeat("k", 300), strings.Repeat("v", 100), "PreferNoSchedule")
	f.Fuzz(func(t *testing.T, key, value, effect string) {
		if err := validateLabelChange(labelsBody{Set: map[string]string{key: value}}); err == nil {
			if protectedNodeLabels[key] {
				t.Fatalf("a protected label %q may be set", key)
			}
			if strings.ContainsAny(key+value, " \n\r\t\"'\\") {
				t.Fatalf("accepted label %q=%q", key, value)
			}
		}
		var tb taintBody
		tb.Taints = append(tb.Taints, struct {
			Key    string `json:"key"`
			Value  string `json:"value"`
			Effect string `json:"effect"`
		}{key, value, effect})
		if ts, err := validateTaints(tb); err == nil {
			e := string(ts[0].Effect)
			if e != "NoSchedule" && e != "PreferNoSchedule" && e != "NoExecute" {
				t.Fatalf("accepted taint effect %q", e)
			}
			if strings.ContainsAny(ts[0].Key+ts[0].Value, " \n\r\t\"'\\") {
				t.Fatalf("accepted taint %q=%q", ts[0].Key, ts[0].Value)
			}
		}
	})
}

func FuzzValidateBinding(f *testing.F) {
	f.Add("namespace", "ClusterRole", "view", "default", "", "alice", "User")
	f.Add("cluster", "ClusterRole", "edit", "", "edit", "system:masters", "Group")
	f.Add("cluster", "Role", "x", "", "x", "a", "ServiceAccount")
	f.Add("namespace", "ClusterRole", "cluster-admin", "kube-system", "", "../../x", "User")
	f.Fuzz(func(t *testing.T, scope, roleKind, roleName, ns, confirm, subject, subjectKind string) {
		b := &bindingBody{Scope: scope, RoleKind: roleKind, RoleName: roleName, Namespace: ns, ConfirmName: confirm}
		b.Subjects = append(b.Subjects, subjectBody{Kind: subjectKind, Name: subject, Namespace: ns})
		if err := validateBinding(b); err != nil {
			return
		}
		if scope == "cluster" && (roleKind != "ClusterRole" || confirm != roleName || ns != "") {
			t.Fatalf("a cluster-wide grant slipped through: %+v", b)
		}
		if strings.ContainsAny(roleName+b.Name, " \n\r\t\"'/") {
			t.Fatalf("accepted names %q / %q", roleName, b.Name)
		}
	})
}
