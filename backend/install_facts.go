package main

import (
	"os"

	"github.com/f1nn17/vinaire/backend/internal/config"
	"github.com/f1nn17/vinaire/backend/internal/securitychecks"
)

// devOptOuts are the switches that turn a safety check off for local development; none is meant for a real
// deployment (AGENTS.md names each). The "This installation" security checks warn when one is on.
var devOptOuts = []string{
	"PROVISION_ALLOW_INSECURE",
	"OIDC_ALLOW_INSECURE",
	"GITOPS_ALLOW_INSECURE_GIT",
	"VAULT_ALLOW_INSECURE",
}

// installationFacts is what Vinaire says about its own installation (Roadmap Phase 86). The Helm chart sets
// INSTALL_AUDIT_PERSISTENT and INSTALL_NETWORK_POLICY ("true"/"false") — pure information for the checks, not
// settings: without them the two facts they carry are "not known".
func installationFacts(cfg config.Config) *securitychecks.Installation {
	inst := &securitychecks.Installation{
		AuthMode:            cfg.AuthMode,
		CookieSecure:        cfg.CookieSecure,
		SessionStore:        cfg.SessionStore,
		HTTPKeycloakAllowed: os.Getenv("OIDC_ALLOW_HTTP_PRIVATE_NETWORK") == "true",
	}
	for _, name := range devOptOuts {
		if os.Getenv(name) == "true" {
			inst.DevOptOuts = append(inst.DevOptOuts, name+"=true")
		}
	}
	if v := os.Getenv("INSTALL_AUDIT_PERSISTENT"); v != "" {
		inst.Chart = true
		inst.AuditOnVolume = v == "true"
		inst.NetworkPolicy = os.Getenv("INSTALL_NETWORK_POLICY") == "true"
	}
	return inst
}
