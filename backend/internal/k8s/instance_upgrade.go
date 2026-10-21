package k8s

import (
	"context"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
)

// PatchStatefulSetImage applies a strategic merge patch built by internal/instances (Roadmap Phase 77d:
// a new tag for the instance's own image, plus the annotation that remembers the old one). Made as the
// caller; the StatefulSet controller then rolls the pod.
func PatchStatefulSetImage(ctx context.Context, cs *kubernetes.Clientset, namespace, name string, patch []byte) error {
	if _, err := cs.AppsV1().StatefulSets(namespace).Patch(ctx, name, types.StrategicMergePatchType, patch, metav1.PatchOptions{}); err != nil {
		return fmt.Errorf("patch statefulset %s/%s: %w", namespace, name, err)
	}
	return nil
}
