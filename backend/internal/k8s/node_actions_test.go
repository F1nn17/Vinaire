package k8s

import (
	"context"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	clienttesting "k8s.io/client-go/testing"
)

func pod(ns, name string, mutate func(*corev1.Pod)) *corev1.Pod {
	p := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Namespace: ns, Name: name},
		Spec:       corev1.PodSpec{NodeName: "n1"},
		Status:     corev1.PodStatus{Phase: corev1.PodRunning},
	}
	if mutate != nil {
		mutate(p)
	}
	return p
}

func controlled(kind string) func(*corev1.Pod) {
	t := true
	return func(p *corev1.Pod) {
		p.OwnerReferences = []metav1.OwnerReference{{Kind: kind, Name: "x", Controller: &t}}
	}
}

func TestClassifyPodsLikeKubectlDrain(t *testing.T) {
	pods := []corev1.Pod{
		*pod("a", "web", controlled("ReplicaSet")),
		*pod("a", "agent", controlled("DaemonSet")),
		*pod("a", "bare", nil),
		*pod("a", "cache", func(p *corev1.Pod) {
			controlled("ReplicaSet")(p)
			p.Spec.Volumes = []corev1.Volume{{Name: "v", VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}}}}
		}),
		*pod("kube-system", "static", func(p *corev1.Pod) { p.Annotations = map[string]string{corev1.MirrorPodAnnotationKey: "h"} }),
		*pod("a", "done", func(p *corev1.Pod) { p.Status.Phase = corev1.PodSucceeded }),
	}
	got := map[string]DrainDisposition{}
	for _, d := range classifyPods(pods) {
		got[d.Name] = d.Disposition
	}
	want := map[string]DrainDisposition{"web": DrainEvict, "agent": DrainSkip, "bare": DrainUnmanaged, "cache": DrainLocalData, "static": DrainSkip, "done": DrainEvict}
	for n, w := range want {
		if got[n] != w {
			t.Errorf("%s: %s, want %s", n, got[n], w)
		}
	}
}

func evictionReactor(cs *fake.Clientset, refuse map[string]bool, evicted *[]string) {
	cs.PrependReactor("create", "pods", func(a clienttesting.Action) (bool, runtime.Object, error) {
		if a.GetSubresource() != "eviction" {
			return false, nil, nil
		}
		name := a.(clienttesting.CreateAction).GetObject().(metav1.Object).GetName()
		if refuse[name] {
			return true, nil, apierrors.NewTooManyRequests("disruption budget", 1)
		}
		*evicted = append(*evicted, name)
		return true, nil, nil
	})
}

func TestDrainCordonsEvictsAndRespectsBudgetsAndConsent(t *testing.T) {
	cs := fake.NewSimpleClientset(
		&corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "n1"}},
		pod("a", "web", controlled("ReplicaSet")),
		pod("a", "guarded", controlled("ReplicaSet")),
		pod("a", "agent", controlled("DaemonSet")),
		pod("a", "bare", nil),
	)
	var evicted []string
	evictionReactor(cs, map[string]bool{"guarded": true}, &evicted)

	// a bare pod blocks the drain until the caller consents — and nothing is cordoned or evicted
	if _, err := DrainNode(context.Background(), cs, "n1", DrainOptions{}, time.Millisecond); err == nil {
		t.Fatal("a bare pod didn't block the drain")
	}
	if n, _ := cs.CoreV1().Nodes().Get(context.Background(), "n1", metav1.GetOptions{}); n.Spec.Unschedulable || len(evicted) != 0 {
		t.Fatal("a blocked drain still cordoned or evicted")
	}

	res, err := DrainNode(context.Background(), cs, "n1", DrainOptions{EvictUnmanaged: true}, time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	if n, _ := cs.CoreV1().Nodes().Get(context.Background(), "n1", metav1.GetOptions{}); !n.Spec.Unschedulable {
		t.Error("the node wasn't cordoned")
	}
	if res.Evicted != 2 || res.Skipped != 1 || len(res.Pending) != 1 || res.Pending[0].Name != "guarded" {
		t.Errorf("result = %+v, evicted=%v (want web+bare evicted, agent skipped, guarded pending)", res, evicted)
	}
}

func TestCordonLabelsTaintsAndDelete(t *testing.T) {
	cs := fake.NewSimpleClientset(&corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "n1", Labels: map[string]string{"old": "1", "keep": "x"}}})
	ctx := context.Background()
	if err := CordonNode(ctx, cs, "n1", true); err != nil {
		t.Fatal(err)
	}
	n, _ := cs.CoreV1().Nodes().Get(ctx, "n1", metav1.GetOptions{})
	if !n.Spec.Unschedulable {
		t.Error("not cordoned")
	}
	if err := CordonNode(ctx, cs, "n1", false); err != nil {
		t.Fatal(err)
	}
	n, _ = cs.CoreV1().Nodes().Get(ctx, "n1", metav1.GetOptions{})
	if n.Spec.Unschedulable {
		t.Error("not uncordoned")
	}
	if err := PatchNodeLabels(ctx, cs, "n1", map[string]string{"role": "db"}, []string{"old"}); err != nil {
		t.Fatal(err)
	}
	n, _ = cs.CoreV1().Nodes().Get(ctx, "n1", metav1.GetOptions{})
	if n.Labels["role"] != "db" || n.Labels["keep"] != "x" {
		t.Errorf("labels = %v", n.Labels)
	}
	if _, has := n.Labels["old"]; has {
		t.Errorf("label not removed: %v", n.Labels)
	}
	if err := ReplaceNodeTaints(ctx, cs, "n1", []corev1.Taint{{Key: "k", Value: "v", Effect: corev1.TaintEffectNoSchedule}}); err != nil {
		t.Fatal(err)
	}
	n, _ = cs.CoreV1().Nodes().Get(ctx, "n1", metav1.GetOptions{})
	if len(n.Spec.Taints) != 1 || n.Spec.Taints[0].Key != "k" {
		t.Errorf("taints = %v", n.Spec.Taints)
	}
	if err := DeleteNode(ctx, cs, "n1"); err != nil {
		t.Fatal(err)
	}
	if _, err := cs.CoreV1().Nodes().Get(ctx, "n1", metav1.GetOptions{}); !apierrors.IsNotFound(err) {
		t.Errorf("node still there: %v", err)
	}
}
