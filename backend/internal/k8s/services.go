package k8s

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// ListServices returns services in namespace, or across all namespaces if
// namespace is empty.
func ListServices(ctx context.Context, clientset *kubernetes.Clientset, namespace string) ([]corev1.Service, error) {
	list, err := clientset.CoreV1().Services(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("list services: %w", err)
	}
	return list.Items, nil
}
