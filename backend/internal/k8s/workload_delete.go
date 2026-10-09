package k8s

import (
	"context"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// Deleting a StatefulSet, a DaemonSet or a Job (the owner's decision of 2026-10-09: the multi-select
// in the Workloads lists works on every tab, and so does the Delete on their pages). Each is a
// plain Delete() with background propagation — the same as `kubectl delete` — so the pods they own
// are removed by the garbage collector. (A Job needs it spelled out: client-go's default for batch/v1
// is to orphan the pods.) The volume claims a StatefulSet created stay, as with kubectl.

var background = metav1.DeletePropagationBackground

func deleteOptions() metav1.DeleteOptions {
	return metav1.DeleteOptions{PropagationPolicy: &background}
}

// DeleteStatefulSet deletes a StatefulSet and, in the background, its pods.
func DeleteStatefulSet(ctx context.Context, clientset *kubernetes.Clientset, namespace, name string) error {
	if err := clientset.AppsV1().StatefulSets(namespace).Delete(ctx, name, deleteOptions()); err != nil {
		return fmt.Errorf("delete statefulset %s/%s: %w", namespace, name, err)
	}
	return nil
}

// DeleteDaemonSet deletes a DaemonSet and, in the background, its pods.
func DeleteDaemonSet(ctx context.Context, clientset *kubernetes.Clientset, namespace, name string) error {
	if err := clientset.AppsV1().DaemonSets(namespace).Delete(ctx, name, deleteOptions()); err != nil {
		return fmt.Errorf("delete daemonset %s/%s: %w", namespace, name, err)
	}
	return nil
}

// DeleteJob deletes a Job and, in the background, its pods.
func DeleteJob(ctx context.Context, clientset *kubernetes.Clientset, namespace, name string) error {
	if err := clientset.BatchV1().Jobs(namespace).Delete(ctx, name, deleteOptions()); err != nil {
		return fmt.Errorf("delete job %s/%s: %w", namespace, name, err)
	}
	return nil
}
