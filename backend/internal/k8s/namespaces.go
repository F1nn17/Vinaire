package k8s

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
)

// ListNamespaces returns every Namespace in the cluster.
func ListNamespaces(ctx context.Context, clientset *kubernetes.Clientset) ([]corev1.Namespace, error) {
	list, err := clientset.CoreV1().Namespaces().List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("list namespaces: %w", err)
	}
	return list.Items, nil
}

// GetNamespace fetches a single Namespace by name.
func GetNamespace(ctx context.Context, clientset *kubernetes.Clientset, name string) (*corev1.Namespace, error) {
	ns, err := clientset.CoreV1().Namespaces().Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("get namespace %s: %w", name, err)
	}
	return ns, nil
}

// ListResourceQuotas returns the ResourceQuotas in namespace ("" = all).
func ListResourceQuotas(ctx context.Context, clientset *kubernetes.Clientset, namespace string) ([]corev1.ResourceQuota, error) {
	list, err := clientset.CoreV1().ResourceQuotas(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("list resourcequotas: %w", err)
	}
	return list.Items, nil
}

// ListLimitRanges returns the LimitRanges in namespace.
func ListLimitRanges(ctx context.Context, clientset *kubernetes.Clientset, namespace string) ([]corev1.LimitRange, error) {
	list, err := clientset.CoreV1().LimitRanges(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("list limitranges: %w", err)
	}
	return list.Items, nil
}

// CountNamespaceResources counts what lives in a namespace, per kind. A kind
// the caller can't list (RBAC) or that fails comes back nil rather than 0 —
// "unknown" must not read as "empty".
func CountNamespaceResources(ctx context.Context, clientset *kubernetes.Clientset, namespace string) map[string]*int {
	opts := metav1.ListOptions{}
	counters := []struct {
		kind  string
		count func() (int, error)
	}{
		{"pods", func() (int, error) {
			l, err := clientset.CoreV1().Pods(namespace).List(ctx, opts)
			return itemCount(l, err)
		}},
		{"deployments", func() (int, error) {
			l, err := clientset.AppsV1().Deployments(namespace).List(ctx, opts)
			return itemCount(l, err)
		}},
		{"statefulsets", func() (int, error) {
			l, err := clientset.AppsV1().StatefulSets(namespace).List(ctx, opts)
			return itemCount(l, err)
		}},
		{"daemonsets", func() (int, error) {
			l, err := clientset.AppsV1().DaemonSets(namespace).List(ctx, opts)
			return itemCount(l, err)
		}},
		{"jobs", func() (int, error) {
			l, err := clientset.BatchV1().Jobs(namespace).List(ctx, opts)
			return itemCount(l, err)
		}},
		{"services", func() (int, error) {
			l, err := clientset.CoreV1().Services(namespace).List(ctx, opts)
			return itemCount(l, err)
		}},
		{"ingresses", func() (int, error) {
			l, err := clientset.NetworkingV1().Ingresses(namespace).List(ctx, opts)
			return itemCount(l, err)
		}},
		{"configmaps", func() (int, error) {
			l, err := clientset.CoreV1().ConfigMaps(namespace).List(ctx, opts)
			return itemCount(l, err)
		}},
		{"secrets", func() (int, error) {
			l, err := clientset.CoreV1().Secrets(namespace).List(ctx, opts)
			return itemCount(l, err)
		}},
		{"persistentvolumeclaims", func() (int, error) {
			l, err := clientset.CoreV1().PersistentVolumeClaims(namespace).List(ctx, opts)
			return itemCount(l, err)
		}},
	}

	out := make(map[string]*int, len(counters))
	for _, c := range counters {
		n, err := c.count()
		if err != nil {
			out[c.kind] = nil
			continue
		}
		out[c.kind] = &n
	}
	return out
}

func itemCount(list runtime.Object, err error) (int, error) {
	if err != nil {
		return 0, err
	}
	return meta.LenList(list), nil
}

// DeleteNamespace is Roadmap Phase 27's one new write action: an irreversible,
// cascading delete (everything inside the namespace goes with it). See
// AGENTS.md before adding another write verb here.
func DeleteNamespace(ctx context.Context, clientset *kubernetes.Clientset, name string) error {
	if err := clientset.CoreV1().Namespaces().Delete(ctx, name, metav1.DeleteOptions{}); err != nil {
		return fmt.Errorf("delete namespace %s: %w", name, err)
	}
	return nil
}
