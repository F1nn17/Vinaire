package helm

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	storagev1 "k8s.io/api/storage/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func node(providerID string) *corev1.Node {
	return &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "n1"}, Spec: corev1.NodeSpec{ProviderID: providerID}}
}

// Roadmap Phase 75b: a Service of type LoadBalancer stays <pending> for ever when nothing answers it.
func TestALoadBalancerNeedsSomethingToAnswerIt(t *testing.T) {
	ctx := context.Background()
	// plain k3s with its service load balancer pods
	cs := fake.NewSimpleClientset(node("k3s://n1"), &corev1.Pod{ObjectMeta: metav1.ObjectMeta{
		Name: "svclb-x", Namespace: "kube-system", Labels: map[string]string{"svccontroller.k3s.cattle.io/svcname": "x"}}})
	if has, known := loadBalancerAvailable(ctx, cs); !has || !known {
		t.Errorf("k3s's own load balancer: has=%v known=%v", has, known)
	}
	// a cloud provider names itself in the node's providerID
	if has, _ := loadBalancerAvailable(ctx, fake.NewSimpleClientset(node("aws:///eu-west-1a/i-1"))); !has {
		t.Error("a cloud provider serves load balancers")
	}
	// MetalLB
	metal := fake.NewSimpleClientset(node(""), &corev1.Pod{ObjectMeta: metav1.ObjectMeta{
		Name: "speaker", Namespace: "metallb-system", Labels: map[string]string{"app.kubernetes.io/name": "metallb"}}})
	if has, _ := loadBalancerAvailable(ctx, metal); !has {
		t.Error("MetalLB serves load balancers")
	}
	// a bare cluster: nothing answers
	if has, known := loadBalancerAvailable(ctx, fake.NewSimpleClientset(node("k3s://n1"))); has || !known {
		t.Errorf("a bare cluster: has=%v known=%v", has, known)
	}
	if has, known := loadBalancerAvailable(ctx, fake.NewSimpleClientset(node("kind://docker/kind/kind-control-plane"))); has || !known {
		t.Errorf("kind has none either: has=%v known=%v", has, known)
	}
}

func TestPlanNoticesALoadBalancerAndWarnsOnlyWhenNothingAnswers(t *testing.T) {
	objs, err := decodeObjects(`
apiVersion: v1
kind: Service
metadata: {name: db, namespace: ns}
spec: {type: LoadBalancer}
---
apiVersion: v1
kind: Service
metadata: {name: inner, namespace: ns}
spec: {type: ClusterIP}
`)
	if err != nil {
		t.Fatal(err)
	}
	p := planOf(objs)
	if !p.hasLoadBalancer {
		t.Fatal("a LoadBalancer Service was not noticed")
	}
	has := func(ws []Warning, code string) bool {
		for _, w := range ws {
			if w.Code == code {
				return true
			}
		}
		return false
	}
	if !has(clusterWarnings(context.Background(), fake.NewSimpleClientset(node("k3s://n1")), p), "no-load-balancer") {
		t.Error("nothing answers it: warn")
	}
	withLB := fake.NewSimpleClientset(node("gce://p/z/n"))
	if has(clusterWarnings(context.Background(), withLB, p), "no-load-balancer") {
		t.Error("a cloud answers it: no warning")
	}
	// a ClusterIP Service needs no load balancer
	only, _ := decodeObjects("apiVersion: v1\nkind: Service\nmetadata: {name: a, namespace: ns}\nspec: {type: ClusterIP}\n")
	if planOf(only).hasLoadBalancer {
		t.Error("ClusterIP is not a load balancer")
	}
}

func TestADatabaseBehindAnIngressIsWarnedAbout(t *testing.T) {
	objs, _ := decodeObjects(`
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata: {name: web, namespace: ns}
spec: {ingressClassName: nginx}
`)
	p := planOf(objs)
	cs := fake.NewSimpleClientset()
	for _, w := range clusterWarnings(context.Background(), cs, p) {
		if w.Code == "database-ingress" {
			t.Fatal("not a database: no warning")
		}
	}
	p.databaseChart = true
	found := false
	for _, w := range clusterWarnings(context.Background(), cs, p) {
		found = found || w.Code == "database-ingress"
	}
	if !found {
		t.Error("an Ingress carries HTTP, a database speaks TCP: warn")
	}
}

func TestEnvironmentReportsClassesAndWhatIsUnknown(t *testing.T) {
	def := &storagev1.StorageClass{ObjectMeta: metav1.ObjectMeta{Name: "local-path", Annotations: map[string]string{defaultClassAnnotation: "true"}}}
	other := &storagev1.StorageClass{ObjectMeta: metav1.ObjectMeta{Name: "fast"}}
	env := ReadEnvironment(context.Background(), fake.NewSimpleClientset(node("k3s://n1"), def, other))
	if !env.DefaultStorageClass || !env.StorageClassesKnown || len(env.StorageClasses) != 2 || env.StorageClasses[0] != "fast" {
		t.Errorf("%+v", env)
	}
	none := ReadEnvironment(context.Background(), fake.NewSimpleClientset(node("k3s://n1"), other))
	if none.DefaultStorageClass || !none.StorageClassesKnown {
		t.Errorf("a class that is not the default does not make a default: %+v", none)
	}
	if env.StorageClasses == nil || env.IngressClasses == nil {
		t.Error("lists are never null in the JSON")
	}
}
