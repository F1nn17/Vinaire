// Package k8s wires up a client-go clientset for talking to the target
// Kubernetes cluster (k3s). All handlers in this package must be read-only:
// see AGENTS.md for the read-only / GitOps architecture rule.
package k8s

import (
	"fmt"
	"os"
	"path/filepath"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

// NewClientset builds a Kubernetes clientset. It first tries in-cluster
// config (for when the backend itself runs inside the cluster), then falls
// back to kubeconfigPath, or ~/.kube/config if kubeconfigPath is empty.
// The caller (main, via internal/config) owns reading KUBECONFIG from the
// environment — this function just takes the resolved path.
func NewClientset(kubeconfigPath string) (*kubernetes.Clientset, error) {
	config, err := rest.InClusterConfig()
	if err != nil {
		if kubeconfigPath == "" {
			home, herr := os.UserHomeDir()
			if herr != nil {
				return nil, fmt.Errorf("resolve home dir: %w", herr)
			}
			kubeconfigPath = filepath.Join(home, ".kube", "config")
		}

		config, err = clientcmd.BuildConfigFromFlags("", kubeconfigPath)
		if err != nil {
			return nil, fmt.Errorf("load kubeconfig from %s: %w", kubeconfigPath, err)
		}
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("create clientset: %w", err)
	}

	return clientset, nil
}

// Pods/Deployments/StatefulSets/DaemonSets/Jobs listers live in
// workloads.go (wired up as HTTP handlers in internal/api, Phase 1).
//
// TODO(vinaire): read-only listers still to add here (later roadmap phases):
//   - ListConfigMaps(namespace string)
//   - ListSecrets(namespace string)          // metadata only, never decode values
//   - ListIngresses(namespace string)
//
// Reminder: this package must stay read-only. Any "write" action (scale,
// edit, delete, apply) belongs in a GitOps PR flow, not a direct API call
// from here. See AGENTS.md.
