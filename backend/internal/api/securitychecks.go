package api

import (
	"net/http"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"

	"github.com/f1nn17/vinaire/backend/internal/k8s"
	"github.com/f1nn17/vinaire/backend/internal/securitychecks"
)

// SecurityChecksResponse is GET /api/v1/security/checks.
type SecurityChecksResponse struct {
	GeneratedAt string                  `json:"generatedAt"`
	Results     []securitychecks.Result `json:"results"`
	Failed      int                     `json:"failed"`
	Passed      int                     `json:"passed"`
	Unknown     int                     `json:"unknown"`
}

// SecurityChecks handles GET /api/v1/security/checks (Roadmap Phase 54, light version).
//
// It only READS, as the caller: every list goes through the caller's own identity, so a kind
// they may not list makes the checks that need it "unknown" instead of leaking or guessing. No
// Job, no node access, no write of any kind.
func (h *Handler) SecurityChecks(w http.ResponseWriter, r *http.Request) {
	_, cs, ok := h.resolveCluster(w, r)
	if !ok {
		return
	}
	ctx := r.Context()
	snap := &securitychecks.Snapshot{Unavailable: map[string]string{}}
	note := func(kind string, err error) bool {
		if err == nil {
			return true
		}
		if apierrors.IsForbidden(err) || apierrors.IsUnauthorized(err) {
			snap.Unavailable[kind] = "you may not list " + kind
		} else {
			snap.Unavailable[kind] = "could not list " + kind
		}
		return false
	}
	var err error
	if snap.Namespaces, err = k8s.ListNamespaces(ctx, cs); !note("namespaces", err) {
		snap.Namespaces = nil
	}
	if snap.Pods, err = k8s.ListPods(ctx, cs, ""); !note("pods", err) {
		snap.Pods = nil
	}
	if snap.Services, err = k8s.ListServices(ctx, cs, ""); !note("services", err) {
		snap.Services = nil
	}
	if snap.Ingresses, err = k8s.ListIngresses(ctx, cs, ""); !note("ingresses", err) {
		snap.Ingresses = nil
	}
	if snap.NetworkPolicies, err = k8s.ListNetworkPolicies(ctx, cs, ""); !note("networkpolicies", err) {
		snap.NetworkPolicies = nil
	}
	if snap.ResourceQuotas, err = k8s.ListResourceQuotas(ctx, cs, ""); !note("resourcequotas", err) {
		snap.ResourceQuotas = nil
	}
	if snap.Roles, err = k8s.ListRoles(ctx, cs, ""); !note("roles", err) {
		snap.Roles = nil
	}
	if snap.ClusterRoles, err = k8s.ListClusterRoles(ctx, cs); !note("clusterroles", err) {
		snap.ClusterRoles = nil
	}
	if snap.RoleBindings, err = k8s.ListRoleBindings(ctx, cs, ""); !note("rolebindings", err) {
		snap.RoleBindings = nil
	}
	if snap.ClusterRoleBindings, err = k8s.ListClusterRoleBindings(ctx, cs); !note("clusterrolebindings", err) {
		snap.ClusterRoleBindings = nil
	}

	resp := SecurityChecksResponse{GeneratedAt: time.Now().UTC().Format(time.RFC3339), Results: securitychecks.RunAll(snap)}
	for _, res := range resp.Results {
		switch res.Status {
		case securitychecks.Fail:
			resp.Failed++
		case securitychecks.Pass:
			resp.Passed++
		default:
			resp.Unknown++
		}
	}
	respondJSON(w, resp)
}
