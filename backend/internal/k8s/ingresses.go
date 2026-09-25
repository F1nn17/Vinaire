package k8s

import (
	"context"
	"fmt"

	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// ListIngresses returns ingresses in namespace, or across all namespaces if
// namespace is empty.
func ListIngresses(ctx context.Context, clientset *kubernetes.Clientset, namespace string) ([]networkingv1.Ingress, error) {
	list, err := clientset.NetworkingV1().Ingresses(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("list ingresses: %w", err)
	}
	return list.Items, nil
}
