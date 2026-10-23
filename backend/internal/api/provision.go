package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	authorizationv1 "k8s.io/api/authorization/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"github.com/f1nn17/vinaire/backend/internal/authn"
	"github.com/f1nn17/vinaire/backend/internal/provision"
)

// Roadmap Phase 30: adding worker nodes over SSH. This is the most
// security-sensitive feature in Vinaire: it takes SSH credentials from the
// browser and runs an installer as root on a machine outside the cluster. The
// rules this file enforces (see also AGENTS.md):
//
//   - Credentials only travel over HTTPS (requireSecureTransport) and are
//     only ever in the request body — never a URL or query string, so they
//     can't land in an access log.
//   - They are held in memory for the duration of the run, wrapped in
//     provision.Secret (prints as [REDACTED]), scrubbed from every line that
//     is stored or streamed, never written to disk, never audited.
//   - The server's host key fingerprint must be fetched, shown to the user and
//     echoed back before anything is sent; the SSH handshake is aborted on a
//     mismatch.
//   - Who may do this is decided by the caller's own Kubernetes RBAC: they must
//     be allowed to `create` `nodes`.

const maxProvisionBody = 64 << 10

var fingerprintRe = regexp.MustCompile(`^SHA256:[A-Za-z0-9+/]{43}$`)

// requireSecureTransport refuses to accept credentials over plain HTTP. HTTPS
// directly, a TLS-terminating proxy that says so (X-Forwarded-Proto), and a
// loopback-to-loopback request (local development) are accepted; anything else
// is rejected unless the operator explicitly opts out.
func (h *Handler) requireSecureTransport(w http.ResponseWriter, r *http.Request) bool {
	if h.ProvisionAllowInsecure || r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https") {
		return true
	}
	if remote, _, err := net.SplitHostPort(r.RemoteAddr); err == nil && net.ParseIP(remote).IsLoopback() {
		host := r.Host
		if hh, _, err := net.SplitHostPort(r.Host); err == nil {
			host = hh
		}
		if host == "localhost" || net.ParseIP(host).IsLoopback() {
			return true
		}
	}
	respondError(w, http.StatusBadRequest,
		"credentials (SSH, tokens, passwords) may only be sent over HTTPS — open Vinaire through a TLS URL (or set PROVISION_ALLOW_INSECURE=true for local development; for a Keycloak administrator on a private network see OIDC_ALLOW_HTTP_PRIVATE_NETWORK)")
	return false
}

// requireNodeCreate checks, with the caller's own identity, that they could
// create Nodes — the real-RBAC stand-in for "may this person grow the cluster".
func requireNodeCreate(ctx context.Context, w http.ResponseWriter, clientset *kubernetes.Clientset) bool {
	review, err := clientset.AuthorizationV1().SelfSubjectAccessReviews().Create(ctx, &authorizationv1.SelfSubjectAccessReview{
		Spec: authorizationv1.SelfSubjectAccessReviewSpec{
			ResourceAttributes: &authorizationv1.ResourceAttributes{Verb: "create", Resource: "nodes"},
		},
	}, metav1.CreateOptions{})
	if err != nil {
		respondK8sError(w, err)
		return false
	}
	if !review.Status.Allowed {
		respondError(w, http.StatusForbidden, "Not allowed — adding nodes requires permission to create nodes in this cluster")
		return false
	}
	return true
}

func (h *Handler) provisioningEnabled(w http.ResponseWriter) bool {
	if h.Provisions == nil {
		respondError(w, http.StatusServiceUnavailable, "node provisioning is not enabled on this backend")
		return false
	}
	return true
}

// ownerKey identifies who started a job, so only they can watch it.
func ownerKey(r *http.Request) string {
	if sess, ok := authn.FromContext(r.Context()); ok {
		return sess.OwnerKey()
	}
	return "anonymous"
}

// --- defaults ---

type provisionDefaults struct {
	ServerURL  string `json:"serverUrl"`
	K3sVersion string `json:"k3sVersion"`
}

// k3sServerVersion finds the control plane's k3s version, so a new worker is
// never newer than the server.
func k3sServerVersion(ctx context.Context, clientset *kubernetes.Clientset) string {
	nodes, err := clientset.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if err != nil {
		return ""
	}
	for _, n := range nodes.Items {
		if v := n.Status.NodeInfo.KubeletVersion; strings.Contains(v, "+k3s") {
			return v
		}
	}
	return ""
}

// ProvisionDefaults handles GET /api/v1/nodes/provision/defaults — suggestions
// for the form. The server URL is the cluster's API address as Vinaire knows
// it, which may not be reachable from the new node (the UI says so).
func (h *Handler) ProvisionDefaults(w http.ResponseWriter, r *http.Request) {
	if !h.provisioningEnabled(w) {
		return
	}
	entry, clientset, ok := h.resolveCluster(w, r)
	if !ok {
		return
	}
	d := provisionDefaults{K3sVersion: k3sServerVersion(r.Context(), clientset)}
	if entry.RESTConfig != nil && strings.HasPrefix(entry.RESTConfig.Host, "https://") {
		d.ServerURL = entry.RESTConfig.Host
	}
	respondJSON(w, d)
}

// --- host key ---

type hostKeyRequest struct {
	Host string `json:"host"`
	Port int    `json:"port"`
}

type hostKeyResponse struct {
	Fingerprint string `json:"fingerprint"`
	KeyType     string `json:"keyType"`
	Address     string `json:"address"`
}

// ProvisionHostKey handles POST /api/v1/nodes/provision/hostkey: connect,
// read the server's SSH host key, hang up. No credentials are involved; the
// user compares the fingerprint with the machine's own before confirming.
func (h *Handler) ProvisionHostKey(w http.ResponseWriter, r *http.Request) {
	if !h.provisioningEnabled(w) {
		return
	}
	_, clientset, ok := h.resolveCluster(w, r)
	if !ok || !requireNodeCreate(r.Context(), w, clientset) {
		return
	}

	var req hostKeyRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, `request body must be {"host": "...", "port": 22}`)
		return
	}
	if req.Port == 0 {
		req.Port = 22
	}
	ip, err := provision.ValidateTarget(r.Context(), req.Host, req.Port, "")
	if err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}
	address := net.JoinHostPort(ip, strconv.Itoa(req.Port))
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	info, err := provision.FetchHostKey(ctx, address)
	if err != nil {
		respondError(w, http.StatusBadGateway, err.Error())
		return
	}
	respondJSON(w, hostKeyResponse{Fingerprint: info.Fingerprint, KeyType: info.KeyType, Address: address})
}

// --- start ---

type provisionAuth struct {
	Type       string           `json:"type"` // "key" | "password"
	Password   provision.Secret `json:"password"`
	PrivateKey provision.Secret `json:"privateKey"`
	Passphrase provision.Secret `json:"passphrase"`
}

type provisionTarget struct {
	Host               string        `json:"host"`
	Port               int           `json:"port"`
	Username           string        `json:"username"`
	Auth               provisionAuth `json:"auth"`
	HostKeyFingerprint string        `json:"hostKeyFingerprint"`
}

type provisionRequest struct {
	Provider     string           `json:"provider"`
	Node         provisionTarget  `json:"node"`
	ControlPlane *provisionTarget `json:"controlPlane"`
	ServerURL    string           `json:"serverUrl"`
	Token        provision.Secret `json:"token"`
	Version      string           `json:"version"`
	NodeName     string           `json:"nodeName"`
}

func (t provisionTarget) toTarget(label string) (provision.Target, error) {
	if t.Port == 0 {
		t.Port = 22
	}
	if t.Username == "" {
		return provision.Target{}, fmt.Errorf("%s: username is required", label)
	}
	if !fingerprintRe.MatchString(t.HostKeyFingerprint) {
		return provision.Target{}, fmt.Errorf("%s: confirm the host key fingerprint first", label)
	}
	creds := provision.Credentials{Username: t.Username}
	switch t.Auth.Type {
	case "key":
		if t.Auth.PrivateKey == "" {
			return provision.Target{}, fmt.Errorf("%s: a private key is required", label)
		}
		creds.PrivateKey, creds.Passphrase = t.Auth.PrivateKey, t.Auth.Passphrase
	case "password":
		if t.Auth.Password == "" {
			return provision.Target{}, fmt.Errorf("%s: a password is required", label)
		}
		creds.Password = t.Auth.Password
	default:
		return provision.Target{}, fmt.Errorf(`%s: auth.type must be "key" or "password"`, label)
	}
	if _, err := provision.ValidateTarget(context.Background(), t.Host, t.Port, t.Username); err != nil {
		return provision.Target{}, fmt.Errorf("%s: %w", label, err)
	}
	return provision.Target{Host: t.Host, Port: t.Port, Creds: creds, HostKeyFingerprint: t.HostKeyFingerprint}, nil
}

type provisionStarted struct {
	JobID string `json:"jobId"`
}

// waitForNode returns the hook that confirms the node in the cluster.
func waitForNode(clientset *kubernetes.Clientset) func(ctx context.Context, name string, log provision.LogFunc) error {
	return func(ctx context.Context, name string, log provision.LogFunc) error {
		tick := time.NewTicker(3 * time.Second)
		defer tick.Stop()
		reported := false
		// A join takes the node's kubelet through several restarts and the API is
		// often reached over a flaky path (a port-forward, a load balancer), so a
		// single dropped request must not fail an install that actually worked.
		// Permission errors are real and fatal; anything else is retried a few
		// times before giving up.
		const maxTransient = 10
		transient := 0
		for {
			node, err := clientset.CoreV1().Nodes().Get(ctx, name, metav1.GetOptions{})
			if err == nil || apierrors.IsNotFound(err) {
				transient = 0
			}
			switch {
			case err == nil:
				for _, c := range node.Status.Conditions {
					if c.Type == corev1.NodeReady && c.Status == corev1.ConditionTrue {
						return nil
					}
				}
				if !reported {
					log("node " + name + " registered, waiting for Ready")
					reported = true
				}
			case apierrors.IsNotFound(err):
			case apierrors.IsUnauthorized(err) || apierrors.IsForbidden(err) || ctx.Err() != nil:
				return fmt.Errorf("couldn't check the node in the cluster: %w", err)
			default:
				transient++
				if transient >= maxTransient {
					return fmt.Errorf("couldn't check the node in the cluster: %w", err)
				}
			}
			select {
			case <-ctx.Done():
				return fmt.Errorf("node %s didn't become Ready in time", name)
			case <-tick.C:
			}
		}
	}
}

// ProvisionStart handles POST /api/v1/nodes/provision.
func (h *Handler) ProvisionStart(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if !h.provisioningEnabled(w) || !h.requireSecureTransport(w, r) {
		return
	}
	entry, clientset, ok := h.resolveCluster(w, r)
	if !ok || !requireNodeCreate(r.Context(), w, clientset) {
		return
	}

	var req provisionRequest
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxProvisionBody))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.Provider == "" {
		req.Provider = providerForCluster(entry)
	}
	provider, err := provision.ProviderByName(req.Provider)
	if err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}
	node, err := req.Node.toTarget("node")
	if err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}
	var cp *provision.Target
	if req.ControlPlane != nil {
		t, err := req.ControlPlane.toTarget("control plane")
		if err != nil {
			respondError(w, http.StatusBadRequest, err.Error())
			return
		}
		cp = &t
	}
	switch {
	case req.Token == "" && cp == nil:
		respondError(w, http.StatusBadRequest, "provide the join token, or SSH access to the control plane to read it from")
		return
	case req.Token != "":
		if err := provider.ValidateJoinToken(req.Token); err != nil {
			respondError(w, http.StatusBadRequest, err.Error())
			return
		}
		// Where the worker will connect: from the join command (kubeadm) or the
		// explicit server URL (k3s).
		if _, err := provider.JoinEndpoint(req.ServerURL, req.Token); err != nil {
			respondError(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	if req.ServerURL != "" {
		if err := provision.ValidateServerURL(req.ServerURL); err != nil {
			respondError(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	if req.Version != "" && provider.Name() == "k3s" {
		if err := provision.ValidateVersion(req.Version); err != nil {
			respondError(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	if err := provision.ValidateNodeName(req.NodeName); err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}
	version := req.Version
	if version == "" {
		version = serverVersionFor(r.Context(), provider.Name(), clientset)
	}

	job, err := h.Provisions.New(ownerKey(r), provider.Name(), req.Node.Host)
	if err != nil {
		if errors.Is(err, provision.ErrTooManyJobs) {
			respondError(w, http.StatusTooManyRequests, err.Error())
			return
		}
		respondError(w, http.StatusInternalServerError, "couldn't start the run")
		return
	}

	// Audited with who/where/how — never with a secret.
	cpHost := ""
	if cp != nil {
		cpHost = cp.Host
	}
	auditExtra := []any{
		"provider", provider.Name(), "host", node.Host, "port", node.Port, "username", node.Creds.Username,
		"authType", req.Node.Auth.Type, "controlPlane", cpHost, "version", version, "job", job.ID,
	}
	h.auditLog(r, "provision-node", "", node.Host, auditExtra...)

	job.Step("Starting")
	provision.Start(job, provision.Request{
		Provider:     provider.Name(),
		Node:         node,
		ControlPlane: cp,
		ServerURL:    req.ServerURL,
		Token:        req.Token,
		Version:      version,
		NodeName:     req.NodeName,
	}, provision.Hooks{
		ExistingNodes: existingNodes(clientset),
		WaitForNode:   waitForNode(clientset),
		OnDone: func(nodeName string, err error) {
			result := "succeeded"
			extra := append([]any{}, auditExtra...)
			if err != nil {
				result = "failed"
				extra = append(extra, "error", err.Error())
			} else {
				extra = append(extra, "node", nodeName)
			}
			h.auditLog(r, "provision-node-"+result, "", node.Host, extra...)
		},
	})

	// The decoded request (and the secrets in it) is dropped here; the run
	// goroutine holds its own copy only until it finishes.
	req = provisionRequest{}
	w.WriteHeader(http.StatusAccepted)
	respondJSON(w, provisionStarted{JobID: job.ID})
}

// ProvisionStatus handles GET /api/v1/nodes/provision/{id}: the job's progress
// log so far, for a reconnecting client.
func (h *Handler) ProvisionStatus(w http.ResponseWriter, r *http.Request) {
	if !h.provisioningEnabled(w) {
		return
	}
	job, ok := h.Provisions.Get(chi.URLParam(r, "id"), ownerKey(r))
	if !ok {
		respondError(w, http.StatusNotFound, "no such provisioning run")
		return
	}
	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(job.Snapshot()); err != nil {
		respondError(w, http.StatusInternalServerError, "couldn't encode the run")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(buf.Bytes())
}
