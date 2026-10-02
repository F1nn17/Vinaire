package api

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/client-go/kubernetes"

	"github.com/shiraku/vinaire/backend/internal/authn"
	"github.com/shiraku/vinaire/backend/internal/k8s"
)

// Roadmap Phase 26: an authenticated reverse HTTP proxy to a pod's port, at
//
//	/proxy/{cluster}/{namespace}/{pod}/{port}/…   (or {https:port} for TLS)
//
// Each proxied request dials the pod through the Kubernetes portforward
// subresource (internal/k8s.DialPodPort) — no OS port is opened on this
// server. Keep-alives are off, so every connection is tied to the request
// that needed it: close the tab and the request context cancels, the dial's
// conn closes, and the SPDY connection goes with it.
//
// The caller's own RBAC (`create` on `pods/portforward`) decides access.
//
// Because the proxied page is served from Vinaire's own origin, a hostile or
// merely buggy pod web UI must not be able to act with the user's Vinaire
// session. So the proxy: sends the page with a `Content-Security-Policy:
// sandbox` (opaque origin — no access to Vinaire's cookie or same-origin
// API), never forwards the session Cookie/Authorization to the pod, and drops
// the pod's Set-Cookie (it could otherwise overwrite Vinaire's cookie).

const (
	maxConcurrentProxied = 32
	proxyResponseTimeout = 60 * time.Second
	// auditWindow dedupes the audit trail: a page load is many requests, but
	// one "this user reached this pod port" entry per window is what matters.
	auditWindow = time.Minute
)

var (
	proxySlots   = make(chan struct{}, maxConcurrentProxied)
	proxyAuditMu sync.Mutex
	proxyAudited = map[string]time.Time{}
)

// parseProxyTarget reads "8080" or "https:8443".
func parseProxyTarget(target string) (scheme string, port int, ok bool) {
	scheme = "http"
	if s, p, found := strings.Cut(target, ":"); found {
		if s != "http" && s != "https" {
			return "", 0, false
		}
		scheme, target = s, p
	}
	port, err := strconv.Atoi(target)
	if err != nil || port < 1 || port > 65535 {
		return "", 0, false
	}
	return scheme, port, true
}

func proxyAuditDue(key string) bool {
	proxyAuditMu.Lock()
	defer proxyAuditMu.Unlock()
	now := time.Now()
	for k, t := range proxyAudited {
		if now.Sub(t) > auditWindow {
			delete(proxyAudited, k)
		}
	}
	if _, seen := proxyAudited[key]; seen {
		return false
	}
	proxyAudited[key] = now
	return true
}

// PortForwardRedirect adds the trailing slash so a bare
// /proxy/{cluster}/{ns}/{pod}/{port} URL resolves relative links correctly.
func (h *Handler) PortForwardRedirect(w http.ResponseWriter, r *http.Request) {
	// Only ever to this route's own path plus a slash: it must stay a path on
	// this host (a "//host/..." value would be read as another site).
	if !strings.HasPrefix(r.URL.Path, "/proxy/") {
		http.NotFound(w, r)
		return
	}
	http.Redirect(w, r, r.URL.Path+"/", http.StatusPermanentRedirect) // #nosec G710 -- checked to be this route's own /proxy/ path above
}

// PortForwardProxy handles /proxy/{cluster}/{namespace}/{pod}/{target}/*.
// It must sit behind authn.Manager.Middleware.
func (h *Handler) PortForwardProxy(w http.ResponseWriter, r *http.Request) {
	clusterName := chi.URLParam(r, "cluster")
	namespace := chi.URLParam(r, "namespace")
	pod := chi.URLParam(r, "pod")
	scheme, port, ok := parseProxyTarget(chi.URLParam(r, "target"))
	if !ok {
		http.Error(w, "port must be a number 1-65535, optionally prefixed http: or https:", http.StatusBadRequest)
		return
	}
	if !k8sNameRe.MatchString(namespace) || !k8sNameRe.MatchString(pod) {
		http.Error(w, "invalid namespace or pod name", http.StatusBadRequest)
		return
	}

	entry, found := h.Clusters.Get(clusterName)
	if !found || entry == nil || entry.Clientset == nil {
		http.Error(w, "unknown cluster", http.StatusBadRequest)
		return
	}
	sess, authed := authn.FromContext(r.Context())
	if !authed {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	cfg := entry.RESTConfig
	if h.Auth.Mode != authn.ModeDisabled {
		cfg = entry.RESTConfigForToken(sess.K8sToken)
	}
	clientset, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		http.Error(w, "failed to build kubernetes client: "+err.Error(), http.StatusInternalServerError)
		return
	}

	select {
	case proxySlots <- struct{}{}:
		defer func() { <-proxySlots }()
	default:
		http.Error(w, "too many concurrent proxied requests — try again shortly", http.StatusServiceUnavailable)
		return
	}

	prefix := fmt.Sprintf("/proxy/%s/%s/%s/%s", url.PathEscape(clusterName), namespace, pod, chi.URLParam(r, "target"))
	rest := "/" + chi.URLParam(r, "*")
	hostHeader := "localhost:" + strconv.Itoa(port)

	transport := &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			conn, err := k8s.DialPodPort(ctx, clientset, cfg, namespace, pod, port)
			if err == nil && proxyAuditDue(sess.Username+"|"+clusterName+"|"+namespace+"/"+pod+"|"+strconv.Itoa(port)) {
				h.auditLog(r, "portforward", namespace, pod, "port", port, "scheme", scheme)
			}
			return conn, err
		},
		DisableKeepAlives:     true,
		ResponseHeaderTimeout: proxyResponseTimeout,
		// Pods commonly serve self-signed certificates. The hop to the pod is
		// already inside the authenticated Kubernetes tunnel, so there is no
		// meaningful chain to verify here.
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, // #nosec G402 -- pods serve self-signed certs; the connection is a tunnel through the Kubernetes API
	}

	proxy := &httputil.ReverseProxy{
		Transport: transport,
		Rewrite: func(pr *httputil.ProxyRequest) {
			pr.Out.URL.Scheme = scheme
			pr.Out.URL.Host = hostHeader
			pr.Out.URL.Path = rest
			pr.Out.URL.RawPath = ""
			pr.Out.Host = hostHeader
			pr.Out.Header.Del("Cookie")
			pr.Out.Header.Del("Authorization")
			pr.Out.Header.Set("X-Forwarded-Prefix", prefix)
		},
		ModifyResponse: func(resp *http.Response) error {
			resp.Header.Del("Set-Cookie")
			// Added, not replaced: a CSP the app sets itself stays enforced too.
			resp.Header.Add("Content-Security-Policy", "sandbox allow-scripts allow-forms allow-popups allow-modals allow-downloads")
			resp.Header.Set("X-Content-Type-Options", "nosniff")
			resp.Header.Set("Referrer-Policy", "no-referrer")
			if loc := resp.Header.Get("Location"); loc != "" {
				resp.Header.Set("Location", rewriteProxyLocation(loc, prefix, hostHeader))
			}
			return nil
		},
		ErrorHandler: func(w http.ResponseWriter, _ *http.Request, err error) {
			status, msg := classifyProxyError(err, namespace, pod, port)
			slog.Warn("portforward proxy request failed", "namespace", namespace, "pod", pod, "port", port, "error", err)
			http.Error(w, msg, status)
		},
	}
	proxy.ServeHTTP(w, r)
}

// rewriteProxyLocation keeps a redirect inside the proxy: a root-relative
// Location, or an absolute one pointing back at the pod, gets the proxy
// prefix; anything else (a genuinely external URL) is left alone.
func rewriteProxyLocation(loc, prefix, host string) string {
	if strings.HasPrefix(loc, "/") && !strings.HasPrefix(loc, "//") {
		return prefix + loc
	}
	if u, err := url.Parse(loc); err == nil && u.Host == host {
		return prefix + u.RequestURI()
	}
	return loc
}

func classifyProxyError(err error, namespace, pod string, port int) (int, string) {
	var statusErr *apierrors.StatusError
	switch {
	case errors.As(err, &statusErr) && apierrors.IsForbidden(err):
		return http.StatusForbidden, "Not allowed — " + statusErr.Status().Message
	case errors.As(err, &statusErr) && apierrors.IsNotFound(err):
		return http.StatusNotFound, fmt.Sprintf("pod %s/%s not found", namespace, pod)
	}
	msg := strings.ToLower(err.Error())
	switch {
	case strings.Contains(msg, "forbidden"):
		return http.StatusForbidden, "Not allowed — " + err.Error()
	case strings.Contains(msg, "not found"):
		return http.StatusNotFound, fmt.Sprintf("pod %s/%s not found", namespace, pod)
	case strings.Contains(msg, "connection refused"):
		return http.StatusBadGateway, fmt.Sprintf("nothing is listening on port %d inside pod %s/%s", port, namespace, pod)
	case errors.Is(err, context.Canceled):
		return 499, "client closed the request"
	}
	return http.StatusBadGateway, "proxy error: " + err.Error()
}
