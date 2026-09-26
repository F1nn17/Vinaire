package k8s

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// ListRoles returns Roles in namespace, or across all namespaces if
// namespace is empty.
func ListRoles(ctx context.Context, clientset *kubernetes.Clientset, namespace string) ([]rbacv1.Role, error) {
	list, err := clientset.RbacV1().Roles(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("list roles: %w", err)
	}
	return list.Items, nil
}

// ListClusterRoles returns every ClusterRole — cluster-scoped.
func ListClusterRoles(ctx context.Context, clientset *kubernetes.Clientset) ([]rbacv1.ClusterRole, error) {
	list, err := clientset.RbacV1().ClusterRoles().List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("list clusterroles: %w", err)
	}
	return list.Items, nil
}

// ListRoleBindings returns RoleBindings in namespace, or across all
// namespaces if namespace is empty.
func ListRoleBindings(ctx context.Context, clientset *kubernetes.Clientset, namespace string) ([]rbacv1.RoleBinding, error) {
	list, err := clientset.RbacV1().RoleBindings(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("list rolebindings: %w", err)
	}
	return list.Items, nil
}

// ListClusterRoleBindings returns every ClusterRoleBinding —
// cluster-scoped.
func ListClusterRoleBindings(ctx context.Context, clientset *kubernetes.Clientset) ([]rbacv1.ClusterRoleBinding, error) {
	list, err := clientset.RbacV1().ClusterRoleBindings().List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("list clusterrolebindings: %w", err)
	}
	return list.Items, nil
}

// ListServiceAccounts returns ServiceAccounts in namespace, or across all
// namespaces if namespace is empty.
func ListServiceAccounts(ctx context.Context, clientset *kubernetes.Clientset, namespace string) ([]corev1.ServiceAccount, error) {
	list, err := clientset.CoreV1().ServiceAccounts(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("list serviceaccounts: %w", err)
	}
	return list.Items, nil
}
