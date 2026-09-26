package k8s

import (
	"context"
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// ListPods returns pods in namespace, or across all namespaces if namespace
// is empty.
func ListPods(ctx context.Context, clientset *kubernetes.Clientset, namespace string) ([]corev1.Pod, error) {
	list, err := clientset.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("list pods: %w", err)
	}
	return list.Items, nil
}

// ListDeployments returns deployments in namespace, or across all
// namespaces if namespace is empty.
func ListDeployments(ctx context.Context, clientset *kubernetes.Clientset, namespace string) ([]appsv1.Deployment, error) {
	list, err := clientset.AppsV1().Deployments(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("list deployments: %w", err)
	}
	return list.Items, nil
}

// ListStatefulSets returns statefulsets in namespace, or across all
// namespaces if namespace is empty.
func ListStatefulSets(ctx context.Context, clientset *kubernetes.Clientset, namespace string) ([]appsv1.StatefulSet, error) {
	list, err := clientset.AppsV1().StatefulSets(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("list statefulsets: %w", err)
	}
	return list.Items, nil
}

// ListDaemonSets returns daemonsets in namespace, or across all namespaces
// if namespace is empty.
func ListDaemonSets(ctx context.Context, clientset *kubernetes.Clientset, namespace string) ([]appsv1.DaemonSet, error) {
	list, err := clientset.AppsV1().DaemonSets(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("list daemonsets: %w", err)
	}
	return list.Items, nil
}

// ListJobs returns jobs in namespace, or across all namespaces if namespace
// is empty.
func ListJobs(ctx context.Context, clientset *kubernetes.Clientset, namespace string) ([]batchv1.Job, error) {
	list, err := clientset.BatchV1().Jobs(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("list jobs: %w", err)
	}
	return list.Items, nil
}

// GetStatefulSet fetches a single statefulset by namespace and name.
func GetStatefulSet(ctx context.Context, clientset *kubernetes.Clientset, namespace, name string) (*appsv1.StatefulSet, error) {
	sts, err := clientset.AppsV1().StatefulSets(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("get statefulset %s/%s: %w", namespace, name, err)
	}
	return sts, nil
}

// GetDaemonSet fetches a single daemonset by namespace and name.
func GetDaemonSet(ctx context.Context, clientset *kubernetes.Clientset, namespace, name string) (*appsv1.DaemonSet, error) {
	ds, err := clientset.AppsV1().DaemonSets(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("get daemonset %s/%s: %w", namespace, name, err)
	}
	return ds, nil
}

// GetJob fetches a single job by namespace and name.
func GetJob(ctx context.Context, clientset *kubernetes.Clientset, namespace, name string) (*batchv1.Job, error) {
	job, err := clientset.BatchV1().Jobs(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("get job %s/%s: %w", namespace, name, err)
	}
	return job, nil
}
