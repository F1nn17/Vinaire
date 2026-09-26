// Package config centralizes environment-driven configuration for the
// backend, so every other package reads settings from a Config value
// instead of calling os.Getenv directly.
package config

import (
	"os"
	"strings"
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
	// to call this API from a browser.
	CORSAllowedOrigins []string

	// OIDCIssuerURL is the Keycloak realm's issuer URL used for both token
	// verification here and the frontend's login redirect (Roadmap
	// Phase 10). See /keycloak at the repo root for the dev instance this
	// points at by default.
	OIDCIssuerURL string
	// OIDCClientID is the public OIDC client every verified token must
	// have been issued for.
	OIDCClientID string

	// PolicyPath points at the YAML file mapping OIDC groups to allowed
	// namespaces/actions (see internal/authz.LoadPolicy and
	// backend/policy.yaml).
	PolicyPath string

	// ClusterContexts lists the kubeconfig context names Vinaire is
	// configured to talk to (Roadmap Phase 11). Empty means single-cluster
	// mode: use KubeconfigPath/in-cluster config exactly as before Phase 11
	// existed.
	ClusterContexts []string
}

const (
	defaultPort          = "8080"
	defaultCORSOrigin    = "http://localhost:3100"
	defaultOIDCIssuerURL = "http://localhost:8180/realms/vinaire-dev"
	defaultOIDCClientID  = "vinaire-frontend"
	defaultPolicyPath    = "policy.yaml"
	envPort              = "PORT"
	envKubeconfig        = "KUBECONFIG"
	envCORSAllowedOrigin = "CORS_ALLOWED_ORIGINS"
	envOIDCIssuerURL     = "OIDC_ISSUER_URL"
	envOIDCClientID      = "OIDC_CLIENT_ID"
	envPolicyPath        = "POLICY_PATH"
	envClusterContexts   = "CLUSTER_CONTEXTS"
)

// Load reads configuration from the environment, falling back to defaults
// that work out of the box for local development.
func Load() Config {
	return Config{
		Port:               getEnv(envPort, defaultPort),
		KubeconfigPath:     os.Getenv(envKubeconfig),
		CORSAllowedOrigins: splitCSV(getEnv(envCORSAllowedOrigin, defaultCORSOrigin)),
		OIDCIssuerURL:      getEnv(envOIDCIssuerURL, defaultOIDCIssuerURL),
		OIDCClientID:       getEnv(envOIDCClientID, defaultOIDCClientID),
		PolicyPath:         getEnv(envPolicyPath, defaultPolicyPath),
		ClusterContexts:    splitCSV(os.Getenv(envClusterContexts)),
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
