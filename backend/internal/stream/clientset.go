package stream

import (
	"fmt"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

	"github.com/f1nn17/vinaire/backend/internal/authn"
	"github.com/f1nn17/vinaire/backend/internal/clusters"
)

// clientsetFor resolves clusterName (Roadmap Phase 11; "" = the registry's
// default cluster) and returns the clientset sess should use against it —
// the entry's own backend-credentialed Clientset in authn.ModeDisabled, or
// one built from sess's own K8s token otherwise (Roadmap Phase 10), mirroring
// internal/api.Handler.resolveCluster for the WebSocket handlers, which
// can't reuse that method directly since they don't go through a plain
// http.Handler chain.
func clientsetFor(registry *clusters.Registry, auth *authn.Manager, sess *authn.Session, clusterName string) (*kubernetes.Clientset, error) {
	entry, ok := registry.Resolve(clusterName)
	if !ok {
		return nil, fmt.Errorf("unknown cluster %q", clusterName)
	}
	if entry == nil || entry.Clientset == nil {
		return nil, fmt.Errorf("kubernetes client unavailable")
	}
	if auth.Mode == authn.ModeDisabled {
		return entry.Clientset, nil
	}
	return entry.ClientsetForToken(sess.K8sToken)
}

// restConfigFor is clientsetFor's counterpart for callers that need the
// *rest.Config itself (internal/stream's exec bridge, for
// remotecommand.NewSPDYExecutor) rather than a Clientset built from it.
func restConfigFor(registry *clusters.Registry, auth *authn.Manager, sess *authn.Session, clusterName string) (*clusters.Entry, *rest.Config, error) {
	entry, ok := registry.Resolve(clusterName)
	if !ok {
		return nil, nil, fmt.Errorf("unknown cluster %q", clusterName)
	}
	if entry == nil || entry.Clientset == nil {
		return nil, nil, fmt.Errorf("kubernetes client unavailable")
	}
	if auth.Mode == authn.ModeDisabled {
		return entry, entry.RESTConfig, nil
	}
	return entry, entry.RESTConfigForToken(sess.K8sToken), nil
}
