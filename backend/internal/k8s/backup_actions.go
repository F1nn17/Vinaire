package k8s

import (
	"context"
	"fmt"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"

	"github.com/f1nn17/vinaire/backend/internal/backup"
)

// The write verbs of Roadmap Phase 77b (owner's decision of 2026-10-21): backups of an application
// installed from an image. All are made with the caller's own clients, so the cluster's RBAC decides,
// and each is audited by the handler. Only what internal/backup built is created.

// EnsureBackupClaim creates the claim dumps are written to unless it is already there. An existing
// claim of that name is used as it is.
func EnsureBackupClaim(ctx context.Context, cs *kubernetes.Clientset, pvc *corev1.PersistentVolumeClaim) (created bool, err error) {
	_, err = cs.CoreV1().PersistentVolumeClaims(pvc.Namespace).Get(ctx, pvc.Name, metav1.GetOptions{})
	if err == nil {
		return false, nil
	}
	if !apierrors.IsNotFound(err) {
		return false, fmt.Errorf("get claim %s/%s: %w", pvc.Namespace, pvc.Name, err)
	}
	if _, err = cs.CoreV1().PersistentVolumeClaims(pvc.Namespace).Create(ctx, pvc, metav1.CreateOptions{}); err != nil {
		return false, fmt.Errorf("create claim %s/%s: %w", pvc.Namespace, pvc.Name, err)
	}
	return true, nil
}

// CreateBackupJob creates a one-off dump.
func CreateBackupJob(ctx context.Context, cs *kubernetes.Clientset, job *batchv1.Job) error {
	if _, err := cs.BatchV1().Jobs(job.Namespace).Create(ctx, job, metav1.CreateOptions{}); err != nil {
		return fmt.Errorf("create job %s/%s: %w", job.Namespace, job.Name, err)
	}
	return nil
}

// CreateBackupCronJob creates a scheduled dump.
func CreateBackupCronJob(ctx context.Context, cs *kubernetes.Clientset, cj *batchv1.CronJob) error {
	if _, err := cs.BatchV1().CronJobs(cj.Namespace).Create(ctx, cj, metav1.CreateOptions{}); err != nil {
		return fmt.Errorf("create cronjob %s/%s: %w", cj.Namespace, cj.Name, err)
	}
	return nil
}

// ListBackupObjects lists what Vinaire made as backups of one instance.
func ListBackupObjects(ctx context.Context, cs *kubernetes.Clientset, namespace, instance string) ([]batchv1.Job, []batchv1.CronJob, error) {
	opts := metav1.ListOptions{LabelSelector: backup.LabelBackupOf + "=" + instance + "," + backup.LabelManagedBy + "=vinaire"}
	jobs, err := cs.BatchV1().Jobs(namespace).List(ctx, opts)
	if err != nil {
		return nil, nil, fmt.Errorf("list backup jobs: %w", err)
	}
	crons, err := cs.BatchV1().CronJobs(namespace).List(ctx, opts)
	if err != nil {
		return nil, nil, fmt.Errorf("list backup cronjobs: %w", err)
	}
	return jobs.Items, crons.Items, nil
}

// GetBackupLabels returns the labels of a backup object (kind job|cronjob|snapshot), so a delete can
// check that Vinaire made it.
func GetBackupLabels(ctx context.Context, cs *kubernetes.Clientset, dyn dynamic.Interface, kind, namespace, name string) (map[string]string, error) {
	switch kind {
	case "job":
		o, err := cs.BatchV1().Jobs(namespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return nil, err
		}
		return o.Labels, nil
	case "cronjob":
		o, err := cs.BatchV1().CronJobs(namespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return nil, err
		}
		return o.Labels, nil
	case "snapshot":
		o, err := dyn.Resource(backup.SnapshotGVR).Namespace(namespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return nil, err
		}
		return o.GetLabels(), nil
	}
	return nil, fmt.Errorf("unknown backup kind %q", kind)
}

// DeleteBackupObject deletes one backup object (a Job takes its pods with it, in the background). The
// dump files already written to a claim or a bucket are not touched.
func DeleteBackupObject(ctx context.Context, cs *kubernetes.Clientset, dyn dynamic.Interface, kind, namespace, name string) error {
	var err error
	switch kind {
	case "job":
		err = cs.BatchV1().Jobs(namespace).Delete(ctx, name, deleteOptions())
	case "cronjob":
		err = cs.BatchV1().CronJobs(namespace).Delete(ctx, name, deleteOptions())
	case "snapshot":
		err = dyn.Resource(backup.SnapshotGVR).Namespace(namespace).Delete(ctx, name, metav1.DeleteOptions{})
	default:
		return fmt.Errorf("unknown backup kind %q", kind)
	}
	if err != nil {
		return fmt.Errorf("delete %s %s/%s: %w", kind, namespace, name, err)
	}
	return nil
}

// CreateSnapshot creates a VolumeSnapshot.
func CreateSnapshot(ctx context.Context, dyn dynamic.Interface, snap *unstructured.Unstructured) error {
	if _, err := dyn.Resource(backup.SnapshotGVR).Namespace(snap.GetNamespace()).Create(ctx, snap, metav1.CreateOptions{}); err != nil {
		return fmt.Errorf("create snapshot %s/%s: %w", snap.GetNamespace(), snap.GetName(), err)
	}
	return nil
}

// ListSnapshots lists the snapshots Vinaire made of one instance.
func ListSnapshots(ctx context.Context, dyn dynamic.Interface, namespace, instance string) ([]unstructured.Unstructured, error) {
	l, err := dyn.Resource(backup.SnapshotGVR).Namespace(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: backup.LabelBackupOf + "=" + instance + "," + backup.LabelManagedBy + "=vinaire"})
	if err != nil {
		return nil, fmt.Errorf("list snapshots: %w", err)
	}
	return l.Items, nil
}

// ListSnapshotClasses names the VolumeSnapshotClasses of the cluster.
func ListSnapshotClasses(ctx context.Context, dyn dynamic.Interface) ([]string, error) {
	l, err := dyn.Resource(backup.SnapshotClassGVR).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("list snapshot classes: %w", err)
	}
	out := make([]string, 0, len(l.Items))
	for _, c := range l.Items {
		out = append(out, c.GetName())
	}
	return out, nil
}

// ListRestoreJobs lists the restore Jobs that loaded something into one instance.
func ListRestoreJobs(ctx context.Context, cs *kubernetes.Clientset, namespace, instance string) ([]batchv1.Job, error) {
	l, err := cs.BatchV1().Jobs(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: backup.LabelRestoreInto + "=" + instance + "," + backup.LabelManagedBy + "=vinaire"})
	if err != nil {
		return nil, fmt.Errorf("list restore jobs: %w", err)
	}
	return l.Items, nil
}

// GetSnapshot reads one VolumeSnapshot.
func GetSnapshot(ctx context.Context, dyn dynamic.Interface, namespace, name string) (*unstructured.Unstructured, error) {
	s, err := dyn.Resource(backup.SnapshotGVR).Namespace(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("get snapshot %s/%s: %w", namespace, name, err)
	}
	return s, nil
}

// CreateClaim creates a claim built by internal/backup (a restore of a snapshot into a new claim).
func CreateClaim(ctx context.Context, cs *kubernetes.Clientset, pvc *corev1.PersistentVolumeClaim) error {
	if _, err := cs.CoreV1().PersistentVolumeClaims(pvc.Namespace).Create(ctx, pvc, metav1.CreateOptions{}); err != nil {
		return fmt.Errorf("create claim %s/%s: %w", pvc.Namespace, pvc.Name, err)
	}
	return nil
}
