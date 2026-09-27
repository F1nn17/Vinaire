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
//
// The *rest.Config is also returned so callers can build other typed
// clients against the same cluster (e.g. the metrics-server client in
// internal/api) without re-resolving kubeconfig/in-cluster config again.
func NewClientset(kubeconfigPath string) (*kubernetes.Clientset, *rest.Config, error) {
	config, err := rest.InClusterConfig()
	if err != nil {
		if kubeconfigPath == "" {
			home, herr := os.UserHomeDir()
			if herr != nil {
				return nil, nil, fmt.Errorf("resolve home dir: %w", herr)
			}
			kubeconfigPath = filepath.Join(home, ".kube", "config")
		}

		config, err = clientcmd.BuildConfigFromFlags("", kubeconfigPath)
		if err != nil {
			return nil, nil, fmt.Errorf("load kubeconfig from %s: %w", kubeconfigPath, err)
		}
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, nil, fmt.Errorf("create clientset: %w", err)
	}

	return clientset, config, nil
}

// CurrentContextName returns the kubeconfig's current-context name, or ""
// if there's no kubeconfig to read (e.g. running in-cluster, where
// NewClientset never even looks at a kubeconfig file) or it can't be
// loaded. Kubernetes clusters have no self-reported name of their own —
// this is the best available real (not hardcoded) label for the
// single-cluster/no-CLUSTER_CONTEXTS case, see main.go's
// buildClusterRegistry.
func CurrentContextName(kubeconfigPath string) string {
	loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
	if kubeconfigPath != "" {
		loadingRules.ExplicitPath = kubeconfigPath
	}
	rawConfig, err := loadingRules.Load()
	if err != nil {
		return ""
	}
	return rawConfig.CurrentContext
}

// NewClientsetForContext builds a clientset for one named kubeconfig
// context, ignoring whatever the kubeconfig's own current-context is set
// to. Used for Roadmap Phase 11 (multi-cluster): one call per configured
// context name, each producing an independent internal/clusters.Entry.
// kubeconfigPath empty means the default loading rules (KUBECONFIG env var,
// then ~/.kube/config).
func NewClientsetForContext(kubeconfigPath, contextName string) (*kubernetes.Clientset, *rest.Config, error) {
	loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
	if kubeconfigPath != "" {
		loadingRules.ExplicitPath = kubeconfigPath
	}
	overrides := &clientcmd.ConfigOverrides{CurrentContext: contextName}

	config, err := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(loadingRules, overrides).ClientConfig()
	if err != nil {
		return nil, nil, fmt.Errorf("load kubeconfig context %s: %w", contextName, err)
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, nil, fmt.Errorf("create clientset for context %s: %w", contextName, err)
	}

	return clientset, config, nil
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
