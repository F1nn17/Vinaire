package stream

import (
	"context"

	"k8s.io/client-go/kubernetes"

	"github.com/f1nn17/vinaire/backend/internal/k8s"
)

// resolveContainer defaults to the pod's first container when none is
// explicitly requested, mirroring `kubectl logs`/`kubectl exec`'s behavior
// for single-container pods (and picking a reasonable default rather than
// erroring for multi-container ones).
func resolveContainer(ctx context.Context, clientset *kubernetes.Clientset, namespace, name, container string) string {
	if container != "" {
		return container
	}
	pod, err := k8s.GetPod(ctx, clientset, namespace, name)
	if err != nil || len(pod.Spec.Containers) == 0 {
		return ""
	}
	return pod.Spec.Containers[0].Name
}
