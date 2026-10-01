package provision

import (
	"context"
	"errors"
	"fmt"
	"net"
)

// Roadmap Phase 34: adding a whole cluster over SSH, as opposed to a node
// (Phase 30). Two modes share the same SSH layer, host-key pinning, job log and
// secret handling:
//
//   - import: log into a server that already runs k3s, read its admin
//     kubeconfig, and register it.
//   - create: log into a clean server, install a single-server k3s with the
//     right certificate address, wait for its API, then register it.

// Cluster modes.
const (
	ModeImport = "import"
	ModeCreate = "create"
)

// ClusterRequest is everything a cluster run needs.
type ClusterRequest struct {
	Provider string
	Mode     string
	Node     Target
	// Address is the address the cluster will be reached by — what the API
	// certificate must cover (create) and what replaces 127.0.0.1 in the
	// kubeconfig (both). Defaults to the SSH host.
	Address string
	// ServerURL overrides the final API URL entirely (e.g. a forwarded port or
	// a hostname other than Address). Optional.
	ServerURL string
	Version   string
	// Kubeadm configures a kubeadm cluster (ignored by k3s).
	Kubeadm KubeadmOptions
}

// ClusterHooks are the parts of a run that belong to the HTTP layer.
type ClusterHooks struct {
	// Register validates and stores the cluster (it must prove the connection
	// works before saving anything) and returns the name it was registered as.
	// kubeconfig is the admin kubeconfig as read from the server; serverURL is
	// the API address to use instead of the one inside it.
	Register func(ctx context.Context, kubeconfig Secret, serverURL string, log LogFunc) (name string, err error)
	// OnDone is called once with the registered name or the redacted error.
	OnDone func(name string, err error)
}

// StartCluster runs the job in the background and returns immediately.
func StartCluster(job *Job, req ClusterRequest, hooks ClusterHooks) {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), runTimeout)
		defer cancel()

		red := NewRedactor(req.Node.Creds.Password, req.Node.Creds.PrivateKey, req.Node.Creds.Passphrase)
		log := func(line string) { job.Log(red.Redact(line)) }

		name, err := runCluster(ctx, job, req, hooks, log)

		req.Node.Creds = Credentials{}
		if err != nil {
			err = errors.New(red.Redact(err.Error()))
			job.finish(StatusFailed, err.Error(), "")
		} else {
			job.finish(StatusSucceeded, "", name)
		}
		if hooks.OnDone != nil {
			hooks.OnDone(name, err)
		}
	}()
}

func runCluster(ctx context.Context, job *Job, req ClusterRequest, hooks ClusterHooks, log LogFunc) (name string, err error) {
	defer func() {
		if rec := recover(); rec != nil {
			err = fmt.Errorf("internal error: %v", rec)
		}
	}()

	provider, err := ProviderByName(req.Provider)
	if err != nil {
		return "", err
	}
	if req.Mode != ModeImport && req.Mode != ModeCreate {
		return "", fmt.Errorf("unknown mode %q", req.Mode)
	}
	clusterAddr := req.Address
	if clusterAddr == "" {
		clusterAddr = req.Node.Host
	}

	job.Step("Connecting to " + req.Node.Host)
	addr, err := address(req.Node)
	if err != nil {
		return "", err
	}
	node, err := Dial(ctx, addr, req.Node.Creds, req.Node.HostKeyFingerprint)
	if err != nil {
		return "", err
	}
	defer node.Close()
	log("connected as " + req.Node.Creds.Username)

	if req.Mode == ModeCreate {
		job.Step("Checking the machine is clean")
		_, err := provider.PreflightControlPlane(ctx, node, log)
		if err != nil {
			return "", fmt.Errorf("preflight failed: %w", err)
		}
		job.Step("Preparing the machine")
		if err := provider.PrepareNode(ctx, node, PrepareOptions{K8sMinor: req.Kubeadm.Minor}, log); err != nil {
			return "", fmt.Errorf("prepare failed: %w", err)
		}

		job.Step("Installing " + provider.Name() + " (single server) — this takes a few minutes")
		opts := ControlPlaneOptions{TLSSAN: clusterAddr, Version: req.Version, Kubeadm: req.Kubeadm}
		if err := provider.InstallControlPlane(ctx, node, opts, log); err != nil {
			return "", fmt.Errorf("install failed: %w", err)
		}
	}

	job.Step("Reading the cluster's kubeconfig")
	kubeconfig, err := provider.FetchKubeconfig(ctx, node)
	if err != nil {
		return "", err
	}
	log("kubeconfig read (not shown)")

	serverURL := req.ServerURL
	if serverURL == "" {
		serverURL = "https://" + net.JoinHostPort(clusterAddr, "6443")
	}

	job.Step("Connecting to the cluster and registering it")
	name, err = hooks.Register(ctx, kubeconfig, serverURL, log)
	if err != nil {
		return "", err
	}
	log(fmt.Sprintf("cluster %s registered", name))
	return name, nil
}
