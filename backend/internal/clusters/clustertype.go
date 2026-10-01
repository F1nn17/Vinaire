package clusters

import (
	"context"
	"strings"
	"time"

	authorizationv1 "k8s.io/api/authorization/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// Cluster types (Roadmap Phase 34). Detection is a heuristic — it is shown, can
// be corrected by the user, and nothing hard-depends on it except hiding the
// "Add node" button where adding a machine over SSH makes no sense.
const (
	TypeK3s      = "k3s"
	TypeK3d      = "k3d"
	TypeKind     = "kind"
	TypeMinikube = "minikube"
	TypeMicroK8s = "microk8s"
	TypeKubeadm  = "kubeadm"
	TypeManaged  = "managed" // EKS / GKE / AKS and friends
	TypeUnknown  = "unknown"
)

var validTypes = map[string]bool{
	TypeK3s: true, TypeK3d: true, TypeKind: true, TypeMinikube: true,
	TypeMicroK8s: true, TypeKubeadm: true, TypeManaged: true, TypeUnknown: true,
}

// ValidType reports whether t is one of the known cluster types.
func ValidType(t string) bool { return validTypes[t] }

// SupportsAddNode is whether adding a machine over SSH (Phase 30) is a
// sensible thing for this kind of cluster: real machines joining a k3s or
// kubeadm cluster. k3d/kind "nodes" are containers, minikube is a single VM,
// and managed clusters scale through their provider.
func SupportsAddNode(t string) bool { return t == TypeK3s || t == TypeKubeadm || t == TypeUnknown }

// DetectType works out a cluster's type from cheap, read-only signals: the
// context name the credentials came with (`hint`, may be empty), the server's
// version string, its nodes and, last, whether kubeadm left its ConfigMap.
// Anything it can't read just narrows the evidence; the worst case is
// TypeUnknown.
func DetectType(ctx context.Context, clientset *kubernetes.Clientset, hint string) string {
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()

	hint = strings.ToLower(hint)
	version := ""
	if v, err := clientset.Discovery().ServerVersion(); err == nil {
		version = strings.ToLower(v.GitVersion)
	}

	var nodes []corev1.Node
	if list, err := clientset.CoreV1().Nodes().List(ctx, metav1.ListOptions{Limit: 20}); err == nil {
		nodes = list.Items
	}

	for _, n := range nodes {
		id := strings.ToLower(n.Spec.ProviderID)
		switch {
		case strings.HasPrefix(id, "kind://"):
			return TypeKind
		case strings.HasPrefix(id, "aws://"), strings.HasPrefix(id, "gce://"), strings.HasPrefix(id, "azure://"):
			return TypeManaged
		}
		if _, ok := n.Labels["minikube.k8s.io/name"]; ok {
			return TypeMinikube
		}
		if _, ok := n.Labels["microk8s.io/cluster"]; ok {
			return TypeMicroK8s
		}
	}

	switch {
	case strings.HasPrefix(hint, "kind-"):
		return TypeKind
	case hint == "minikube":
		return TypeMinikube
	case strings.HasPrefix(hint, "microk8s"):
		return TypeMicroK8s
	}

	if strings.Contains(version, "+k3s") {
		// k3d runs k3s inside containers it names k3d-<cluster>-server-N.
		if strings.HasPrefix(hint, "k3d-") {
			return TypeK3d
		}
		for _, n := range nodes {
			if strings.HasPrefix(n.Name, "k3d-") {
				return TypeK3d
			}
		}
		return TypeK3s
	}
	if strings.Contains(version, "-gke") || strings.Contains(version, "-eks") || strings.Contains(version, "-aks") {
		return TypeManaged
	}

	if _, err := clientset.CoreV1().ConfigMaps("kube-system").Get(ctx, "kubeadm-config", metav1.GetOptions{}); err == nil {
		return TypeKubeadm
	}
	return TypeUnknown
}

// IsClusterAdmin asks the API whether the credentials in clientset may do
// everything (SelfSubjectAccessReview for */*/*). The answer is advisory —
// shown so the user knows how powerful what they're storing is.
func IsClusterAdmin(ctx context.Context, clientset *kubernetes.Clientset) (bool, bool) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	r, err := clientset.AuthorizationV1().SelfSubjectAccessReviews().Create(ctx, &authorizationv1.SelfSubjectAccessReview{
		Spec: authorizationv1.SelfSubjectAccessReviewSpec{
			ResourceAttributes: &authorizationv1.ResourceAttributes{Verb: "*", Resource: "*", Group: "*"},
		},
	}, metav1.CreateOptions{})
	if err != nil {
		return false, false
	}
	return r.Status.Allowed, true
}

// Identity returns a stable identifier for the cluster itself — the UID of its
// kube-system namespace — so the same cluster reached by different addresses
// (127.0.0.1, a LAN IP, a hostname) is recognized as one.
func Identity(ctx context.Context, clientset *kubernetes.Clientset) (string, bool) {
	ctx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	ns, err := clientset.CoreV1().Namespaces().Get(ctx, "kube-system", metav1.GetOptions{})
	if err != nil {
		return "", false
	}
	return string(ns.UID), true
}
