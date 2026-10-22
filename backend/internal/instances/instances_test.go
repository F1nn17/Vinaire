package instances

import (
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func lab(managedBy, inst string) map[string]string {
	return map[string]string{labelManagedBy: managedBy, labelInstance: inst, labelName: "postgresql"}
}

func meta(ns, name string, l map[string]string, refs ...metav1.OwnerReference) metav1.ObjectMeta {
	return metav1.ObjectMeta{Namespace: ns, Name: name, Labels: l, OwnerReferences: refs}
}

func TestImageInstanceKeepsClaimsAndUnownedSecrets(t *testing.T) {
	l := lab("vinaire", "db")
	owner := metav1.OwnerReference{Kind: "StatefulSet", Name: "db"}
	got := Build(Snapshot{
		StatefulSets: []appsv1.StatefulSet{{ObjectMeta: meta("apps", "db", l)}},
		Services:     []corev1.Service{{ObjectMeta: meta("apps", "db", l, owner)}},
		Secrets: []corev1.Secret{
			{ObjectMeta: meta("apps", "db-pw", l, owner)},
			{ObjectMeta: meta("apps", "db-extra", l)},
		},
		PVCs: []corev1.PersistentVolumeClaim{{ObjectMeta: meta("apps", "data-db-0", l)}},
	})
	if len(got) != 1 {
		t.Fatalf("want 1 instance, got %d", len(got))
	}
	fates := map[string]string{}
	for _, p := range got[0].Parts {
		fates[p.Name] = p.Fate
	}
	want := map[string]string{"db": "removed", "db-pw": "removed", "db-extra": "stays", "data-db-0": "stays"}
	for n, f := range want {
		if fates[n] != f {
			t.Errorf("%s: want %s, got %s", n, f, fates[n])
		}
	}
	if got[0].Stays != 2 || got[0].Origin != OriginImage {
		t.Errorf("stays=%d origin=%s", got[0].Stays, got[0].Origin)
	}
}

func TestClaimWithOnlySelectorLabelsJoinsItsInstance(t *testing.T) {
	l := lab("vinaire", "db")
	sel := map[string]string{labelName: "postgres", labelInstance: "db"}
	got := Build(Snapshot{
		StatefulSets: []appsv1.StatefulSet{{ObjectMeta: meta("apps", "db", l)}},
		PVCs: []corev1.PersistentVolumeClaim{
			{ObjectMeta: meta("apps", "data-db-0", sel)},
			{ObjectMeta: meta("other", "data-db-0", sel)}, // same name, another namespace: not ours
		},
	})
	if len(got) != 1 || got[0].Stays != 1 || len(got[0].Parts) != 2 {
		t.Fatalf("unexpected: %+v", got)
	}
}

func TestHelmRecordIsNotAPartAndHelmRemovesItsObjects(t *testing.T) {
	l := lab("Helm", "pg")
	got := Build(Snapshot{
		Deployments: []appsv1.Deployment{{ObjectMeta: meta("apps", "pg", l)}},
		Services:    []corev1.Service{{ObjectMeta: meta("apps", "pg", l)}},
		Secrets: []corev1.Secret{
			{ObjectMeta: meta("apps", "sh.helm.release.v1.pg.v1", l), Type: helmSecretType},
		},
	})
	if len(got) != 1 || len(got[0].Parts) != 2 || got[0].Stays != 0 {
		t.Fatalf("unexpected: %+v", got)
	}
}

func TestNeedsAWorkloadAndIgnoresForeignOrBadLabels(t *testing.T) {
	got := Build(Snapshot{
		Services: []corev1.Service{
			{ObjectMeta: meta("apps", "lonely", lab("vinaire", "lonely"))},
		},
		Deployments: []appsv1.Deployment{
			{ObjectMeta: meta("apps", "x", lab("kubectl", "x"))},
			{ObjectMeta: meta("apps", "y", lab("Helm", "<script>"))},
		},
	})
	if len(got) != 0 {
		t.Fatalf("want none, got %+v", got)
	}
}

func TestSystemNamespaceIsMarked(t *testing.T) {
	got := Build(Snapshot{Deployments: []appsv1.Deployment{{ObjectMeta: meta("kube-system", "traefik", lab("Helm", "traefik"))}}})
	if len(got) != 1 || !got[0].System {
		t.Fatalf("unexpected: %+v", got)
	}
}
