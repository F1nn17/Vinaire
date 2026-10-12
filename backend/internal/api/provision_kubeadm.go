package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"github.com/f1nn17/vinaire/backend/internal/clusters"
	"github.com/f1nn17/vinaire/backend/internal/provision"
)

// Roadmap Phase 35: the kubeadm provider's pieces of the HTTP layer — what the
// wizard may offer, the kubeadm options of a create request, the cluster facts
// a new node is checked against, and Reset.

// --- options for the wizard ---

type kubeadmVersionOption struct {
	Minor   string `json:"minor"`
	Default bool   `json:"default"`
}

type kubeadmCNIOption struct {
	Name    string `json:"name"`
	Label   string `json:"label"`
	Version string `json:"version"`
	PodCIDR string `json:"podCIDR"`
	Default bool   `json:"default"`
}

// ProvisionOptions is what the wizard offers for a kubeadm install, straight
// from the backend's own kubeadm configuration.
type ProvisionOptions struct {
	Kubeadm struct {
		Versions []kubeadmVersionOption `json:"versions"`
		CNIs     []kubeadmCNIOption     `json:"cnis"`
	} `json:"kubeadm"`
}

// ProvisionOptionsHandler handles GET /api/v1/provision/options.
func (h *Handler) ProvisionOptionsHandler(w http.ResponseWriter, r *http.Request) {
	cfg := provision.CurrentKubeadmConfig()
	var out ProvisionOptions
	for _, v := range cfg.Versions {
		out.Kubeadm.Versions = append(out.Kubeadm.Versions, kubeadmVersionOption{Minor: v.Minor, Default: v.Default})
	}
	for _, c := range cfg.CNIs {
		out.Kubeadm.CNIs = append(out.Kubeadm.CNIs, kubeadmCNIOption{Name: c.Name, Label: c.Label, Version: c.Version, PodCIDR: c.PodCIDR, Default: c.Default})
	}
	respondJSON(w, out)
}

// kubeadmRequest is the kubeadm part of a create-cluster request.
type kubeadmRequest struct {
	Minor                 string   `json:"minor"`
	CNI                   string   `json:"cni"`
	SingleNode            bool     `json:"singleNode"`
	ControlPlaneEndpoint  string   `json:"controlPlaneEndpoint"`
	IgnorePreflightErrors []string `json:"ignorePreflightErrors"`
}

func (k kubeadmRequest) options() (provision.KubeadmOptions, error) {
	return provision.NormalizeKubeadmOptions(provision.CurrentKubeadmConfig(), provision.KubeadmOptions{
		Minor: k.Minor, CNI: k.CNI, SingleNode: k.SingleNode,
		ControlPlaneEndpoint: k.ControlPlaneEndpoint, IgnorePreflightErrors: k.IgnorePreflightErrors,
	})
}

// --- cluster facts for adding a node ---

// providerForCluster picks the provisioner for an Add node request that didn't
// name one: by the selected cluster's type.
func providerForCluster(entry *clusters.Entry) string {
	if entry != nil && entry.Type() == clusters.TypeKubeadm {
		return "kubeadm"
	}
	return "k3s"
}

// serverVersionFor finds the cluster's version in the form the provider wants:
// a k3s release for k3s (so a worker is never newer than the server), the
// control plane's kubelet version for kubeadm (its minor selects the packages).
func serverVersionFor(ctx context.Context, providerName string, clientset *kubernetes.Clientset) string {
	if providerName == "k3s" {
		return k3sServerVersion(ctx, clientset)
	}
	nodes, err := clientset.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if err != nil || len(nodes.Items) == 0 {
		return ""
	}
	pick := nodes.Items[0]
	for _, n := range nodes.Items {
		if _, ok := n.Labels["node-role.kubernetes.io/control-plane"]; ok {
			pick = n
			break
		}
	}
	return pick.Status.NodeInfo.KubeletVersion
}

// existingNodes returns the hook that lists the cluster's nodes' identities.
func existingNodes(clientset *kubernetes.Clientset) func(ctx context.Context) []provision.NodeIdentity {
	return func(ctx context.Context) []provision.NodeIdentity {
		list, err := clientset.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
		if err != nil {
			return nil
		}
		ids := make([]provision.NodeIdentity, 0, len(list.Items))
		for _, n := range list.Items {
			ids = append(ids, provision.NodeIdentity{Name: n.Name, SystemUUID: strings.ToLower(n.Status.NodeInfo.SystemUUID)})
		}
		return ids
	}
}

// --- reset ---

type resetRequest struct {
	Provider string          `json:"provider"`
	Node     provisionTarget `json:"node"`
	// ConfirmHost must equal node.host: the UI makes the user type it, and the
	// backend re-checks, because a reset is destructive and can't be undone.
	ConfirmHost string `json:"confirmHost"`
}

// ProvisionReset handles POST /api/v1/nodes/reset — undo a Kubernetes install
// on a machine over SSH (kubeadm reset and cleanup, or k3s's uninstall script).
// Same credential rules as every other SSH run, plus the typed confirmation.
func (h *Handler) ProvisionReset(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if !h.provisioningEnabled(w) || !h.requireSecureTransport(w, r) {
		return
	}
	_, clientset, ok := h.resolveCluster(w, r)
	if !ok || !requireNodeCreate(r.Context(), w, clientset) {
		return
	}

	var req resetRequest
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
	if req.ConfirmHost == "" || req.ConfirmHost != req.Node.Host {
		respondError(w, http.StatusBadRequest, "type the server's address to confirm — a reset is destructive and can't be undone")
		return
	}

	job, err := h.Provisions.New(ownerKey(r), provider.Name(), req.Node.Host)
	if err != nil {
		if err == provision.ErrTooManyJobs {
			respondError(w, http.StatusTooManyRequests, err.Error())
			return
		}
		respondError(w, http.StatusInternalServerError, "couldn't start the run")
		return
	}

	auditExtra := []any{
		"provider", provider.Name(), "host", node.Host, "port", node.Port,
		"username", node.Creds.Username, "authType", req.Node.Auth.Type, "job", job.ID,
	}
	h.auditLog(r, "provision-reset", "", node.Host, auditExtra...)

	job.Step("Starting")
	provision.StartReset(job, provision.ResetRequest{Provider: provider.Name(), Node: node}, func(err error) {
		result := "succeeded"
		extra := append([]any{}, auditExtra...)
		if err != nil {
			result = "failed"
			extra = append(extra, "error", err.Error())
		}
		h.auditLog(r, "provision-reset-"+result, "", node.Host, extra...)
	})

	w.WriteHeader(http.StatusAccepted)
	respondJSON(w, provisionStarted{JobID: job.ID})
}
