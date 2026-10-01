package provision

import (
	"context"
	"fmt"
	"sync"
)

// LogFunc receives human-readable progress lines. Everything passed to it is
// already redacted by the caller's Redactor before it is stored or streamed.
type LogFunc func(line string)

// NodeFacts is what Preflight learns about the target machine.
type NodeFacts struct {
	Hostname    string
	Arch        string
	OS          string
	CPUs        int
	MemKB       int64
	ProductUUID string // empty where the machine doesn't expose one
	Epoch       int64  // the machine's clock, unix seconds
}

// JoinInfo is what a worker needs to join an existing cluster.
type JoinInfo struct {
	// ServerURL is the cluster API as reachable *from the new node*
	// (https://host:port) — derived from the join command for kubeadm.
	ServerURL string
	// Token is the provider's join credential: a k3s node token, or the whole
	// `kubeadm join ...` command (it carries the bootstrap token and the CA
	// hash).
	Token    Secret
	Version  string // optional pin to the cluster's version
	NodeName string // optional: defaults to the machine's hostname
}

// PrepareOptions says what to prepare a machine for.
type PrepareOptions struct {
	// K8sMinor is the Kubernetes minor ("1.37") a kubeadm node is prepared for;
	// ignored by k3s, whose installer brings its own.
	K8sMinor string
}

// KubeadmOptions configures a new kubeadm cluster's first server.
type KubeadmOptions struct {
	Minor string // "1.37"
	CNI   string // a name from the backend's kubeadm config
	// ControlPlaneEndpoint is a VIP or DNS name for the API. Setting it at init
	// time is what keeps a later move to HA possible.
	ControlPlaneEndpoint string
	// SingleNode removes the control-plane taint so workloads schedule there.
	SingleNode bool
	// IgnorePreflightErrors names kubeadm preflight checks to skip — an escape
	// hatch for unusual environments, empty by default.
	IgnorePreflightErrors []string
}

// ControlPlaneOptions configures a new cluster's first server.
type ControlPlaneOptions struct {
	// TLSSAN is the address the API server's certificate must cover — the one
	// Vinaire (and everyone else) will reach it by. Without it, connecting by
	// the machine's public address fails TLS verification.
	TLSSAN  string
	Version string // optional pin (k3s)
	Kubeadm KubeadmOptions
}

// ClusterProvisioner turns bare servers into cluster members. Roadmap Phase 30
// added workers, Phase 34 whole clusters (k3s), and Phase 35 a second
// implementation — kubeadm — behind the same interface.
type ClusterProvisioner interface {
	Name() string

	// ValidateJoinToken checks the provider's join credential has the expected
	// shape before anything is run with it.
	ValidateJoinToken(token Secret) error
	// JoinEndpoint returns the cluster API address (https://host:port) a worker
	// should reach, from the explicit server URL and/or the join token, or an
	// error if neither yields one.
	JoinEndpoint(serverURL string, token Secret) (string, error)

	// Preflight only reads: is this machine usable as a worker, and can it
	// reach the cluster's API server?
	Preflight(ctx context.Context, node Runner, join JoinInfo, log LogFunc) (NodeFacts, error)
	// PrepareNode makes the machine ready for the install, if it needs anything.
	PrepareNode(ctx context.Context, node Runner, opts PrepareOptions, log LogFunc) error
	// PreflightControlPlane checks a machine is fit to become a new cluster's
	// first server, and refuses — without changing anything — if a cluster is
	// already installed there or the API port is taken.
	PreflightControlPlane(ctx context.Context, node Runner, log LogFunc) (NodeFacts, error)
	// InstallControlPlane creates a new single-server cluster on node and
	// waits for it to be ready.
	InstallControlPlane(ctx context.Context, node Runner, opts ControlPlaneOptions, log LogFunc) error
	// JoinWorker installs the agent and joins the cluster.
	JoinWorker(ctx context.Context, node Runner, join JoinInfo, log LogFunc) error
	// FetchKubeconfig reads the admin kubeconfig off a control plane. It is
	// returned as a Secret — it holds cluster-admin credentials — and never
	// logged.
	FetchKubeconfig(ctx context.Context, controlPlane Runner) (Secret, error)
	// JoinTokenFromControlPlane reads (or mints) the worker-join credential on a
	// control-plane machine. Returned as a Secret and never logged.
	JoinTokenFromControlPlane(ctx context.Context, controlPlane Runner) (Secret, error)
	// Reset undoes a Kubernetes install on node — for a machine whose install
	// failed partway, or one being taken out of its cluster. Destructive.
	Reset(ctx context.Context, node Runner, log LogFunc) error
}

var (
	configMu      sync.RWMutex
	kubeadmConfig = DefaultKubeadmConfig()
)

// Configure sets the kubeadm configuration (supported versions, CNI manifests
// and their pinned versions) used from now on. Called once at startup.
func Configure(cfg KubeadmConfig) {
	configMu.Lock()
	defer configMu.Unlock()
	kubeadmConfig = cfg
}

// CurrentKubeadmConfig returns the configuration in effect.
func CurrentKubeadmConfig() KubeadmConfig {
	configMu.RLock()
	defer configMu.RUnlock()
	return kubeadmConfig
}

// ProviderByName returns the provisioner registered under name.
func ProviderByName(name string) (ClusterProvisioner, error) {
	switch name {
	case "", "k3s":
		return K3s{}, nil
	case "kubeadm":
		return Kubeadm{Config: CurrentKubeadmConfig()}, nil
	}
	return nil, fmt.Errorf("unknown provider %q (supported: k3s, kubeadm)", name)
}
