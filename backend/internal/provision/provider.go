package provision

import (
	"context"
	"errors"
	"fmt"
)

// ErrNotImplemented marks a ClusterProvisioner step a provider doesn't do (yet).
var ErrNotImplemented = errors.New("not implemented by this provider")

// LogFunc receives human-readable progress lines. Everything passed to it is
// already redacted by the caller's Redactor before it is stored or streamed.
type LogFunc func(line string)

// NodeFacts is what Preflight learns about the target machine.
type NodeFacts struct {
	Hostname string
	Arch     string
	OS       string
}

// JoinInfo is what a worker needs to join an existing cluster.
type JoinInfo struct {
	ServerURL string // https://host:6443 — as reachable *from the new node*
	Token     Secret
	Version   string // optional: pin the install to the control plane's version
	NodeName  string // optional: defaults to the machine's hostname
}

// ClusterProvisioner turns bare servers into cluster members. Roadmap Phase 30
// implements Preflight, PrepareNode and JoinWorker for k3s (adding workers);
// Phase 34 adds InstallControlPlane/FetchKubeconfig (creating a cluster) and
// Phase 35 adds a kubeadm implementation behind the same interface.
type ClusterProvisioner interface {
	Name() string

	// Preflight only reads: is this machine usable, and can it reach the
	// cluster's API server?
	Preflight(ctx context.Context, node Runner, serverURL string, log LogFunc) (NodeFacts, error)
	// PrepareNode makes the machine ready for the install, if it needs anything.
	PrepareNode(ctx context.Context, node Runner, log LogFunc) error
	// InstallControlPlane creates a new cluster on node (Phase 34).
	InstallControlPlane(ctx context.Context, node Runner, log LogFunc) error
	// JoinWorker installs the agent and joins the cluster.
	JoinWorker(ctx context.Context, node Runner, join JoinInfo, log LogFunc) error
	// FetchKubeconfig reads an admin kubeconfig off a control plane (Phase 34).
	FetchKubeconfig(ctx context.Context, controlPlane Runner) ([]byte, error)

	// JoinTokenFromControlPlane reads the worker-join token off a control-plane
	// machine. The token is returned as a Secret and never logged.
	JoinTokenFromControlPlane(ctx context.Context, controlPlane Runner) (Secret, error)
}

// ProviderByName returns the provisioner registered under name.
func ProviderByName(name string) (ClusterProvisioner, error) {
	switch name {
	case "", "k3s":
		return K3s{}, nil
	}
	return nil, fmt.Errorf("unknown provider %q (supported: k3s)", name)
}
