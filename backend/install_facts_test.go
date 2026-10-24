package main

import (
	"strings"
	"testing"

	"github.com/f1nn17/vinaire/backend/internal/config"
)

func TestInstallationFactsReadTheSettingsAndTheChartsHints(t *testing.T) {
	cfg := config.Config{AuthMode: "oidc", CookieSecure: false, SessionStore: "memory"}
	t.Setenv("OIDC_ALLOW_HTTP_PRIVATE_NETWORK", "true")
	t.Setenv("VAULT_ALLOW_INSECURE", "true")
	t.Setenv("INSTALL_AUDIT_PERSISTENT", "false")
	t.Setenv("INSTALL_NETWORK_POLICY", "true")
	i := installationFacts(cfg)
	if i.AuthMode != "oidc" || i.CookieSecure || i.SessionStore != "memory" || !i.HTTPKeycloakAllowed {
		t.Fatalf("%+v", i)
	}
	if !i.Chart || i.AuditOnVolume || !i.NetworkPolicy {
		t.Fatalf("chart hints: %+v", i)
	}
	if strings.Join(i.DevOptOuts, ",") != "VAULT_ALLOW_INSECURE=true" {
		t.Fatalf("%v", i.DevOptOuts)
	}
}

func TestWithoutTheChartsHintsTheChartFactsAreNotKnown(t *testing.T) {
	t.Setenv("INSTALL_AUDIT_PERSISTENT", "")
	t.Setenv("INSTALL_NETWORK_POLICY", "true") // meaningless alone
	if i := installationFacts(config.Config{}); i.Chart || i.NetworkPolicy {
		t.Fatalf("%+v", i)
	}
}
