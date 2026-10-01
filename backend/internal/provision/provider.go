package provision

import (
	"context"
	"fmt"
)

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

// ControlPlaneOptions configures a new cluster's first server.
type ControlPlaneOptions struct {
	// TLSSAN is the address the API server's certificate must cover — the one
	// Vinaire (and everyone else) will reach it by. Without it, connecting by
	// the machine's public address fails TLS verification.
	TLSSAN  string
	Version string // optional pin
}

// ClusterProvisioner turns bare servers into cluster members. Roadmap Phase 30
// implements Preflight, PrepareNode and JoinWorker for k3s (adding workers);
// Phase 34 implements PreflightControlPlane, InstallControlPlane and
// FetchKubeconfig (creating a cluster, importing an existing one) and Phase 35
// adds a kubeadm implementation behind the same interface.
type ClusterProvisioner interface {
	Name() string

	// Preflight only reads: is this machine usable, and can it reach the
	// cluster's API server?
	Preflight(ctx context.Context, node Runner, serverURL string, log LogFunc) (NodeFacts, error)
	// PrepareNode makes the machine ready for the install, if it needs anything.
	PrepareNode(ctx context.Context, node Runner, log LogFunc) error
	// PreflightControlPlane checks a machine is fit to become a new cluster's
	// first server, and refuses — without changing anything — if a cluster is
	// already installed there or the API port is taken (Phase 34).
	PreflightControlPlane(ctx context.Context, node Runner, log LogFunc) (NodeFacts, error)
	// InstallControlPlane creates a new single-server cluster on node and
	// waits for its API to be ready (Phase 34).
	InstallControlPlane(ctx context.Context, node Runner, opts ControlPlaneOptions, log LogFunc) error
	// JoinWorker installs the agent and joins the cluster.
	JoinWorker(ctx context.Context, node Runner, join JoinInfo, log LogFunc) error
	// FetchKubeconfig reads the admin kubeconfig off a control plane (Phase
	// 34). It is returned as a Secret — it holds cluster-admin credentials —
	// and never logged.
	FetchKubeconfig(ctx context.Context, controlPlane Runner) (Secret, error)

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
