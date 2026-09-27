package k8s

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/client-go/kubernetes"
)

// ListNodes returns every Node in the cluster (Nodes are cluster-scoped, no
// namespace filter).
func ListNodes(ctx context.Context, clientset *kubernetes.Clientset) ([]corev1.Node, error) {
	list, err := clientset.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("list nodes: %w", err)
	}
	return list.Items, nil
}

// GetNode fetches a single Node by name.
func GetNode(ctx context.Context, clientset *kubernetes.Clientset, name string) (*corev1.Node, error) {
	node, err := clientset.CoreV1().Nodes().Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("get node %s: %w", name, err)
	}
	return node, nil
}

// ListPodsOnNode returns every Pod (across all namespaces) actually
// scheduled onto nodeName, via a spec.nodeName field selector — the same
// thing `kubectl get pods --all-namespaces --field-selector spec.nodeName=`
// does.
func ListPodsOnNode(ctx context.Context, clientset *kubernetes.Clientset, nodeName string) ([]corev1.Pod, error) {
	list, err := clientset.CoreV1().Pods("").List(ctx, metav1.ListOptions{
		FieldSelector: fields.OneTermEqualSelector("spec.nodeName", nodeName).String(),
	})
	if err != nil {
		return nil, fmt.Errorf("list pods on node %s: %w", nodeName, err)
	}
	return list.Items, nil
}
