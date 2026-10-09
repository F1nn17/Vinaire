package helm

import (
	"context"
	"testing"

	networkingv1 "k8s.io/api/networking/v1"
	storagev1 "k8s.io/api/storage/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

const renderedManifest = `
apiVersion: apps/v1
kind: StatefulSet
metadata: {name: db, namespace: ns}
spec:
  replicas: 3
  volumeClaimTemplates:
  - metadata: {name: data}
    spec: {resources: {requests: {storage: 10Gi}}}
---
apiVersion: v1
kind: PersistentVolumeClaim
metadata: {name: files, namespace: ns}
spec: {storageClassName: fast, resources: {requests: {storage: 5Gi}}}
---
apiVersion: v1
kind: Service
metadata: {name: a, namespace: ns}
---
apiVersion: v1
kind: Service
metadata: {name: b, namespace: ns}
---
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata: {name: web, namespace: ns}
spec: {ingressClassName: nginx}
`

func TestPlanSummarisesWhatWillBeCreated(t *testing.T) {
	objs, err := decodeObjects(renderedManifest)
	if err != nil {
		t.Fatal(err)
	}
	p := planOf(objs)
	if p.Created[0].Kind != "Service" || p.Created[0].Count != 2 {
		t.Errorf("created = %+v, want the most numerous kind first", p.Created)
	}
	if len(p.Volumes) != 2 || p.Volumes[0].Size != "10Gi" || p.Volumes[0].Name != "data (db × 3)" || p.Volumes[1].StorageClass != "fast" {
		t.Errorf("volumes = %+v", p.Volumes)
	}
	if !p.hasIngress || len(p.ingressClasses) != 1 || p.ingressClasses[0] != "nginx" {
		t.Errorf("ingress info = %v %v", p.hasIngress, p.ingressClasses)
	}
}

func codes(ws []Warning) map[string]string {
	m := map[string]string{}
	for _, w := range ws {
		m[w.Code] = w.Detail
	}
	return m
}

func TestWarningsFromACluster(t *testing.T) {
	objs, _ := decodeObjects(renderedManifest)
	p := planOf(objs)

	// nothing there: no default class, the named classes are missing, no ingress controller
	bare := fake.NewSimpleClientset()
	got := codes(clusterWarnings(context.Background(), bare, p))
	for _, want := range []string{"no-default-storage-class", "missing-storage-class", "no-ingress-controller"} {
		if _, ok := got[want]; !ok {
			t.Errorf("a bare cluster should warn %s, got %v", want, got)
		}
	}
	if got["missing-storage-class"] != "fast" {
		t.Errorf("detail = %q", got["missing-storage-class"])
	}

	// a cluster that has all of it
	full := fake.NewSimpleClientset(
		&storagev1.StorageClass{ObjectMeta: metav1.ObjectMeta{Name: "std", Annotations: map[string]string{defaultClassAnnotation: "true"}}},
		&storagev1.StorageClass{ObjectMeta: metav1.ObjectMeta{Name: "fast"}},
		&networkingv1.IngressClass{ObjectMeta: metav1.ObjectMeta{Name: "nginx"}},
	)
	if got := clusterWarnings(context.Background(), full, p); len(got) != 0 {
		t.Errorf("a complete cluster warned: %v", got)
	}

	// an ingress class that isn't the one in the chart
	other := fake.NewSimpleClientset(
		&storagev1.StorageClass{ObjectMeta: metav1.ObjectMeta{Name: "std", Annotations: map[string]string{defaultClassAnnotation: "true"}}},
		&storagev1.StorageClass{ObjectMeta: metav1.ObjectMeta{Name: "fast"}},
		&networkingv1.IngressClass{ObjectMeta: metav1.ObjectMeta{Name: "traefik"}},
	)
	if got := codes(clusterWarnings(context.Background(), other, p)); got["missing-ingress-class"] != "nginx" {
		t.Errorf("got %v", got)
	}
}

func TestNoWarningsForWhatTheChartDoesNotUse(t *testing.T) {
	objs, _ := decodeObjects("apiVersion: v1\nkind: ConfigMap\nmetadata: {name: c, namespace: ns}\n")
	if got := clusterWarnings(context.Background(), fake.NewSimpleClientset(), planOf(objs)); len(got) != 0 {
		t.Errorf("a ConfigMap-only chart needs no storage or ingress: %v", got)
	}
}
