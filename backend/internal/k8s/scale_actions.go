package k8s

import (
	"context"
	"encoding/json"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
)

// This file holds Roadmap Phase 33's write actions — Scale (Deployment/
// StatefulSet) and Pause/Resume rollout (Deployment) — an explicit,
// deliberate extension to AGENTS.md's core rule alongside Phase 9's
// original three. See AGENTS.md before adding another write verb here.

// ScaleDeployment sets a Deployment's replica count via the Scale
// subresource (`UpdateScale`), the native Kubernetes mechanism for this —
// not a direct patch of spec.replicas. GetScale first so UpdateScale
// carries the current ResourceVersion, the same optimistic-concurrency
// pattern `kubectl scale` itself relies on.
func ScaleDeployment(ctx context.Context, clientset *kubernetes.Clientset, namespace, name string, replicas int32) error {
	scale, err := clientset.AppsV1().Deployments(namespace).GetScale(ctx, name, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("get scale for deployment %s/%s: %w", namespace, name, err)
	}
	scale.Spec.Replicas = replicas
	if _, err := clientset.AppsV1().Deployments(namespace).UpdateScale(ctx, name, scale, metav1.UpdateOptions{}); err != nil {
		return fmt.Errorf("scale deployment %s/%s to %d: %w", namespace, name, replicas, err)
	}
	return nil
}

// ScaleStatefulSet is ScaleDeployment's StatefulSet counterpart. Note for
// callers: a StatefulSet scale-down terminates pods one at a time, in
// descending ordinal order — unlike a Deployment, where termination order
// isn't guaranteed.
func ScaleStatefulSet(ctx context.Context, clientset *kubernetes.Clientset, namespace, name string, replicas int32) error {
	scale, err := clientset.AppsV1().StatefulSets(namespace).GetScale(ctx, name, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("get scale for statefulset %s/%s: %w", namespace, name, err)
	}
	scale.Spec.Replicas = replicas
	if _, err := clientset.AppsV1().StatefulSets(namespace).UpdateScale(ctx, name, scale, metav1.UpdateOptions{}); err != nil {
		return fmt.Errorf("scale statefulset %s/%s to %d: %w", namespace, name, replicas, err)
	}
	return nil
}

// PauseDeploymentRollout and ResumeDeploymentRollout are the equivalent of
// `kubectl rollout pause`/`resume` — patching spec.paused so a series of
// edits (e.g. applying new YAML via a future phase) doesn't each trigger
// their own intermediate rollout.
func PauseDeploymentRollout(ctx context.Context, clientset *kubernetes.Clientset, namespace, name string) error {
	return patchDeploymentPaused(ctx, clientset, namespace, name, true)
}

func ResumeDeploymentRollout(ctx context.Context, clientset *kubernetes.Clientset, namespace, name string) error {
	return patchDeploymentPaused(ctx, clientset, namespace, name, false)
}

func patchDeploymentPaused(ctx context.Context, clientset *kubernetes.Clientset, namespace, name string, paused bool) error {
	patch := map[string]any{"spec": map[string]any{"paused": paused}}
	patchBytes, err := json.Marshal(patch)
	if err != nil {
		return fmt.Errorf("marshal pause patch: %w", err)
	}

	if _, err := clientset.AppsV1().Deployments(namespace).Patch(
		ctx, name, types.StrategicMergePatchType, patchBytes, metav1.PatchOptions{},
	); err != nil {
		verb := "pause"
		if !paused {
			verb = "resume"
		}
		return fmt.Errorf("%s deployment %s/%s: %w", verb, namespace, name, err)
	}
	return nil
}
