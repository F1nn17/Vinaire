package backup

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Labels on everything this package builds. They deliberately do not carry
// `app.kubernetes.io/instance`, so a backup is never mistaken for part of the instance (and is not
// removed with it).
const (
	LabelBackupOf   = "vinaire.io/backup-of"
	LabelBackupKind = "vinaire.io/backup-kind" // dump | snapshot
	LabelManagedBy  = "app.kubernetes.io/managed-by"

	// MaxInstanceName keeps `<instance>-backup-<timestamp>` within 63 characters.
	MaxInstanceName = 40
)

// engine describes how one database is dumped.
type engine struct {
	id     string
	repo   *regexp.Regexp // the instance image's repository, to be sure the label is not a lie
	ext    string
	port   string
	keep   []string // environment variables copied from the instance's container
	script string
}

// The scripts are fixed text. Values arrive in environment variables (BACKUP_HOST, BACKUP_PREFIX and
// the instance's own variables); none is spliced into the script.
var engines = []engine{
	{
		id: "postgres", repo: regexp.MustCompile(`^(docker\.io/)?(library/)?postgres$`), ext: "dump", port: "5432",
		keep: []string{"POSTGRES_USER", "POSTGRES_DB", "POSTGRES_PASSWORD"},
		script: `set -eu
f="/backup/${BACKUP_PREFIX}-$(date -u +%Y%m%dT%H%M%SZ).dump"
PGPASSWORD="$POSTGRES_PASSWORD" pg_dump -h "$BACKUP_HOST" -p "$BACKUP_PORT" -U "$POSTGRES_USER" -d "$POSTGRES_DB" -Fc -f "$f.part"
mv "$f.part" "$f"
echo "wrote $f"`,
	},
	{
		id: "mysql", repo: regexp.MustCompile(`^(docker\.io/)?(library/)?mysql$`), ext: "sql", port: "3306",
		keep: []string{"MYSQL_ROOT_PASSWORD"},
		script: `set -eu
f="/backup/${BACKUP_PREFIX}-$(date -u +%Y%m%dT%H%M%SZ).sql"
MYSQL_PWD="$MYSQL_ROOT_PASSWORD" mysqldump -h "$BACKUP_HOST" -P "$BACKUP_PORT" -uroot --single-transaction --all-databases > "$f.part"
mv "$f.part" "$f"
echo "wrote $f"`,
	},
	{
		id: "mariadb", repo: regexp.MustCompile(`^(docker\.io/)?(library/)?mariadb$`), ext: "sql", port: "3306",
		keep: []string{"MARIADB_ROOT_PASSWORD"},
		script: `set -eu
f="/backup/${BACKUP_PREFIX}-$(date -u +%Y%m%dT%H%M%SZ).sql"
MYSQL_PWD="$MARIADB_ROOT_PASSWORD" mariadb-dump -h "$BACKUP_HOST" -P "$BACKUP_PORT" -uroot --single-transaction --all-databases > "$f.part"
mv "$f.part" "$f"
echo "wrote $f"`,
	},
	{
		id: "redis", repo: regexp.MustCompile(`^(docker\.io/)?(library/)?redis$`), ext: "rdb", port: "6379",
		keep: []string{"REDIS_PASSWORD"},
		script: `set -eu
f="/backup/${BACKUP_PREFIX}-$(date -u +%Y%m%dT%H%M%SZ).rdb"
REDISCLI_AUTH="$REDIS_PASSWORD" redis-cli -h "$BACKUP_HOST" -p "$BACKUP_PORT" ${REDIS_USER:+--user "$REDIS_USER"} --rdb "$f.part"
mv "$f.part" "$f"
echo "wrote $f"`,
	},
	{
		id: "valkey", repo: regexp.MustCompile(`^(docker\.io/)?(valkey/)?valkey$`), ext: "rdb", port: "6379",
		keep: []string{"VALKEY_PASSWORD"},
		script: `set -eu
f="/backup/${BACKUP_PREFIX}-$(date -u +%Y%m%dT%H%M%SZ).rdb"
REDISCLI_AUTH="$VALKEY_PASSWORD" valkey-cli -h "$BACKUP_HOST" -p "$BACKUP_PORT" ${REDIS_USER:+--user "$REDIS_USER"} --rdb "$f.part"
mv "$f.part" "$f"
echo "wrote $f"`,
	},
}

// uploadScript copies what the dump wrote to the bucket. Endpoint, bucket and prefix come from the
// operator's configuration, the credentials from the Secret the request names.
const uploadScript = `set -eu
for f in /backup/*; do
  aws s3 cp "$f" "s3://${S3_BUCKET}/${S3_PREFIX}${f##*/}" --endpoint-url "$S3_ENDPOINT" --only-show-errors
  echo "uploaded ${f##*/}"
done`

// Source is what a dump is built from: an instance's StatefulSet, read as the caller.
type Source struct {
	Namespace, Instance string
	Engine              string
	Image               string
	env                 []corev1.EnvVar
	eng                 engine
	secret              string // the instance's Secret, for the optional Redis user
}

// SourceFrom recognises an instance created by "Install from an image" that this package can dump.
// The label and the image repository must agree, so a label alone cannot point a Job at something else.
func SourceFrom(sts appsv1.StatefulSet) (Source, error) {
	if sts.Labels[LabelManagedBy] != "vinaire" {
		return Source{}, fmt.Errorf("only instances installed from an image can be dumped; use a snapshot for others")
	}
	if len(sts.Name) > MaxInstanceName {
		return Source{}, fmt.Errorf("instance name is too long for a backup (%d characters at most)", MaxInstanceName)
	}
	var app *corev1.Container
	for i := range sts.Spec.Template.Spec.Containers {
		if sts.Spec.Template.Spec.Containers[i].Name == "app" {
			app = &sts.Spec.Template.Spec.Containers[i]
		}
	}
	if app == nil {
		return Source{}, fmt.Errorf("the instance has no container named app")
	}
	repo := app.Image
	if i := strings.LastIndex(repo, ":"); i > strings.LastIndex(repo, "/") {
		repo = repo[:i]
	}
	for _, e := range engines {
		if sts.Labels["app.kubernetes.io/name"] != e.id || !e.repo.MatchString(repo) {
			continue
		}
		if !imageRe.MatchString(app.Image) {
			return Source{}, fmt.Errorf("the instance image has an unexpected form")
		}
		s := Source{Namespace: sts.Namespace, Instance: sts.Name, Engine: e.id, Image: app.Image, eng: e}
		for _, name := range e.keep {
			found := false
			for _, v := range app.Env {
				if v.Name == name {
					s.env = append(s.env, v)
					found = true
					if v.ValueFrom != nil && v.ValueFrom.SecretKeyRef != nil {
						s.secret = v.ValueFrom.SecretKeyRef.Name
					}
				}
			}
			if !found && strings.HasSuffix(name, "PASSWORD") {
				return Source{}, fmt.Errorf("the instance does not set %s", name)
			}
		}
		// postgres needs its user/db even when they are plain values; others only the password
		return s, nil
	}
	return Source{}, fmt.Errorf("a dump is supported for PostgreSQL, MySQL, MariaDB, Redis and Valkey installed from an image")
}

// Target says where a dump is written.
type Target struct {
	// PVC (default) writes to a claim in the instance's namespace, named PVCName(instance).
	// S3 uploads to a configured target, with credentials from CredentialsSecret.
	S3                *S3Target
	CredentialsSecret string
	UploadImage       string
}

// PVCName is the claim dumps are written to when the target is a PVC.
func PVCName(instance string) string { return instance + "-backups" }

var sizeRe = regexp.MustCompile(`^[1-9][0-9]{0,3}(Mi|Gi|Ti)$`)

// ValidSize reports whether a requested claim size is acceptable.
func ValidSize(s string) bool { return sizeRe.MatchString(s) }

// ClaimFor builds the claim a PVC target writes to.
func ClaimFor(src Source, size string) (*corev1.PersistentVolumeClaim, error) {
	if !ValidSize(size) {
		return nil, fmt.Errorf("size must look like 5Gi")
	}
	return &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name: PVCName(src.Instance), Namespace: src.Namespace,
			Labels: map[string]string{LabelManagedBy: "vinaire", LabelBackupOf: src.Instance, LabelBackupKind: "dump"},
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
			Resources: corev1.VolumeResourceRequirements{
				Requests: corev1.ResourceList{corev1.ResourceStorage: resource.MustParse(size)},
			},
		},
	}, nil
}

func secure(user int64) *corev1.SecurityContext {
	f, t := false, true
	return &corev1.SecurityContext{
		AllowPrivilegeEscalation: &f,
		RunAsNonRoot:             &t,
		RunAsUser:                &user,
		Capabilities:             &corev1.Capabilities{Drop: []corev1.Capability{"ALL"}},
		SeccompProfile:           &corev1.SeccompProfile{Type: corev1.SeccompProfileTypeRuntimeDefault},
	}
}

func str(name, value string) corev1.EnvVar { return corev1.EnvVar{Name: name, Value: value} }

// Labels of a dump's Job / CronJob.
func Labels(instance string) map[string]string {
	return map[string]string{LabelManagedBy: "vinaire", LabelBackupOf: instance, LabelBackupKind: "dump"}
}

// PodSpec is the pod a dump runs in. For a PVC target the dump writes straight to the claim; for S3
// it writes to an emptyDir in an init container and a second container uploads it.
func PodSpec(src Source, tgt Target) (corev1.PodSpec, error) {
	const uid = 999
	env := []corev1.EnvVar{
		str("HOME", "/tmp"),
		str("BACKUP_HOST", fmt.Sprintf("%s.%s.svc.cluster.local", src.Instance, src.Namespace)),
		str("BACKUP_PORT", src.eng.port),
		str("BACKUP_PREFIX", src.Instance+"-"+src.Engine),
	}
	env = append(env, src.env...)
	if (src.Engine == "redis" || src.Engine == "valkey") && src.secret != "" {
		t := true
		env = append(env, corev1.EnvVar{Name: "REDIS_USER", ValueFrom: &corev1.EnvVarSource{
			SecretKeyRef: &corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: src.secret}, Key: "username", Optional: &t},
		}})
	}
	tmp := corev1.VolumeMount{Name: "tmp", MountPath: "/tmp"}
	out := corev1.VolumeMount{Name: "backup", MountPath: "/backup"}
	dump := corev1.Container{
		Name: "dump", Image: src.Image, Command: []string{"sh", "-c", src.eng.script},
		Env: env, SecurityContext: secure(uid), VolumeMounts: []corev1.VolumeMount{tmp, out},
		Resources: corev1.ResourceRequirements{Requests: corev1.ResourceList{
			corev1.ResourceCPU: resource.MustParse("50m"), corev1.ResourceMemory: resource.MustParse("128Mi")}},
	}
	ps := corev1.PodSpec{
		RestartPolicy: corev1.RestartPolicyNever,
		SecurityContext: &corev1.PodSecurityContext{
			FSGroup: func() *int64 { v := int64(uid); return &v }(),
		},
		Volumes: []corev1.Volume{{Name: "tmp", VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}}}},
	}
	if tgt.S3 == nil {
		ps.Volumes = append(ps.Volumes, corev1.Volume{Name: "backup", VolumeSource: corev1.VolumeSource{
			PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: PVCName(src.Instance)}}})
		ps.Containers = []corev1.Container{dump}
		return ps, nil
	}
	if !nameRe.MatchString(tgt.CredentialsSecret) {
		return ps, fmt.Errorf("name the Secret that holds the S3 credentials (keys AWS_ACCESS_KEY_ID and AWS_SECRET_ACCESS_KEY)")
	}
	prefix := ""
	if tgt.S3.Prefix != "" {
		prefix = strings.Trim(tgt.S3.Prefix, "/") + "/"
	}
	ref := func(key string) corev1.EnvVar {
		return corev1.EnvVar{Name: key, ValueFrom: &corev1.EnvVarSource{SecretKeyRef: &corev1.SecretKeySelector{
			LocalObjectReference: corev1.LocalObjectReference{Name: tgt.CredentialsSecret}, Key: key}}}
	}
	region := tgt.S3.Region
	if region == "" {
		region = "us-east-1"
	}
	ps.Volumes = append(ps.Volumes, corev1.Volume{Name: "backup", VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}}})
	ps.InitContainers = []corev1.Container{dump}
	ps.Containers = []corev1.Container{{
		Name: "upload", Image: tgt.UploadImage, Command: []string{"sh", "-c", uploadScript},
		Env: []corev1.EnvVar{
			str("HOME", "/tmp"), str("S3_ENDPOINT", tgt.S3.Endpoint), str("S3_BUCKET", tgt.S3.Bucket),
			str("S3_PREFIX", prefix), str("AWS_DEFAULT_REGION", region),
			ref("AWS_ACCESS_KEY_ID"), ref("AWS_SECRET_ACCESS_KEY"),
		},
		SecurityContext: secure(uid), VolumeMounts: []corev1.VolumeMount{tmp, out},
	}}
	return ps, nil
}

// Job builds a one-off dump.
func Job(src Source, tgt Target, now time.Time) (*batchv1.Job, error) {
	ps, err := PodSpec(src, tgt)
	if err != nil {
		return nil, err
	}
	one, deadline := int32(1), int64(3600)
	return &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name: src.Instance + "-backup-" + now.UTC().Format("20060102-150405"), Namespace: src.Namespace,
			Labels: Labels(src.Instance),
		},
		Spec: batchv1.JobSpec{
			BackoffLimit: &one, ActiveDeadlineSeconds: &deadline,
			Template: corev1.PodTemplateSpec{ObjectMeta: metav1.ObjectMeta{Labels: Labels(src.Instance)}, Spec: ps},
		},
	}, nil
}

var cronRe = regexp.MustCompile(`^[0-9*/,-]+( [0-9*/,A-Za-z-]+){4}$`)

// ValidSchedule reports whether a cron expression has the five-field form (the API server checks the rest).
func ValidSchedule(s string) bool { return len(s) <= 64 && cronRe.MatchString(s) }

// CronJob builds a scheduled dump, one per instance.
func CronJob(src Source, tgt Target, schedule string) (*batchv1.CronJob, error) {
	if !ValidSchedule(schedule) {
		return nil, fmt.Errorf("schedule must be a five-field cron expression, e.g. 0 3 * * *")
	}
	ps, err := PodSpec(src, tgt)
	if err != nil {
		return nil, err
	}
	one, deadline := int32(1), int64(3600)
	keepOK, keepFail := int32(3), int32(1)
	return &batchv1.CronJob{
		ObjectMeta: metav1.ObjectMeta{Name: src.Instance + "-backup", Namespace: src.Namespace, Labels: Labels(src.Instance)},
		Spec: batchv1.CronJobSpec{
			Schedule: schedule, ConcurrencyPolicy: batchv1.ForbidConcurrent,
			SuccessfulJobsHistoryLimit: &keepOK, FailedJobsHistoryLimit: &keepFail,
			JobTemplate: batchv1.JobTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: Labels(src.Instance)},
				Spec: batchv1.JobSpec{
					BackoffLimit: &one, ActiveDeadlineSeconds: &deadline,
					Template: corev1.PodTemplateSpec{ObjectMeta: metav1.ObjectMeta{Labels: Labels(src.Instance)}, Spec: ps},
				},
			},
		},
	}, nil
}
