package k8s

import (
	"context"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// DeletePod is Roadmap Phase 33's fourth write action — deleting a Pod
// outright. If the pod is owned by a controller (a ReplicaSet,
// StatefulSet, DaemonSet, Job, ...), that controller recreates it, making
// this effectively the same as a restart; a pod with no owner is gone for
// good. Telling those two cases apart for the caller is internal/api's
// job (see PodDetailResponse.HasController) — this function just deletes.
func DeletePod(ctx context.Context, clientset *kubernetes.Clientset, namespace, name string) error {
	if err := clientset.CoreV1().Pods(namespace).Delete(ctx, name, metav1.DeleteOptions{}); err != nil {
		return fmt.Errorf("delete pod %s/%s: %w", namespace, name, err)
	}
	return nil
}
