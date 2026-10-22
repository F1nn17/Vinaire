package apps

import (
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func pod(ns, name, image, ownerKind, ownerName string, ready bool) corev1.Pod {
	yes := true
	p := corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Namespace: ns, Name: name},
		Spec:       corev1.PodSpec{Containers: []corev1.Container{{Name: "c", Image: image}}},
		Status:     corev1.PodStatus{Phase: corev1.PodRunning},
	}
	if ownerKind != "" {
		p.OwnerReferences = []metav1.OwnerReference{{Kind: ownerKind, Name: ownerName, Controller: &yes}}
	}
	if ready {
		p.Status.Conditions = []corev1.PodCondition{{Type: corev1.PodReady, Status: corev1.ConditionTrue}}
	}
	return p
}

func TestImagesAreRecognisedByTheirRepository(t *testing.T) {
	for image, want := range map[string]string{
		"postgres:17":                                   "postgresql",
		"docker.io/library/postgres:17.2":               "postgresql",
		"bitnami/postgresql:16":                         "postgresql",
		"ghcr.io/cloudnative-pg/postgresql:17.2":        "postgresql",
		"redis:7.4":                                     "redis",
		"valkey/valkey:8":                               "redis",
		"quay.io/opstree/redis:v7.0.15":                 "redis",
		"quay.io/minio/aistor/minio:RELEASE.2026-01-01": "minio",
		"minio/minio:latest":                            "minio",
		"quay.io/keycloak/keycloak:26.2.5":              "keycloak",
		"registry.local:5000/team/postgres@sha256:abc":  "postgresql",
		// not the application itself
		"quay.io/prometheuscommunity/postgres-exporter:v0.17": "",
		"oliver006/redis_exporter:v1":                         "",
		"minio/operator:v6":                                   "",
		"quay.io/keycloak/keycloak-operator:26":               "",
		"nginx:1.27":                                          "",
		"myorg/not-postgres-really:1":                         "",
	} {
		got := ""
		if apps := Detect([]corev1.Pod{pod("ns", "p-0", image, "", "", true)}); len(apps) == 1 {
			got = apps[0].ID
		}
		if got != want {
			t.Errorf("%s: recognised as %q, want %q", image, got, want)
		}
	}
}

func TestPodsAreGroupedIntoTheirWorkloads(t *testing.T) {
	apps := Detect([]corev1.Pod{
		pod("db", "pg-0", "postgres:17", "StatefulSet", "pg", true),
		pod("db", "pg-1", "postgres:17", "StatefulSet", "pg", false),
		pod("web", "kc-5d9c7b8f4-x2x2x", "quay.io/keycloak/keycloak:26.2.5", "ReplicaSet", "kc-5d9c7b8f4", true),
		pod("a", "lonely", "redis:7.4", "", "", true),
	})
	if len(apps) != 3 || apps[0].ID != "postgresql" || apps[1].ID != "redis" || apps[2].ID != "keycloak" {
		t.Fatalf("in the display order of the rules: %+v", apps)
	}
	pg := apps[0].Instances
	if len(pg) != 1 || pg[0].Kind != "StatefulSet" || pg[0].Name != "pg" || pg[0].Pods != 2 || pg[0].Ready != 1 || pg[0].Image != "postgres:17" {
		t.Errorf("two pods of one StatefulSet are one instance: %+v", pg)
	}
	if kc := apps[2].Instances[0]; kc.Kind != "Deployment" || kc.Name != "kc" {
		t.Errorf("a ReplicaSet's hash is dropped: %+v", kc)
	}
	if r := apps[1].Instances[0]; r.Kind != "Pod" || r.Name != "lonely" {
		t.Errorf("a pod with no owner is its own instance: %+v", r)
	}
}

func TestFinishedPodsAndTheEmptyClusterGiveNothing(t *testing.T) {
	done := pod("ns", "job-1", "postgres:17", "Job", "backup", false)
	done.Status.Phase = corev1.PodSucceeded
	if got := Detect([]corev1.Pod{done}); len(got) != 0 {
		t.Errorf("a completed Job is not a running database: %+v", got)
	}
	if got := Detect(nil); len(got) != 0 {
		t.Errorf("%+v", got)
	}
}

func TestTheNumberOfInstancesIsBounded(t *testing.T) {
	var pods []corev1.Pod
	for i := 0; i < maxInstances+20; i++ {
		pods = append(pods, pod("ns", "p"+strings.Repeat("x", i%5)+string(rune('a'+i%26))+string(rune('a'+i/26)), "postgres:17", "", "", true))
	}
	if got := Detect(pods); len(got) != 1 || len(got[0].Instances) != maxInstances {
		t.Errorf("got %d", len(got[0].Instances))
	}
}

// What the image parser must never do with whatever a pod spec holds: panic, or return a repository
// that still carries a tag or a digest.
func FuzzRepoAndTag(f *testing.F) {
	f.Add("quay.io:443/a/b:1")
	f.Add("a@sha256:x")
	f.Add(":::@@@///")
	f.Fuzz(func(t *testing.T, image string) {
		repo, tag := repoAndTag(image)
		if strings.Contains(repo, "@") {
			t.Fatalf("a digest stayed in the repository: %q", repo)
		}
		if tag != "" && strings.Contains(tag, "/") {
			t.Fatalf("a tag holding a slash: %q", tag)
		}
	})
}
