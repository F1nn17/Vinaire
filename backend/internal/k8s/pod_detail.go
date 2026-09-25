package k8s

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/client-go/kubernetes"
)

// GetPod fetches a single pod by namespace and name.
func GetPod(ctx context.Context, clientset *kubernetes.Clientset, namespace, name string) (*corev1.Pod, error) {
	pod, err := clientset.CoreV1().Pods(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("get pod %s/%s: %w", namespace, name, err)
	}
	return pod, nil
}

// ListEventsForObject returns events recorded against a specific object
// (e.g. a pod), namespace and name, such as "Started container" or
// "Liveness probe failed".
func ListEventsForObject(ctx context.Context, clientset *kubernetes.Clientset, namespace, name string) ([]corev1.Event, error) {
	selector := fields.Set{
		"involvedObject.name":      name,
		"involvedObject.namespace": namespace,
	}.AsSelector().String()

	list, err := clientset.CoreV1().Events(namespace).List(ctx, metav1.ListOptions{
		FieldSelector: selector,
	})
	if err != nil {
		return nil, fmt.Errorf("list events for %s/%s: %w", namespace, name, err)
	}
	return list.Items, nil
}
