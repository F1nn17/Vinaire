package k8s

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// ListSecrets returns secret metadata (including key names — the caller is
// responsible for never surfacing .Data values from this list) in
// namespace, or across all namespaces if namespace is empty.
func ListSecrets(ctx context.Context, clientset *kubernetes.Clientset, namespace string) ([]corev1.Secret, error) {
	list, err := clientset.CoreV1().Secrets(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("list secrets: %w", err)
	}
	return list.Items, nil
}

// GetSecret fetches one secret by namespace and name, including its decoded
// .Data values. Callers must only use this for the explicit, audited
// reveal flow (internal/api.RevealSecretKey) — never for the list/summary
// endpoints. See AGENTS.md's read-only/secrets rule.
func GetSecret(ctx context.Context, clientset *kubernetes.Clientset, namespace, name string) (*corev1.Secret, error) {
	secret, err := clientset.CoreV1().Secrets(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("get secret %s/%s: %w", namespace, name, err)
	}
	return secret, nil
}

// DeleteSecret removes one Secret. internal/api adds the guard rails (the name typed, no system
// namespace, never a Helm release record); this function just deletes.
func DeleteSecret(ctx context.Context, clientset *kubernetes.Clientset, namespace, name string) error {
	if err := clientset.CoreV1().Secrets(namespace).Delete(ctx, name, metav1.DeleteOptions{}); err != nil {
		return fmt.Errorf("delete secret %s/%s: %w", namespace, name, err)
	}
	return nil
}

// GetSecretType returns a Secret's type without handing its data to the caller of this function
// any further than it needs: only the type is kept.
func GetSecretType(ctx context.Context, clientset *kubernetes.Clientset, namespace, name string) (corev1.SecretType, error) {
	s, err := clientset.CoreV1().Secrets(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return "", fmt.Errorf("get secret %s/%s: %w", namespace, name, err)
	}
	return s.Type, nil
}
