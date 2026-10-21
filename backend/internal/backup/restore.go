package backup

import (
	"fmt"
	"regexp"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Labels of a restore Job. They say what it loaded and where; like a backup, a restore carries no
// `app.kubernetes.io/instance`.
const (
	LabelRestoreInto = "vinaire.io/restore-into"
	LabelRestoreOf   = "vinaire.io/restore-of"
)

var fileRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)

// ValidFile reports whether a dump file name is acceptable (a plain name, no path).
func ValidFile(s string) bool { return fileRe.MatchString(s) }

// CanRestore says whether dumps of this source's engine can be loaded by a Job. Redis and Valkey cannot:
// a running server does not load an RDB file, so those are restored from a snapshot or by hand.
func (s Source) CanRestore() bool { return s.eng.restore != "" }

// RestoreJob loads a dump written for instance `from` into the (new) instance `into`. The dump is read
// from the claim `<from>-backups` in the same namespace, mounted read-only; the client runs in the
// target's own image and signs in with the target's Secret (a reference, never read by Vinaire). With
// file empty the newest dump of `from` is used.
func RestoreJob(into Source, from, file string, now time.Time) (*batchv1.Job, error) {
	if !into.CanRestore() {
		return nil, fmt.Errorf("a %s dump cannot be loaded by a Job; restore from a volume snapshot instead", into.Engine)
	}
	if !nameRe.MatchString(from) || len(from) > MaxInstanceName {
		return nil, fmt.Errorf("invalid source instance name")
	}
	if from == into.Instance {
		return nil, fmt.Errorf("restore goes into a new instance, never over the one the dump came from")
	}
	if file != "" && !ValidFile(file) {
		return nil, fmt.Errorf("invalid dump file name")
	}
	const uid = 999
	script := `set -eu
f="$RESTORE_FILE"
if [ -z "$f" ]; then f=$(ls -1t /backup/"$RESTORE_PREFIX"-*.` + into.eng.ext + ` 2>/dev/null | head -n1); else f="/backup/$f"; fi
[ -f "$f" ] || { echo "no dump found"; exit 1; }
echo "restoring $f"
` + into.eng.restore + `
echo "done"`
	env := []corev1.EnvVar{
		str("HOME", "/tmp"),
		str("BACKUP_HOST", fmt.Sprintf("%s.%s.svc.cluster.local", into.Instance, into.Namespace)),
		str("BACKUP_PORT", into.eng.port),
		str("RESTORE_PREFIX", from+"-"+into.Engine),
		str("RESTORE_FILE", file),
	}
	env = append(env, into.env...)
	one, deadline := int32(1), int64(7200)
	labels := map[string]string{LabelManagedBy: "vinaire", LabelRestoreInto: into.Instance, LabelRestoreOf: from}
	return &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{Name: into.Instance + "-restore-" + now.UTC().Format("20060102-150405"), Namespace: into.Namespace, Labels: labels},
		Spec: batchv1.JobSpec{
			BackoffLimit: &one, ActiveDeadlineSeconds: &deadline,
			Template: corev1.PodTemplateSpec{ObjectMeta: metav1.ObjectMeta{Labels: labels}, Spec: corev1.PodSpec{
				RestartPolicy:   corev1.RestartPolicyNever,
				SecurityContext: &corev1.PodSecurityContext{FSGroup: func() *int64 { v := int64(uid); return &v }()},
				Volumes: []corev1.Volume{
					{Name: "tmp", VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}}},
					{Name: "backup", VolumeSource: corev1.VolumeSource{PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
						ClaimName: PVCName(from), ReadOnly: true}}},
				},
				Containers: []corev1.Container{{
					Name: "restore", Image: into.Image, Command: []string{"sh", "-c", script}, Env: env,
					SecurityContext: secure(uid),
					VolumeMounts: []corev1.VolumeMount{
						{Name: "tmp", MountPath: "/tmp"},
						{Name: "backup", MountPath: "/backup", ReadOnly: true},
					},
				}},
			}},
		},
	}, nil
}

// ClaimFromSnapshot builds a new claim whose data is a VolumeSnapshot's — the way to bring a snapshot
// back without touching the claim it was taken from. size is the snapshot's restore size (or larger).
func ClaimFromSnapshot(namespace, instance, claim, snapshot, size string) (*corev1.PersistentVolumeClaim, error) {
	if !nameRe.MatchString(claim) || !nameRe.MatchString(snapshot) {
		return nil, fmt.Errorf("invalid claim or snapshot name")
	}
	if !ValidSize(size) {
		return nil, fmt.Errorf("size must look like 5Gi")
	}
	group := "snapshot.storage.k8s.io"
	return &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{Name: claim, Namespace: namespace,
			Labels: map[string]string{LabelManagedBy: "vinaire", LabelRestoreOf: instance}},
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
			Resources: corev1.VolumeResourceRequirements{
				Requests: corev1.ResourceList{corev1.ResourceStorage: resource.MustParse(size)}},
			DataSource: &corev1.TypedLocalObjectReference{APIGroup: &group, Kind: "VolumeSnapshot", Name: snapshot},
		},
	}, nil
}
