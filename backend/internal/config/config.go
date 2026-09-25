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
}

const (
	defaultPort          = "8080"
	defaultCORSOrigin    = "http://localhost:3100"
	envPort              = "PORT"
	envKubeconfig        = "KUBECONFIG"
	envCORSAllowedOrigin = "CORS_ALLOWED_ORIGINS"
)

// Load reads configuration from the environment, falling back to defaults
// that work out of the box for local development.
func Load() Config {
	return Config{
		Port:               getEnv(envPort, defaultPort),
		KubeconfigPath:     os.Getenv(envKubeconfig),
		CORSAllowedOrigins: splitCSV(getEnv(envCORSAllowedOrigin, defaultCORSOrigin)),
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
