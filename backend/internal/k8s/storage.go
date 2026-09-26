package k8s

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	storagev1 "k8s.io/api/storage/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// ListPersistentVolumeClaims returns PVCs in namespace, or across all
// namespaces if namespace is empty.
func ListPersistentVolumeClaims(ctx context.Context, clientset *kubernetes.Clientset, namespace string) ([]corev1.PersistentVolumeClaim, error) {
	list, err := clientset.CoreV1().PersistentVolumeClaims(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("list persistentvolumeclaims: %w", err)
	}
	return list.Items, nil
}

// GetPersistentVolumeClaim fetches a single PVC by namespace and name.
func GetPersistentVolumeClaim(ctx context.Context, clientset *kubernetes.Clientset, namespace, name string) (*corev1.PersistentVolumeClaim, error) {
	pvc, err := clientset.CoreV1().PersistentVolumeClaims(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("get persistentvolumeclaim %s/%s: %w", namespace, name, err)
	}
	return pvc, nil
}

// ListPersistentVolumes returns every PersistentVolume — cluster-scoped,
// unlike PersistentVolumeClaims, so this takes no namespace argument.
func ListPersistentVolumes(ctx context.Context, clientset *kubernetes.Clientset) ([]corev1.PersistentVolume, error) {
	list, err := clientset.CoreV1().PersistentVolumes().List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("list persistentvolumes: %w", err)
	}
	return list.Items, nil
}

// ListStorageClasses returns every StorageClass — cluster-scoped.
func ListStorageClasses(ctx context.Context, clientset *kubernetes.Clientset) ([]storagev1.StorageClass, error) {
	list, err := clientset.StorageV1().StorageClasses().List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("list storageclasses: %w", err)
	}
	return list.Items, nil
}
