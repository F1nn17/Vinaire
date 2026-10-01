package provision

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strconv"
	"time"
)

const runTimeout = 12 * time.Minute

// Target is one SSH-reachable machine plus how to log into it.
type Target struct {
	Host               string
	Port               int
	Creds              Credentials
	HostKeyFingerprint string
}

// Request is everything a run needs. Its Secret fields are the only place the
// credentials and token live; Run clears them when it finishes.
type Request struct {
	Provider     string
	Node         Target
	ControlPlane *Target // optional: where to read the join token from
	ServerURL    string  // cluster API as reachable from the new node
	Token        Secret  // optional if ControlPlane is set
	Version      string
	NodeName     string
}

// Hooks are the bits of Run that talk to the Kubernetes API, supplied by the
// HTTP layer so this package stays free of cluster clients.
type Hooks struct {
	// WaitForNode blocks until the named node is Ready in the target cluster.
	WaitForNode func(ctx context.Context, name string, log LogFunc) error
	// OnDone is called once, after the run ends, with the node name (on
	// success) or the redacted error. Used for the audit trail.
	OnDone func(nodeName string, err error)
}

func address(t Target) (string, error) {
	ip, err := ValidateTarget(context.Background(), t.Host, t.Port, t.Creds.Username)
	if err != nil {
		return "", err
	}
	return net.JoinHostPort(ip, strconv.Itoa(t.Port)), nil
}

// Start runs the job in the background and returns immediately.
func Start(job *Job, req Request, hooks Hooks) {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), runTimeout)
		defer cancel()

		red := NewRedactor(
			req.Node.Creds.Password, req.Node.Creds.PrivateKey, req.Node.Creds.Passphrase,
			req.Token,
		)
		if req.ControlPlane != nil {
			red.Add(req.ControlPlane.Creds.Password)
			red.Add(req.ControlPlane.Creds.PrivateKey)
			red.Add(req.ControlPlane.Creds.Passphrase)
		}
		log := func(line string) { job.Log(red.Redact(line)) }

		nodeName, err := run(ctx, job, red, req, hooks, log)

		// Drop every reference to the secrets. Go strings can't be zeroed, so
		// this is best effort — the real protection is that nothing stores them.
		req.Node.Creds = Credentials{}
		req.ControlPlane = nil
		req.Token = ""

		if err != nil {
			err = errors.New(red.Redact(err.Error()))
			job.finish(StatusFailed, err.Error(), "")
		} else {
			job.finish(StatusSucceeded, "", nodeName)
		}
		if hooks.OnDone != nil {
			hooks.OnDone(nodeName, err)
		}
	}()
}

func run(ctx context.Context, job *Job, red *Redactor, req Request, hooks Hooks, log LogFunc) (nodeName string, err error) {
	defer func() {
		if rec := recover(); rec != nil {
			err = fmt.Errorf("internal error: %v", rec)
		}
	}()

	provider, err := ProviderByName(req.Provider)
	if err != nil {
		return "", err
	}

	// 1. Where does the join token come from?
	serverURL, token := req.ServerURL, req.Token
	if token == "" {
		if req.ControlPlane == nil {
			return "", errors.New("a join token or control-plane SSH access is required")
		}
		job.Step("Reading the join token from the control plane")
		cpAddr, err := address(*req.ControlPlane)
		if err != nil {
			return "", fmt.Errorf("control plane: %w", err)
		}
		cp, err := Dial(ctx, cpAddr, req.ControlPlane.Creds, req.ControlPlane.HostKeyFingerprint)
		if err != nil {
			return "", fmt.Errorf("control plane: %w", err)
		}
		token, err = provider.JoinTokenFromControlPlane(ctx, cp)
		_ = cp.Close()
		if err != nil {
			return "", err
		}
		red.Add(token)
		log("join token read from the control plane (not shown)")
		if serverURL == "" {
			serverURL = "https://" + net.JoinHostPort(req.ControlPlane.Host, "6443")
		}
	}
	if serverURL == "" {
		return "", errors.New("the cluster server URL is required")
	}
	if err := ValidateServerURL(serverURL); err != nil {
		return "", err
	}

	// 2. Log into the new node (host key must match what the user confirmed).
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

	// 3. Preflight, prepare, join.
	job.Step("Checking the machine")
	facts, err := provider.Preflight(ctx, node, serverURL, log)
	if err != nil {
		return "", fmt.Errorf("preflight failed: %w", err)
	}
	nodeName = req.NodeName
	if nodeName == "" {
		nodeName = facts.Hostname
	}

	job.Step("Preparing the machine")
	if err := provider.PrepareNode(ctx, node, log); err != nil {
		return "", fmt.Errorf("prepare failed: %w", err)
	}

	job.Step("Installing " + provider.Name() + " and joining the cluster")
	if err := provider.JoinWorker(ctx, node, JoinInfo{ServerURL: serverURL, Token: token, Version: req.Version, NodeName: nodeName}, log); err != nil {
		return "", fmt.Errorf("install failed: %w", err)
	}

	// 4. Confirm in the cluster itself.
	job.Step("Waiting for the node to register and become Ready")
	if hooks.WaitForNode != nil {
		if err := hooks.WaitForNode(ctx, nodeName, log); err != nil {
			return "", err
		}
	}
	log(fmt.Sprintf("node %s is Ready", nodeName))
	return nodeName, nil
}
