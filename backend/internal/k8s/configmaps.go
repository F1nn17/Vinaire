package k8s

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// ListConfigMaps returns configmaps in namespace, or across all namespaces
// if namespace is empty.
func ListConfigMaps(ctx context.Context, clientset *kubernetes.Clientset, namespace string) ([]corev1.ConfigMap, error) {
	list, err := clientset.CoreV1().ConfigMaps(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("list configmaps: %w", err)
	}
	return list.Items, nil
}
