package main

import (
	"strings"
	"testing"

	"github.com/f1nn17/vinaire/backend/internal/config"
	"github.com/f1nn17/vinaire/backend/internal/trustedproxy"
)

func TestInstallationFactsReadTheSettingsAndTheChartsHints(t *testing.T) {
	cfg := config.Config{AuthMode: "oidc", CookieSecure: false, SessionStore: "memory"}
	t.Setenv("OIDC_ALLOW_HTTP_PRIVATE_NETWORK", "true")
	t.Setenv("VAULT_ALLOW_INSECURE", "true")
	t.Setenv("INSTALL_AUDIT_PERSISTENT", "false")
	t.Setenv("INSTALL_NETWORK_POLICY", "true")
	i := installationFacts(cfg, trustedproxy.Set{})
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
	if i := installationFacts(config.Config{}, trustedproxy.Set{}); i.Chart || i.NetworkPolicy {
		t.Fatalf("%+v", i)
	}
}

func TestWideTrustedProxyRangesAreReportedButAProxysOwnAddressIsNot(t *testing.T) {
	set, _, err := trustedproxy.Parse("10.0.0.0/8, 192.168.88.10/32")
	if err != nil {
		t.Fatal(err)
	}
	i := installationFacts(config.Config{}, set)
	if strings.Join(i.BroadTrustedProxies, ",") != "10.0.0.0/8" {
		t.Fatalf("%v", i.BroadTrustedProxies)
	}
}
