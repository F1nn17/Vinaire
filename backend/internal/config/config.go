// Package config centralizes environment-driven configuration for the
// backend, so every other package reads settings from a Config value
// instead of calling os.Getenv directly.
package config

import (
	"fmt"
	"net/url"
	"os"
	"strings"
	"time"
)

// Config holds all environment-driven settings for the backend.
type Config struct {
	// Port the HTTP server listens on, e.g. "8080".
	Port string

	// KubeconfigPath overrides the kubeconfig file used to build the
	// Kubernetes client. Empty means: try in-cluster config first, then
	// fall back to ~/.kube/config (see internal/k8s.NewClientset).
	KubeconfigPath string

	// CORSAllowedOrigins are the exact origins (scheme+host+port) allowed
	// to call this API from a browser. Only matters when the frontend
	// isn't being reached same-origin (e.g. `next dev`'s rewrite proxy) —
	// see AGENTS.md.
	CORSAllowedOrigins []string

	// FrontendURL is where the browser is redirected back to after a
	// successful OIDC login (Roadmap Phase 10).
	FrontendURL string

	// AuthMode selects the active internal/authn.Manager mode: "token"
	// (default — paste a Kubernetes ServiceAccount token), "oidc" (log in
	// via Keycloak/any OIDC provider), or "disabled" (no login, local k3d
	// dev only — every request uses the backend's own credentials).
	AuthMode string
	// CookieSecure sets the session cookie's Secure flag.
	// CookieSameSite sets its SameSite attribute ("none", "lax", or
	// "strict"). These default to "true"/"none": the frontend build here is
	// a static export with no server of its own (see AGENTS.md's "Rendering
	// model"), so it always calls this backend cross-origin, in dev and in
	// most deployments alike — a cross-origin fetch can only carry the
	// cookie at all with SameSite=None, and browsers require Secure
	// whenever SameSite=None is set (they treat `http://localhost`
	// specifically as a secure context, so this still works without real
	// TLS for local dev). Set COOKIE_SAMESITE=strict (and, once actually
	// served over TLS, leave COOKIE_SECURE=true) for a same-origin
	// deployment — e.g. the single combined image (Roadmap Phase 10) with a
	// TLS-terminating ingress in front of it.
	CookieSecure   bool
	CookieSameSite string

	// OIDCIssuerURL/OIDCClientID/OIDCClientSecret/OIDCRedirectURL configure
	// AuthMode "oidc" 's Authorization Code + PKCE flow. See /keycloak at
	// the repo root for the dev instance these point at by default.
	OIDCIssuerURL    string
	OIDCClientID     string
	OIDCClientSecret string
	OIDCRedirectURL  string

	// StaticDir, if set and the directory exists, is served as the
	// frontend's static export (Roadmap Phase 10's single combined image —
	// see AGENTS.md's "Rendering model"). Empty by default so `go run .`
	// during local dev, with no frontend build present, behaves exactly as
	// before — no static serving at all.
	StaticDir string

	// ClusterContexts lists the kubeconfig context names Vinaire is
	// configured to talk to (Roadmap Phase 11). Empty means single-cluster
	// mode: use KubeconfigPath/in-cluster config exactly as before Phase 11
	// existed.
	ClusterContexts []string

	// ClusterName overrides the display name of the single cluster used in
	// single-cluster mode (ClusterContexts empty). Kubernetes clusters have
	// no self-reported name the API exposes, and running in-cluster (the
	// common case for a real deployment) means there's no kubeconfig
	// context name to fall back on either — so this is the only way to
	// show something meaningful instead of the literal word "default" when
	// running in-cluster. Ignored when ClusterContexts is set (each of
	// those already has a real, distinct context name).
	ClusterName string

	// PrometheusURL/LokiURL/AlertmanagerURL point at an already-running
	// observability stack (Roadmap Phase 16) — Vinaire never installs or
	// manages these itself, it only proxies to them if configured. Empty
	// means "not configured": every endpoint that needs one responds with
	// an explicit `{"available": false, ...}` instead of erroring, the
	// same pattern Phase 3 (metrics-server) and Phase 8 (Gateway API) use
	// for an optional cluster capability that may not exist.
	PrometheusURL   string
	LokiURL         string
	AlertmanagerURL string

	// AuditLogPath is where Phase 9 write actions and Phase 6 secret
	// reveals are durably logged (Roadmap Phase 17), in addition to the
	// structured stdout slog line they already write. Relative to the
	// working directory by default — for this to actually "survive a pod
	// restart" (this phase's own Definition of done) in a real Kubernetes
	// Deployment, mount a volume at whatever directory this path lives in.
	AuditLogPath string

	// DataDir (VINAIRE_DATA_DIR) is where an out-of-cluster Vinaire — typically
	// on a home server — keeps its encrypted registry of added clusters
	// (Roadmap Phase 34). Empty means "nowhere to persist": cluster
	// management is then unavailable outside a cluster, as in Phase 24.
	DataDir string

	// ClusterStoreKey (CLUSTER_STORE_KEY) is the base64 32-byte AES-256 key
	// the cluster registry is sealed with. If unset, a key is generated and
	// kept in a Secret (in-cluster) or a 0600 file in DataDir.
	ClusterStoreKey string
	// ClusterStorePrevious (CLUSTER_STORE_KEY_PREVIOUS, comma-separated) are
	// earlier keys the registry may still be sealed with: set the new key as
	// CLUSTER_STORE_KEY and the old one here, restart, and everything is
	// re-encrypted in place on the first load.
	ClusterStorePrevious []string

	// SessionMaxTTL (SESSION_MAX_TTL, a Go duration like 8h) caps how long a login
	// lasts however long its token lives; default 12h.
	SessionMaxTTL time.Duration
	// SessionStore (SESSION_STORE): "memory" (default; sessions live in this process and
	// are lost on a restart) or "secret" (sealed, in a Secret of Vinaire's own namespace,
	// shared by every replica and surviving restarts — Roadmap Phase 48; in-cluster only).
	SessionStore string
	// TrustForwardedFor (TRUST_FORWARDED_FOR=true): the login throttle counts the
	// first X-Forwarded-For address instead of the connection's peer. Set it only
	// behind a proxy you control that sets the header.
	TrustForwardedFor bool

	// DiscoveryKubeconfigPaths (DISCOVERY_KUBECONFIG_PATHS, comma-separated)
	// are extra kubeconfig files local discovery should look at, besides
	// $KUBECONFIG, ~/.kube/config and /etc/rancher/k3s/k3s.yaml.
	DiscoveryKubeconfigPaths []string
}

const (
	defaultPort             = "8080"
	defaultCORSOrigin       = "http://localhost:3100"
	defaultFrontendURL      = "http://localhost:3100"
	defaultAuthMode         = "token"
	defaultOIDCIssuerURL    = "http://localhost:8180/realms/vinaire-dev"
	defaultOIDCClientID     = "vinaire-frontend"
	defaultOIDCRedirectURL  = "http://localhost:8080/api/v1/auth/oidc/callback"
	envPort                 = "PORT"
	envKubeconfig           = "KUBECONFIG"
	envCORSAllowedOrigin    = "CORS_ALLOWED_ORIGINS"
	envFrontendURL          = "FRONTEND_URL"
	envAuthMode             = "AUTH_MODE"
	envCookieSecure         = "COOKIE_SECURE"
	envCookieSameSite       = "COOKIE_SAMESITE"
	defaultCookieSecure     = "true"
	defaultCookieSameSite   = "strict"
	envOIDCIssuerURL        = "OIDC_ISSUER_URL"
	envOIDCClientID         = "OIDC_CLIENT_ID"
	envOIDCClientSecret     = "OIDC_CLIENT_SECRET"
	envOIDCRedirectURL      = "OIDC_REDIRECT_URL"
	envStaticDir            = "STATIC_DIR"
	envClusterContexts      = "CLUSTER_CONTEXTS"
	envClusterName          = "CLUSTER_NAME"
	envPrometheusURL        = "PROMETHEUS_URL"
	envLokiURL              = "LOKI_URL"
	envAlertmanagerURL      = "ALERTMANAGER_URL"
	envAuditLogPath         = "AUDIT_LOG_PATH"
	defaultAuditLogPath     = "audit.log"
	envDataDir              = "VINAIRE_DATA_DIR"
	envClusterStorePrevious = "CLUSTER_STORE_KEY_PREVIOUS"
	envClusterStoreKey      = "CLUSTER_STORE_KEY"
	envDiscoveryPaths       = "DISCOVERY_KUBECONFIG_PATHS"
)

// Load reads configuration from the environment, falling back to defaults
// that work out of the box for local development.
func Load() Config {
	return Config{
		Port:               getEnv(envPort, defaultPort),
		KubeconfigPath:     os.Getenv(envKubeconfig),
		CORSAllowedOrigins: splitCSV(getEnv(envCORSAllowedOrigin, defaultCORSOrigin)),
		FrontendURL:        getEnv(envFrontendURL, defaultFrontendURL),
		AuthMode:           getEnv(envAuthMode, defaultAuthMode),
		CookieSecure:       getEnv(envCookieSecure, defaultCookieSecure) == "true",
		CookieSameSite:     getEnv(envCookieSameSite, defaultCookieSameSite),
		OIDCIssuerURL:      getEnv(envOIDCIssuerURL, defaultOIDCIssuerURL),
		OIDCClientID:       getEnv(envOIDCClientID, defaultOIDCClientID),
		OIDCClientSecret:   os.Getenv(envOIDCClientSecret),
		OIDCRedirectURL:    getEnv(envOIDCRedirectURL, defaultOIDCRedirectURL),
		StaticDir:          os.Getenv(envStaticDir),
		ClusterContexts:    splitCSV(os.Getenv(envClusterContexts)),
		ClusterName:        os.Getenv(envClusterName),
		PrometheusURL:      os.Getenv(envPrometheusURL),
		LokiURL:            os.Getenv(envLokiURL),
		AlertmanagerURL:    os.Getenv(envAlertmanagerURL),
		AuditLogPath:       getEnv(envAuditLogPath, defaultAuditLogPath),

		DataDir:                  os.Getenv(envDataDir),
		ClusterStoreKey:          os.Getenv(envClusterStoreKey),
		ClusterStorePrevious:     splitCSV(os.Getenv(envClusterStorePrevious)),
		SessionMaxTTL:            parseDuration(os.Getenv("SESSION_MAX_TTL")),
		SessionStore:             getEnv("SESSION_STORE", "memory"),
		TrustForwardedFor:        os.Getenv("TRUST_FORWARDED_FOR") == "true",
		DiscoveryKubeconfigPaths: splitCSV(os.Getenv(envDiscoveryPaths)),
	}
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// splitCSV splits a comma-separated env value into a trimmed, non-empty
// slice, e.g. "http://a,  http://b" -> ["http://a", "http://b"].
func splitCSV(s string) []string {
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

// parseDuration reads a Go duration, returning 0 (meaning "use the default") for
// empty or invalid input.
func parseDuration(s string) time.Duration {
	d, err := time.ParseDuration(strings.TrimSpace(s))
	if err != nil || d < 0 {
		return 0
	}
	return d
}

// ValidateCORSOrigins refuses an allow-list that would let any website call the
// API with the user's cookie: a wildcard, a missing scheme or host, or a path.
// CORS_ALLOWED_ORIGINS must be exact origins (scheme://host[:port]).
func ValidateCORSOrigins(origins []string) error {
	for _, o := range origins {
		if strings.Contains(o, "*") {
			return fmt.Errorf("CORS_ALLOWED_ORIGINS may not contain a wildcard (%q): list exact origins, because the API is used with cookies", o)
		}
		u, err := url.Parse(o)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || (u.Path != "" && u.Path != "/") || u.RawQuery != "" {
			return fmt.Errorf("CORS_ALLOWED_ORIGINS entry %q isn't an origin like https://vinaire.example.com", o)
		}
	}
	return nil
}
