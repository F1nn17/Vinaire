package backup

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func sts(app, image string, env ...corev1.EnvVar) appsv1.StatefulSet {
	return appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{Name: "db", Namespace: "apps",
			Labels: map[string]string{LabelManagedBy: "vinaire", "app.kubernetes.io/name": app}},
		Spec: appsv1.StatefulSetSpec{Template: corev1.PodTemplateSpec{Spec: corev1.PodSpec{
			Containers: []corev1.Container{{Name: "app", Image: image, Env: env}}}}},
	}
}

func secretEnv(name string) corev1.EnvVar {
	return corev1.EnvVar{Name: name, ValueFrom: &corev1.EnvVarSource{SecretKeyRef: &corev1.SecretKeySelector{
		LocalObjectReference: corev1.LocalObjectReference{Name: "db"}, Key: "password"}}}
}

func pg() appsv1.StatefulSet {
	return sts("postgres", "postgres:17", corev1.EnvVar{Name: "POSTGRES_USER", Value: "app"},
		corev1.EnvVar{Name: "POSTGRES_DB", Value: "app"}, secretEnv("POSTGRES_PASSWORD"),
		corev1.EnvVar{Name: "PGDATA", Value: "/x"})
}

func TestLabelAndImageMustAgree(t *testing.T) {
	if _, err := SourceFrom(sts("postgres", "evil/postgres:17", secretEnv("POSTGRES_PASSWORD"))); err == nil {
		t.Fatal("an image from another repository must be refused")
	}
	if _, err := SourceFrom(sts("redis", "postgres:17")); err == nil {
		t.Fatal("a redis label on a postgres image must be refused")
	}
	other := pg()
	other.Labels[LabelManagedBy] = "Helm"
	if _, err := SourceFrom(other); err == nil {
		t.Fatal("only image installs are dumped")
	}
}

func TestPVCDumpUsesSecretRefsAndNoPlainPassword(t *testing.T) {
	src, err := SourceFrom(pg())
	if err != nil {
		t.Fatal(err)
	}
	job, err := Job(src, Target{}, time.Date(2026, 10, 21, 1, 2, 3, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if job.Name != "db-backup-20261021-010203" {
		t.Errorf("name %s", job.Name)
	}
	c := job.Spec.Template.Spec.Containers[0]
	for _, e := range c.Env {
		if e.Name == "POSTGRES_PASSWORD" && (e.Value != "" || e.ValueFrom == nil) {
			t.Error("the password must be a secretKeyRef")
		}
		if e.Name == "PGDATA" {
			t.Error("only the whitelisted variables are copied")
		}
	}
	if job.Spec.Template.Spec.Volumes[1].PersistentVolumeClaim.ClaimName != "db-backups" {
		t.Error("PVC target must use the derived claim")
	}
	if job.Labels["app.kubernetes.io/instance"] != "" {
		t.Error("a backup must not look like part of the instance")
	}
	if !*c.SecurityContext.RunAsNonRoot || *c.SecurityContext.AllowPrivilegeEscalation {
		t.Error("the dump must run unprivileged")
	}
}

func TestS3NeedsConfiguredTargetAndSecretName(t *testing.T) {
	src, _ := SourceFrom(pg())
	tg := &S3Target{Name: "main", Endpoint: "https://s3.example.com", Bucket: "bk", Prefix: "vinaire"}
	if _, err := PodSpec(src, Target{S3: tg, UploadImage: DefaultUploadImage}); err == nil {
		t.Fatal("credentials Secret is required")
	}
	if _, err := PodSpec(src, Target{S3: tg, CredentialsSecret: "Bad Name", UploadImage: DefaultUploadImage}); err == nil {
		t.Fatal("secret name is validated")
	}
	ps, err := PodSpec(src, Target{S3: tg, CredentialsSecret: "s3-creds", UploadImage: DefaultUploadImage})
	if err != nil {
		t.Fatal(err)
	}
	if len(ps.InitContainers) != 1 || ps.Containers[0].Name != "upload" {
		t.Fatalf("unexpected pod: %+v", ps)
	}
	for _, e := range ps.Containers[0].Env {
		if e.Name == "AWS_SECRET_ACCESS_KEY" && e.ValueFrom == nil {
			t.Error("the S3 secret must be a secretKeyRef")
		}
	}
}

func TestRedisUsesOptionalACLUser(t *testing.T) {
	src, err := SourceFrom(sts("redis", "redis:8", secretEnv("REDIS_PASSWORD")))
	if err != nil {
		t.Fatal(err)
	}
	ps, _ := PodSpec(src, Target{})
	found := false
	for _, e := range ps.Containers[0].Env {
		if e.Name == "REDIS_USER" && *e.ValueFrom.SecretKeyRef.Optional {
			found = true
		}
	}
	if !found {
		t.Error("REDIS_USER should come from the instance Secret, optionally")
	}
}

func TestScheduleAndSizeValidation(t *testing.T) {
	for _, ok := range []string{"0 3 * * *", "*/15 * * * *", "0 2 * * mon"} {
		if !ValidSchedule(ok) {
			t.Errorf("%q should be valid", ok)
		}
	}
	for _, bad := range []string{"", "0 3 * *", "0 3 * * *; rm -rf /", "$(x) * * * *"} {
		if ValidSchedule(bad) {
			t.Errorf("%q should be refused", bad)
		}
	}
	if !ValidSize("5Gi") || ValidSize("0Gi") || ValidSize("5GB") || ValidSize("5Gi\nx") {
		t.Error("size validation")
	}
}

func TestConfigRefusesUnsafeTargets(t *testing.T) {
	dir := t.TempDir()
	write := func(body string) string {
		p := filepath.Join(dir, "c.json")
		_ = os.WriteFile(p, []byte(body), 0o600)
		return p
	}
	good := `{"targets":[{"name":"main","endpoint":"https://s3.example.com","bucket":"my-bucket","prefix":"a/b"}]}`
	if c, err := LoadConfig(write(good)); err != nil || len(c.Targets) != 1 {
		t.Fatalf("good config: %v", err)
	}
	for _, bad := range []string{
		`{"targets":[{"name":"m","endpoint":"http://s3.example.com","bucket":"my-bucket"}]}`,
		`{"targets":[{"name":"m","endpoint":"https://127.0.0.1","bucket":"my-bucket"}]}`,
		`{"targets":[{"name":"m","endpoint":"https://169.254.169.254","bucket":"my-bucket"}]}`,
		`{"targets":[{"name":"m","endpoint":"https://u:p@s3.example.com","bucket":"my-bucket"}]}`,
		`{"targets":[{"name":"m","endpoint":"https://s3.example.com","bucket":"my-bucket","prefix":"../x"}]}`,
		`{"targets":[{"name":"m","endpoint":"https://s3.example.com","bucket":"b;rm"}]}`,
		`{"targets":[{"name":"m","endpoint":"https://s3.example.com","bucket":"my-bucket"},{"name":"m","endpoint":"https://s3.example.com","bucket":"my-bucket"}]}`,
		`{"unknown":1}`,
	} {
		if _, err := LoadConfig(write(bad)); err == nil {
			t.Errorf("should be refused: %s", bad)
		}
	}
	if !strings.Contains(DefaultUploadImage, ":") {
		t.Error("the upload image must be pinned")
	}
}

func TestRestoreIsIntoANewInstanceFromAValidatedSource(t *testing.T) {
	into, err := SourceFrom(pg())
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 21, 1, 2, 3, 0, time.UTC)
	if _, err := RestoreJob(into, "db", "", now); err == nil {
		t.Fatal("restoring over the source itself must be refused")
	}
	for _, bad := range []string{"Bad Name", "a/b", ""} {
		if _, err := RestoreJob(into, bad, "", now); err == nil {
			t.Errorf("source %q should be refused", bad)
		}
	}
	for _, bad := range []string{"../x.dump", "a b", "x;rm"} {
		if _, err := RestoreJob(into, "old", bad, now); err == nil {
			t.Errorf("file %q should be refused", bad)
		}
	}
	job, err := RestoreJob(into, "old", "", now)
	if err != nil {
		t.Fatal(err)
	}
	vol := job.Spec.Template.Spec.Volumes[1].PersistentVolumeClaim
	if vol.ClaimName != "old-backups" || !vol.ReadOnly {
		t.Errorf("the dump claim must be mounted read-only: %+v", vol)
	}
	if job.Labels["app.kubernetes.io/instance"] != "" || job.Labels[LabelRestoreInto] != "db" {
		t.Errorf("labels: %v", job.Labels)
	}
}

func TestRedisDumpsCannotBeRestoredByAJob(t *testing.T) {
	into, _ := SourceFrom(sts("redis", "redis:8", secretEnv("REDIS_PASSWORD")))
	if _, err := RestoreJob(into, "old", "", time.Now()); err == nil {
		t.Fatal("redis restore from a dump must be refused")
	}
}

func TestClaimFromSnapshot(t *testing.T) {
	c, err := ClaimFromSnapshot("apps", "db", "db-restored", "data-db-0-snap-1", "5Gi")
	if err != nil || c.Spec.DataSource.Kind != "VolumeSnapshot" || *c.Spec.DataSource.APIGroup != "snapshot.storage.k8s.io" {
		t.Fatalf("unexpected: %+v %v", c, err)
	}
	if _, err := ClaimFromSnapshot("apps", "db", "Bad_Name", "s", "5Gi"); err == nil {
		t.Error("claim name is validated")
	}
	if _, err := ClaimFromSnapshot("apps", "db", "ok", "s", "lots"); err == nil {
		t.Error("size is validated")
	}
}
