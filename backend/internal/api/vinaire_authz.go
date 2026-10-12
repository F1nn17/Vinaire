package api

import (
	"context"
	"net/http"
	"time"

	authzv1 "k8s.io/api/authorization/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/rest"
	metricsclientset "k8s.io/metrics/pkg/client/clientset/versioned"
	gatewayclientset "sigs.k8s.io/gateway-api/pkg/client/clientset/versioned"

	"github.com/f1nn17/vinaire/backend/internal/authn"
	"github.com/f1nn17/vinaire/backend/internal/clusters"
)

// Roadmap Phase 36. Two things live here.
//
// 1. Permission for actions on Vinaire ITSELF. Most of what Vinaire does runs as
// the caller's own Kubernetes identity, so the cluster's RBAC decides. But a few
// actions change Vinaire's own state, which no Kubernetes RBAC rule covers:
// adding or removing a cluster in its registry, changing a cluster's type, and
// GitOps mode and sources. Without a gate, any logged-in user could add a cluster
// whose credentials Vinaire then holds. The gate keeps the same philosophy — the
// cluster's RBAC decides — by asking it, with a SelfSubjectAccessReview as the
// caller, whether they may <verb> a *virtual* resource in the group vinaire.io.
// The resource doesn't exist in the cluster; it is only a name for the permission,
// and an administrator grants it with an ordinary ClusterRole (see AGENTS.md).
// It is asked of the registry's default cluster, Vinaire's "home".
//
// 2. The caller's identity for the optional metrics and Gateway API clients,
// which were built at startup with Vinaire's own credentials and so ignored the
// caller's RBAC.

// VinaireGroup is the API group of the virtual permission resources
// ("clusters", "gitops") that gate changes to Vinaire's own configuration.
const VinaireGroup = "vinaire.io"

// requireVinaireRight writes a 403 (and returns false) unless the caller may
// `verb` the virtual resource. Auth mode "disabled" — local development, where
// there is no caller identity at all — is allowed through.
func (h *Handler) requireVinaireRight(w http.ResponseWriter, r *http.Request, resource, verb string) bool {
	if h.Auth.Mode == authn.ModeDisabled {
		return true
	}
	sess, ok := authn.FromContext(r.Context())
	if !ok {
		respondError(w, http.StatusUnauthorized, "unauthenticated")
		return false
	}
	home := h.Clusters.Default()
	if home == nil || home.Clientset == nil {
		respondError(w, http.StatusServiceUnavailable, "no cluster to check this permission against")
		return false
	}
	cs, err := home.ClientsetForToken(sess.K8sToken)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to build kubernetes client: "+err.Error())
		return false
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	res, err := cs.AuthorizationV1().SelfSubjectAccessReviews().Create(ctx, &authzv1.SelfSubjectAccessReview{
		Spec: authzv1.SelfSubjectAccessReviewSpec{ResourceAttributes: &authzv1.ResourceAttributes{
			Group: VinaireGroup, Resource: resource, Verb: verb,
		}},
	}, metav1.CreateOptions{})
	if err != nil {
		if apierrors.IsUnauthorized(err) {
			respondError(w, http.StatusUnauthorized, "unauthorized")
		} else {
			respondError(w, http.StatusBadGateway, "couldn't check this permission: "+statusMessage(err))
		}
		return false
	}
	if !res.Status.Allowed {
		respondError(w, http.StatusForbidden, "this changes Vinaire's own configuration and needs permission to "+verb+" "+
			resource+"."+VinaireGroup+" on cluster "+home.Name+" — ask whoever administers Vinaire to grant it (the vinaire-admin ClusterRole)")
		return false
	}
	return true
}

// callerConfig is the REST config the caller's own identity uses against entry —
// what resolveRESTConfig returns, for handlers that already hold the entry.
// nil means there is no caller (the middleware makes that impossible on a
// protected route; the nil is a fail-closed default).
func (h *Handler) callerConfig(r *http.Request, entry *clusters.Entry) *rest.Config {
	if h.Auth.Mode == authn.ModeDisabled {
		return entry.RESTConfig
	}
	sess, ok := authn.FromContext(r.Context())
	if !ok {
		return nil
	}
	return entry.RESTConfigForToken(sess.K8sToken)
}

// metricsFor is entry's metrics client acting as the caller. nil when metrics
// aren't available (callers already treat nil as "unavailable").
func (h *Handler) metricsFor(r *http.Request, entry *clusters.Entry) *metricsclientset.Clientset {
	if h.Auth.Mode == authn.ModeDisabled {
		return entry.MetricsClientset
	}
	if entry.MetricsClientset == nil {
		return nil // the cluster doesn't serve it
	}
	cfg := h.callerConfig(r, entry)
	if cfg == nil {
		return nil
	}
	mc, err := metricsclientset.NewForConfig(cfg)
	if err != nil {
		return nil
	}
	return mc
}

// gatewayFor is entry's Gateway API client acting as the caller.
func (h *Handler) gatewayFor(r *http.Request, entry *clusters.Entry) *gatewayclientset.Clientset {
	if h.Auth.Mode == authn.ModeDisabled {
		return entry.GatewayClientset
	}
	if entry.GatewayClientset == nil {
		return nil
	}
	cfg := h.callerConfig(r, entry)
	if cfg == nil {
		return nil
	}
	gc, err := gatewayclientset.NewForConfig(cfg)
	if err != nil {
		return nil
	}
	return gc
}
