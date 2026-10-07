package k8s

import (
	"context"
	"fmt"

	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// RBAC bindings (Roadmap Phase 44): create and delete RoleBindings and
// ClusterRoleBindings — and only those. Vinaire never creates or edits a Role or
// ClusterRole, so it can hand out permissions that exist but not invent new ones.
// Every call is made with the caller's own clientset, so Kubernetes' escalation
// check applies: you can't bind a role holding rights you lack (unless the cluster
// granted you `bind` on it). See AGENTS.md's write list.

func CreateRoleBinding(ctx context.Context, cs kubernetes.Interface, rb *rbacv1.RoleBinding) error {
	if _, err := cs.RbacV1().RoleBindings(rb.Namespace).Create(ctx, rb, metav1.CreateOptions{}); err != nil {
		return fmt.Errorf("create rolebinding %s/%s: %w", rb.Namespace, rb.Name, err)
	}
	return nil
}

func CreateClusterRoleBinding(ctx context.Context, cs kubernetes.Interface, crb *rbacv1.ClusterRoleBinding) error {
	if _, err := cs.RbacV1().ClusterRoleBindings().Create(ctx, crb, metav1.CreateOptions{}); err != nil {
		return fmt.Errorf("create clusterrolebinding %s: %w", crb.Name, err)
	}
	return nil
}

func DeleteRoleBinding(ctx context.Context, cs kubernetes.Interface, namespace, name string) error {
	if err := cs.RbacV1().RoleBindings(namespace).Delete(ctx, name, metav1.DeleteOptions{}); err != nil {
		return fmt.Errorf("delete rolebinding %s/%s: %w", namespace, name, err)
	}
	return nil
}

func DeleteClusterRoleBinding(ctx context.Context, cs kubernetes.Interface, name string) error {
	if err := cs.RbacV1().ClusterRoleBindings().Delete(ctx, name, metav1.DeleteOptions{}); err != nil {
		return fmt.Errorf("delete clusterrolebinding %s: %w", name, err)
	}
	return nil
}
