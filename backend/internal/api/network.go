package api

import (
	"context"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	networkingv1 "k8s.io/api/networking/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	"github.com/shiraku/vinaire/backend/internal/k8s"
)

// certExpiryWarnWindow is how far ahead of a TLS cert's expiry the UI
// starts flagging it as "Expiring soon" instead of "Valid".
const certExpiryWarnWindow = 14 * 24 * time.Hour

// IngressRow matches the frontend's IngressRow type
// (frontend/src/lib/types.ts).
type IngressRow struct {
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
	Hosts     string `json:"hosts"`
	Backend   string `json:"backend"`
	TLS       string `json:"tls"`
	TLSColor  string `json:"tlsColor"`
}

// Ingresses handles GET /api/v1/ingresses?namespace=.
func (h *Handler) Ingresses(w http.ResponseWriter, r *http.Request) {
	_, clientset, ok := h.resolveCluster(w, r)
	if !ok {
		return
	}
	namespace := r.URL.Query().Get("namespace")

	ingresses, err := k8s.ListIngresses(r.Context(), clientset, namespace)
	if err != nil {
		respondK8sError(w, err)
		return
	}

	rows := make([]IngressRow, 0, len(ingresses))
	for _, ing := range ingresses {
		rows = append(rows, h.ingressRow(r.Context(), clientset, ing))
	}
	respondJSON(w, rows)
}

func (h *Handler) ingressRow(ctx context.Context, clientset *kubernetes.Clientset, ing networkingv1.Ingress) IngressRow {
	hosts := uniqueOrdered(func(yield func(string)) {
		for _, rule := range ing.Spec.Rules {
			if rule.Host != "" {
				yield(rule.Host)
			}
		}
	})
	hostsStr := strings.Join(hosts, ", ")
	if hostsStr == "" {
		hostsStr = "*"
	}

	backends := uniqueOrdered(func(yield func(string)) {
		for _, rule := range ing.Spec.Rules {
			if rule.HTTP == nil {
				continue
			}
			for _, path := range rule.HTTP.Paths {
				if path.Backend.Service == nil {
					continue
				}
				port := path.Backend.Service.Port.Name
				if path.Backend.Service.Port.Number != 0 {
					port = strconv.Itoa(int(path.Backend.Service.Port.Number))
				}
				yield(fmt.Sprintf("%s:%s", path.Backend.Service.Name, port))
			}
		}
	})
	backendStr := strings.Join(backends, ", ")
	if backendStr == "" {
		backendStr = "—"
	}

	tlsText, tlsColor := h.ingressTLSStatus(ctx, clientset, ing)

	return IngressRow{
		Name:      ing.Name,
		Namespace: ing.Namespace,
		Hosts:     hostsStr,
		Backend:   backendStr,
		TLS:       tlsText,
		TLSColor:  string(tlsColor),
	}
}

// ingressTLSStatus fetches every Secret an Ingress's spec.tls references,
// parses the certificate, and reports the nearest expiry — real validity
// checking, not just "a tls block exists".
func (h *Handler) ingressTLSStatus(ctx context.Context, clientset *kubernetes.Clientset, ing networkingv1.Ingress) (string, StatusColor) {
	if len(ing.Spec.TLS) == 0 {
		return "None", StatusWarn
	}

	var nearestExpiry *time.Time
	for _, tls := range ing.Spec.TLS {
		if tls.SecretName == "" {
			continue
		}
		secret, err := k8s.GetSecret(ctx, clientset, ing.Namespace, tls.SecretName)
		if err != nil {
			slog.Warn("ingress TLS secret unavailable", "namespace", ing.Namespace, "secret", tls.SecretName, "error", err)
			continue
		}
		certPEM, ok := secret.Data["tls.crt"]
		if !ok {
			continue
		}
		block, _ := pem.Decode(certPEM)
		if block == nil {
			continue
		}
		cert, err := x509.ParseCertificate(block.Bytes)
		if err != nil {
			continue
		}
		if nearestExpiry == nil || cert.NotAfter.Before(*nearestExpiry) {
			expiry := cert.NotAfter
			nearestExpiry = &expiry
		}
	}

	if nearestExpiry == nil {
		return "Unknown", StatusWarn
	}

	remaining := time.Until(*nearestExpiry)
	switch {
	case remaining < 0:
		return "Expired", StatusErr
	case remaining < certExpiryWarnWindow:
		return fmt.Sprintf("Expiring in %dd", int(remaining.Hours()/24)), StatusWarn
	default:
		return "Valid", StatusOK
	}
}

// GatewayRow matches the frontend's GatewayRow type.
type GatewayRow struct {
	Name        string `json:"name"`
	Namespace   string `json:"namespace"`
	Kind        string `json:"kind"` // "Gateway" | "HTTPRoute"
	Status      string `json:"status"`
	StatusColor string `json:"statusColor"`
}

// GatewayResourcesResponse matches the frontend's GatewayResourcesResult
// discriminated union — Gateway API CRDs aren't installed on every
// cluster (stock k3s/k3d don't ship them), so this says so explicitly
// instead of returning an empty list or a 500.
type GatewayResourcesResponse struct {
	Available bool         `json:"available"`
	Reason    string       `json:"reason,omitempty"`
	Rows      []GatewayRow `json:"rows"`
}

// GatewayRoutes handles GET /api/v1/gateway-routes?namespace=.
func (h *Handler) GatewayRoutes(w http.ResponseWriter, r *http.Request) {
	cluster, _, ok := h.resolveCluster(w, r)
	if !ok {
		return
	}
	namespace := r.URL.Query().Get("namespace")
	if cluster.GatewayClientset == nil {
		respondJSON(w, GatewayResourcesResponse{
			Available: false,
			Reason:    "Gateway API client is not configured on this backend",
		})
		return
	}

	ctx := r.Context()

	gateways, err := cluster.GatewayClientset.GatewayV1().Gateways(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		if isGatewayAPIUnavailable(err) {
			respondJSON(w, GatewayResourcesResponse{
				Available: false,
				Reason:    "Gateway API CRDs are not installed on this cluster",
			})
			return
		}
		respondK8sError(w, err)
		return
	}

	httpRoutes, err := cluster.GatewayClientset.GatewayV1().HTTPRoutes(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		if isGatewayAPIUnavailable(err) {
			respondJSON(w, GatewayResourcesResponse{
				Available: false,
				Reason:    "Gateway API CRDs are not installed on this cluster",
			})
			return
		}
		respondK8sError(w, err)
		return
	}

	rows := make([]GatewayRow, 0, len(gateways.Items)+len(httpRoutes.Items))
	for _, g := range gateways.Items {
		rows = append(rows, gatewayNodeRow(g))
	}
	for _, hr := range httpRoutes.Items {
		rows = append(rows, httpRouteRow(hr))
	}
	respondJSON(w, GatewayResourcesResponse{Available: true, Rows: rows})
}

// isGatewayAPIUnavailable reports whether err means "this CRD/API group
// isn't registered on the server" — a List (no resource name given) only
// 404s when the resource *type* doesn't exist; an installed-but-empty CRD
// returns 200 with an empty list, never a 404.
func isGatewayAPIUnavailable(err error) bool {
	return apierrors.IsNotFound(err) || meta.IsNoMatchError(err)
}

func gatewayNodeRow(g gatewayv1.Gateway) GatewayRow {
	status, color := "Pending", StatusWarn
	for _, c := range g.Status.Conditions {
		if c.Type == "Programmed" && c.Status == metav1.ConditionTrue {
			status, color = "Programmed", StatusOK
			break
		}
	}
	return GatewayRow{
		Name:        g.Name,
		Namespace:   g.Namespace,
		Kind:        "Gateway",
		Status:      status,
		StatusColor: string(color),
	}
}

func httpRouteRow(hr gatewayv1.HTTPRoute) GatewayRow {
	status, color := "Pending", StatusWarn
loop:
	for _, parent := range hr.Status.Parents {
		for _, c := range parent.Conditions {
			if c.Type == "Accepted" && c.Status == metav1.ConditionTrue {
				status, color = "Accepted", StatusOK
				break loop
			}
		}
	}
	return GatewayRow{
		Name:        hr.Name,
		Namespace:   hr.Namespace,
		Kind:        "HTTPRoute",
		Status:      status,
		StatusColor: string(color),
	}
}

// uniqueOrdered collects values from a push-style iterator, keeping first-
// seen order and dropping duplicates — used to build the Hosts/Backend
// summary columns without pulling in a generic ordered-set dependency.
func uniqueOrdered(iterate func(yield func(string))) []string {
	seen := map[string]struct{}{}
	var out []string
	iterate(func(v string) {
		if _, ok := seen[v]; ok {
			return
		}
		seen[v] = struct{}{}
		out = append(out, v)
	})
	return out
}
