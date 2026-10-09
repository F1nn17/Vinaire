package api

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestListSelectedPodsOnlyTheSelectedOnesAndNeverAllOfThem(t *testing.T) {
	mk := func(name string, labels map[string]string) *corev1.Pod {
		return &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Namespace: "app", Name: name, Labels: labels}}
	}
	cs := fake.NewSimpleClientset(mk("db-0", map[string]string{"app": "db"}), mk("web-1", map[string]string{"app": "web"}))

	got := listSelectedPods(context.Background(), cs, "app", &metav1.LabelSelector{MatchLabels: map[string]string{"app": "db"}})
	rows := podRowsFor(got)
	if len(rows) != 1 || rows[0].Name != "db-0" {
		t.Fatalf("rows = %+v, want just db-0", rows)
	}
	// an empty selector would match every pod in the namespace — it must show none instead
	if rows := podRowsFor(listSelectedPods(context.Background(), cs, "app", &metav1.LabelSelector{})); len(rows) != 0 {
		t.Errorf("an empty selector listed %d pods", len(rows))
	}
	if rows := podRowsFor(nil); rows == nil || len(rows) != 0 {
		t.Errorf("no pod list must be an empty (not null) JSON array: %#v", rows)
	}
}
