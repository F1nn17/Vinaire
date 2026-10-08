package securitychecks

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func find(rs []Result, id string) Result {
	for _, r := range rs {
		if r.ID == id {
			return r
		}
	}
	panic("no check " + id)
}

func pod(ns, name string, c corev1.Container) corev1.Pod {
	return corev1.Pod{ObjectMeta: metav1.ObjectMeta{Namespace: ns, Name: name}, Spec: corev1.PodSpec{Containers: []corev1.Container{c}}}
}

func TestPrivilegedAndSystemSplit(t *testing.T) {
	yes := true
	priv := corev1.Container{Name: "c", Image: "x:1", SecurityContext: &corev1.SecurityContext{Privileged: &yes}}
	s := &Snapshot{Pods: []corev1.Pod{pod("app", "a", priv), pod("kube-system", "b", priv), pod("app", "ok", corev1.Container{Name: "c", Image: "x:1"})}}
	r := find(RunAll(s), "pod-privileged")
	if r.Status != Fail || r.Total != 1 || r.SystemTotal != 1 {
		t.Fatalf("got %+v", r)
	}
	if r.Findings[0].Namespace != "app" {
		t.Fatalf("non-system findings must come first: %+v", r.Findings)
	}
}

func TestSystemOnlyFindingsDoNotFail(t *testing.T) {
	yes := true
	s := &Snapshot{Pods: []corev1.Pod{pod("kube-system", "b", corev1.Container{Name: "c", SecurityContext: &corev1.SecurityContext{Privileged: &yes}})}}
	if r := find(RunAll(s), "pod-privileged"); r.Status != Pass || r.SystemTotal != 1 {
		t.Fatalf("got %+v", r)
	}
}

func TestUnavailableMakesUnknownNotPass(t *testing.T) {
	s := &Snapshot{Unavailable: map[string]string{"pods": "you may not list pods"}}
	r := find(RunAll(s), "pod-privileged")
	if r.Status != Unknown || r.Reason == "" {
		t.Fatalf("got %+v", r)
	}
	// a check that doesn't need pods is unaffected
	if find(RunAll(s), "rbac-cluster-admin").Status != Pass {
		t.Fatal("unrelated check should pass")
	}
}

func TestImageLatest(t *testing.T) {
	cases := map[string]bool{"nginx": true, "nginx:latest": true, "nginx:1.25": false, "reg:5000/a/b": true, "reg:5000/a/b:2": false, "a@sha256:abc": false}
	for img, want := range cases {
		s := &Snapshot{Pods: []corev1.Pod{pod("app", "p", corev1.Container{Name: "c", Image: img})}}
		if got := find(RunAll(s), "pod-image-latest").Total == 1; got != want {
			t.Errorf("%s: flagged=%v want %v", img, got, want)
		}
	}
}

func TestRunAsNonRoot(t *testing.T) {
	yes, zero, uid := true, int64(0), int64(1000)
	good := corev1.Container{Name: "c", SecurityContext: &corev1.SecurityContext{RunAsNonRoot: &yes}}
	root := corev1.Container{Name: "c", SecurityContext: &corev1.SecurityContext{RunAsUser: &zero}}
	p := pod("app", "podlevel", corev1.Container{Name: "c"})
	p.Spec.SecurityContext = &corev1.PodSecurityContext{RunAsUser: &uid}
	s := &Snapshot{Pods: []corev1.Pod{pod("app", "g", good), pod("app", "r", root), pod("app", "u", corev1.Container{Name: "c"}), p}}
	r := find(RunAll(s), "pod-root")
	if r.Total != 2 {
		t.Fatalf("want root + unset flagged, got %+v", r.Findings)
	}
}

func TestClusterAdminSkipsSystemSubjects(t *testing.T) {
	s := &Snapshot{ClusterRoleBindings: []rbacv1.ClusterRoleBinding{{
		ObjectMeta: metav1.ObjectMeta{Name: "x"}, RoleRef: rbacv1.RoleRef{Name: "cluster-admin"},
		Subjects: []rbacv1.Subject{{Kind: "Group", Name: "system:masters"}, {Kind: "User", Name: "alice"}},
	}}}
	r := find(RunAll(s), "rbac-cluster-admin")
	if r.Total != 1 || r.Findings[0].Detail != "User alice" {
		t.Fatalf("got %+v", r)
	}
}

func TestWildcardRole(t *testing.T) {
	star := []rbacv1.PolicyRule{{Verbs: []string{"*"}, Resources: []string{"*"}}}
	s := &Snapshot{
		ClusterRoles: []rbacv1.ClusterRole{{ObjectMeta: metav1.ObjectMeta{Name: "cluster-admin"}, Rules: star}, {ObjectMeta: metav1.ObjectMeta{Name: "mine"}, Rules: star}},
		Roles:        []rbacv1.Role{{ObjectMeta: metav1.ObjectMeta{Namespace: "app", Name: "ok"}, Rules: []rbacv1.PolicyRule{{Verbs: []string{"get"}, Resources: []string{"*"}}}}},
	}
	r := find(RunAll(s), "rbac-wildcard")
	if r.Total != 1 || r.Findings[0].Name != "mine" {
		t.Fatalf("got %+v", r)
	}
}

func TestNamespaceChecksSkipSystem(t *testing.T) {
	s := &Snapshot{Namespaces: []corev1.Namespace{{ObjectMeta: metav1.ObjectMeta{Name: "kube-system"}}, {ObjectMeta: metav1.ObjectMeta{Name: "app"}}}}
	if r := find(RunAll(s), "ns-network-policy"); r.Total != 1 || r.Findings[0].Name != "app" {
		t.Fatalf("got %+v", r)
	}
}

func TestOrderAndCap(t *testing.T) {
	var pods []corev1.Pod
	for i := 0; i < MaxFindings+20; i++ {
		pods = append(pods, pod("app", string(rune('a'+i%26))+string(rune('a'+i/26)), corev1.Container{Name: "c", Image: "x"}))
	}
	rs := RunAll(&Snapshot{Pods: pods})
	if r := find(rs, "pod-image-latest"); len(r.Findings) != MaxFindings || r.Total != MaxFindings+20 {
		t.Fatalf("cap: %d findings, total %d", len(r.Findings), r.Total)
	}
	if rs[0].Status != Fail {
		t.Fatalf("failures should come first: %s", rs[0].ID)
	}
}
