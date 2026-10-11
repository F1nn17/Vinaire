package api

import (
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func hasEdge(g TopologyGraph, from, to string) bool {
	for _, e := range g.Edges {
		if e.From == from && e.To == to {
			return true
		}
	}
	return false
}

// With every namespace in one graph, objects that share a name or a label in different
// namespaces must not be joined to each other.
func TestTopologyAcrossNamespacesKeepsEachNamespaceApart(t *testing.T) {
	lbl := map[string]string{"app": "db"}
	meta := func(ns, name string) metav1.ObjectMeta { return metav1.ObjectMeta{Namespace: ns, Name: name} }
	podMeta := func(ns, name string) metav1.ObjectMeta {
		m := meta(ns, name)
		m.Labels = lbl
		return m
	}
	g := buildTopologyGraph("all",
		[]networkingv1.Ingress{{ObjectMeta: meta("a", "web"), Spec: networkingv1.IngressSpec{
			DefaultBackend: &networkingv1.IngressBackend{Service: &networkingv1.IngressServiceBackend{Name: "db"}},
		}}},
		[]corev1.Service{
			{ObjectMeta: meta("a", "db"), Spec: corev1.ServiceSpec{Selector: lbl}},
			{ObjectMeta: meta("b", "db"), Spec: corev1.ServiceSpec{Selector: lbl}},
		},
		nil,
		[]appsv1.StatefulSet{{ObjectMeta: meta("a", "pg"), Spec: appsv1.StatefulSetSpec{
			Selector: &metav1.LabelSelector{MatchLabels: lbl},
		}}},
		nil, nil,
		[]corev1.Pod{{ObjectMeta: podMeta("a", "pg-0")}, {ObjectMeta: podMeta("b", "other-0")}},
		nil, nil,
	)

	if !hasEdge(g, "ingress/a/web", "service/a/db") {
		t.Error("the ingress should reach the Service of its own namespace")
	}
	if hasEdge(g, "ingress/a/web", "service/b/db") {
		t.Error("the ingress must not reach a same-named Service of another namespace")
	}
	if !hasEdge(g, "service/a/db", "pod/a/pg-0") || hasEdge(g, "service/a/db", "pod/b/other-0") {
		t.Error("a Service selects pods of its own namespace only")
	}
	if !hasEdge(g, "statefulset/a/pg", "pod/a/pg-0") {
		t.Error("a StatefulSet owns the pods its selector matches")
	}
	if hasEdge(g, "statefulset/a/pg", "pod/b/other-0") {
		t.Error("a StatefulSet must not claim pods of another namespace")
	}
}
