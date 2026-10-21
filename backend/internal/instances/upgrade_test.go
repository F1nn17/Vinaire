package instances

import (
	"strings"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func setWith(image, managedBy string, ann map[string]string) appsv1.StatefulSet {
	return appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{Name: "db", Namespace: "apps", Annotations: ann,
			Labels: map[string]string{labelManagedBy: managedBy}},
		Spec: appsv1.StatefulSetSpec{Template: corev1.PodTemplateSpec{Spec: corev1.PodSpec{
			Containers: []corev1.Container{{Name: "app", Image: image}}}}},
	}
}

func TestOnlyTheTagChangesAndMajorNeedsAcknowledgement(t *testing.T) {
	sts := setWith("postgres:17.2", "vinaire", nil)
	p, err := PlanUpgrade(sts, "17.4", false)
	if err != nil || p.To != "postgres:17.4" || p.Major {
		t.Fatalf("minor upgrade: %+v %v", p, err)
	}
	if !strings.Contains(string(p.Patch), `"vinaire.io/previous-image":"postgres:17.2"`) {
		t.Errorf("the previous image must be recorded: %s", p.Patch)
	}
	if _, err := PlanUpgrade(sts, "18", false); err == nil {
		t.Error("a major change needs acceptMajor")
	}
	if p, err := PlanUpgrade(sts, "18", true); err != nil || !p.Major {
		t.Errorf("acknowledged major: %+v %v", p, err)
	}
	if _, err := PlanUpgrade(sts, "16", true); err == nil {
		t.Error("a lower major is refused even when acknowledged")
	}
	if _, err := PlanUpgrade(sts, "latest", false); err == nil {
		t.Error("an incomparable tag needs acceptMajor")
	}
	if _, err := PlanUpgrade(sts, "17.2", true); err == nil {
		t.Error("the same tag is not an upgrade")
	}
}

func TestTagIsValidatedAndRepositoryIsKept(t *testing.T) {
	sts := setWith("postgres:17.2", "vinaire", nil)
	for _, bad := range []string{"", "17;x", "../x", "a b", "evil/postgres:17", "x@sha256:abc"} {
		if _, err := PlanUpgrade(sts, bad, true); err == nil {
			t.Errorf("tag %q should be refused", bad)
		}
	}
	p, _ := PlanUpgrade(setWith("quay.io/keycloak/keycloak:26.0", "vinaire", nil), "26.1", false)
	if p.To != "quay.io/keycloak/keycloak:26.1" {
		t.Errorf("repository must be kept: %s", p.To)
	}
	if _, err := PlanUpgrade(setWith("postgres@sha256:abc", "vinaire", nil), "17", true); err == nil {
		t.Error("a digest-pinned image is not changed here")
	}
	if _, err := PlanUpgrade(setWith("postgres:17", "Helm", nil), "18", true); err == nil {
		t.Error("only image installs")
	}
}

func TestRevert(t *testing.T) {
	if _, err := PlanRevert(setWith("postgres:17.4", "vinaire", nil)); err == nil {
		t.Error("nothing to revert to")
	}
	if p, err := PlanRevert(setWith("postgres:17.4", "vinaire", map[string]string{AnnotationPreviousImage: "postgres:17.2"})); err != nil || p.To != "postgres:17.2" {
		t.Errorf("revert: %+v %v", p, err)
	}
	if _, err := PlanRevert(setWith("postgres:17.4", "vinaire", map[string]string{AnnotationPreviousImage: "evil/postgres:17.2"})); err == nil {
		t.Error("an annotation naming another repository must be refused")
	}
}
