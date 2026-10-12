package api

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"strings"

	"github.com/f1nn17/vinaire/backend/internal/clusters"
	"github.com/f1nn17/vinaire/backend/internal/provision"
)

// Roadmap Phase 34: adding a cluster that lives on another server, over SSH.
// "Import" reads a running k3s server's kubeconfig; "Create" installs a new
// single-server k3s on a clean machine first. Everything about handling SSH
// credentials is Phase 30's and is enforced the same way here: HTTPS only,
// credentials only in the POST body and only in memory, host key pinned before
// anything is sent, the caller must be allowed to create nodes, runs audited
// without secrets. See provision.go.

type clusterSSHRequest struct {
	Provider string          `json:"provider"`
	Name     string          `json:"name"`
	Node     provisionTarget `json:"node"`
	// Address is how the cluster will be reached (and what its certificate must
	// cover); defaults to the SSH host. ServerURL overrides the whole API URL.
	Address   string `json:"address"`
	ServerURL string `json:"serverUrl"`
	Version   string `json:"version"`
	Type      string `json:"type"`
	// Kubeadm configures a kubeadm install (create mode); ignored for k3s.
	Kubeadm kubeadmRequest `json:"kubeadm"`
}

// ClusterSSHStart returns the handler for POST /api/v1/clusters/{import,create}-ssh.
func (h *Handler) ClusterSSHStart(mode string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		// it adds a cluster to Vinaire's registry as well as running code on a server
		if !h.requireVinaireRight(w, r, "clusters", "create") {
			return
		}
		if !h.provisioningEnabled(w) || !h.requireSecureTransport(w, r) {
			return
		}
		if h.ClusterStore == nil {
			respondError(w, http.StatusServiceUnavailable, "cluster management is unavailable here: run Vinaire in a cluster, or set VINAIRE_DATA_DIR so there is somewhere to keep the encrypted registry")
			return
		}
		_, clientset, ok := h.resolveCluster(w, r)
		if !ok || !requireNodeCreate(r.Context(), w, clientset) {
			return
		}

		var req clusterSSHRequest
		dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxProvisionBody))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&req); err != nil {
			respondError(w, http.StatusBadRequest, "invalid request body")
			return
		}

		provider, err := provision.ProviderByName(req.Provider)
		if err != nil {
			respondError(w, http.StatusBadRequest, err.Error())
			return
		}
		node, err := req.Node.toTarget("server")
		if err != nil {
			respondError(w, http.StatusBadRequest, err.Error())
			return
		}
		name := strings.TrimSpace(req.Name)
		if !clusterNameRe.MatchString(name) {
			respondError(w, http.StatusBadRequest, "cluster name must be 1-63 letters, digits, '.', '_' or '-'")
			return
		}
		// Fail before touching the server if the name is taken — otherwise a
		// fresh install would succeed and then fail to register.
		if _, exists := h.Clusters.Get(name); exists {
			respondError(w, http.StatusConflict, "a cluster named "+name+" is already configured")
			return
		}
		if req.Address != "" && net.ParseIP(req.Address) == nil && !validHostname(req.Address) {
			respondError(w, http.StatusBadRequest, "address must be an IP address or a hostname")
			return
		}
		if req.ServerURL != "" {
			if err := provision.ValidateServerURL(req.ServerURL); err != nil {
				respondError(w, http.StatusBadRequest, err.Error())
				return
			}
		}
		var kubeadmOpts provision.KubeadmOptions
		if provider.Name() == "k3s" {
			if err := provision.ValidateVersion(req.Version); err != nil {
				respondError(w, http.StatusBadRequest, err.Error())
				return
			}
		} else if mode == provision.ModeCreate {
			var err error
			if kubeadmOpts, err = req.Kubeadm.options(); err != nil {
				respondError(w, http.StatusBadRequest, err.Error())
				return
			}
		}
		if req.Type != "" && !clusters.ValidType(req.Type) {
			respondError(w, http.StatusBadRequest, "unknown cluster type")
			return
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

		origin := clusters.OriginSSHImport
		if mode == provision.ModeCreate {
			origin = clusters.OriginSSHCreate
		}
		clusterType := req.Type
		if clusterType == "" {
			clusterType = clusters.TypeK3s
			if provider.Name() == "kubeadm" {
				clusterType = clusters.TypeKubeadm
			}
		}
		fingerprint := node.HostKeyFingerprint

		// Audited with who/where/how — never with a secret.
		auditExtra := []any{
			"mode", mode, "provider", provider.Name(), "host", node.Host, "port", node.Port,
			"username", node.Creds.Username, "authType", req.Node.Auth.Type, "cluster", name, "job", job.ID,
		}
		h.auditLog(r, "provision-cluster", "", name, auditExtra...)

		job.Step("Starting")
		provision.StartCluster(job, provision.ClusterRequest{
			Provider: provider.Name(), Mode: mode, Node: node,
			Address: req.Address, ServerURL: req.ServerURL, Version: req.Version,
			Kubeadm: kubeadmOpts,
		}, provision.ClusterHooks{
			Register: func(ctx context.Context, kubeconfig provision.Secret, serverURL string, log provision.LogFunc) (string, error) {
				info, err := clusters.ParseKubeconfig([]byte(kubeconfig.Reveal()), clusters.ParseOptions{ServerOverride: serverURL})
				if err != nil {
					return "", err
				}
				if _, err := h.registerCluster(ctx, r, clusters.StoredCluster{
					Name: name, Server: info.Server, Kubeconfig: string(info.Kubeconfig),
					Type: clusterType, Origin: origin, SSHHostKey: fingerprint,
				}, ""); err != nil {
					return "", err
				}
				return name, nil
			},
			OnDone: func(clusterName string, err error) {
				result := "succeeded"
				extra := append([]any{}, auditExtra...)
				if err != nil {
					result = "failed"
					extra = append(extra, "error", err.Error())
				}
				h.auditLog(r, "provision-cluster-"+result, "", name, extra...)
			},
		})

		w.WriteHeader(http.StatusAccepted)
		respondJSON(w, provisionStarted{JobID: job.ID})
	}
}

func validHostname(s string) bool {
	if len(s) == 0 || len(s) > 253 {
		return false
	}
	for _, label := range strings.Split(s, ".") {
		if label == "" || len(label) > 63 || strings.HasPrefix(label, "-") || strings.HasSuffix(label, "-") {
			return false
		}
		for _, c := range label {
			if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-') {
				return false
			}
		}
	}
	return true
}
