package k8s

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
)

// This file holds exactly the three write actions AGENTS.md's core rule
// names as the deliberate, audited exception to internal/k8s's otherwise
// read-only default (Roadmap Phase 9: Restart / Delete / Rollback on
// Deployments). Do not add another write verb here, or to any other file
// in this package, without raising it first — see AGENTS.md.

// RestartDeployment triggers a rolling restart the same way
// `kubectl rollout restart` does: patch the pod template's restartedAt
// annotation so the Deployment controller sees a spec change and rolls new
// pods, without changing anything else about the spec.
func RestartDeployment(ctx context.Context, clientset *kubernetes.Clientset, namespace, name string) error {
	patch := map[string]any{
		"spec": map[string]any{
			"template": map[string]any{
				"metadata": map[string]any{
					"annotations": map[string]any{
						"kubectl.kubernetes.io/restartedAt": time.Now().Format(time.RFC3339),
					},
				},
			},
		},
	}
	patchBytes, err := json.Marshal(patch)
	if err != nil {
		return fmt.Errorf("marshal restart patch: %w", err)
	}

	if _, err := clientset.AppsV1().Deployments(namespace).Patch(
		ctx, name, types.StrategicMergePatchType, patchBytes, metav1.PatchOptions{},
	); err != nil {
		return fmt.Errorf("restart deployment %s/%s: %w", namespace, name, err)
	}
	return nil
}

// DeleteDeployment deletes a Deployment outright. It does not touch the
// ReplicaSets/Pods it owns directly — the garbage collector cleans those up
// via the Deployment's owner references, same as `kubectl delete deployment`.
func DeleteDeployment(ctx context.Context, clientset *kubernetes.Clientset, namespace, name string) error {
	if err := clientset.AppsV1().Deployments(namespace).Delete(ctx, name, metav1.DeleteOptions{}); err != nil {
		return fmt.Errorf("delete deployment %s/%s: %w", namespace, name, err)
	}
	return nil
}

// RollbackDeployment reverts a Deployment's pod template to the one from an
// earlier revision, found via the ReplicaSet carrying that revision number
// in its deployment.kubernetes.io/revision annotation — the same mechanism
// `kubectl rollout undo --to-revision=N` uses internally.
func RollbackDeployment(ctx context.Context, clientset *kubernetes.Clientset, namespace, name string, revision int64) error {
	deployment, err := clientset.AppsV1().Deployments(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("get deployment %s/%s: %w", namespace, name, err)
	}

	selector, err := metav1.LabelSelectorAsSelector(deployment.Spec.Selector)
	if err != nil {
		return fmt.Errorf("parse deployment %s/%s selector: %w", namespace, name, err)
	}

	replicaSets, err := clientset.AppsV1().ReplicaSets(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: selector.String(),
	})
	if err != nil {
		return fmt.Errorf("list replicasets for %s/%s: %w", namespace, name, err)
	}

	target := findReplicaSetByRevision(replicaSets.Items, deployment.UID, revision)
	if target == nil {
		return fmt.Errorf("no replicaset found for %s/%s at revision %d", namespace, name, revision)
	}

	patch := map[string]any{
		"spec": map[string]any{
			"template": target.Spec.Template,
		},
	}
	patchBytes, err := json.Marshal(patch)
	if err != nil {
		return fmt.Errorf("marshal rollback patch: %w", err)
	}

	if _, err := clientset.AppsV1().Deployments(namespace).Patch(
		ctx, name, types.StrategicMergePatchType, patchBytes, metav1.PatchOptions{},
	); err != nil {
		return fmt.Errorf("rollback deployment %s/%s to revision %d: %w", namespace, name, revision, err)
	}
	return nil
}

func findReplicaSetByRevision(replicaSets []appsv1.ReplicaSet, deploymentUID types.UID, revision int64) *appsv1.ReplicaSet {
	want := fmt.Sprintf("%d", revision)
	for i := range replicaSets {
		rs := &replicaSets[i]
		if !hasOwner(rs.OwnerReferences, deploymentUID) {
			continue
		}
		if rs.Annotations["deployment.kubernetes.io/revision"] == want {
			return rs
		}
	}
	return nil
}

func hasOwner(refs []metav1.OwnerReference, uid types.UID) bool {
	for _, ref := range refs {
		if ref.UID == uid {
			return true
		}
	}
	return false
}
