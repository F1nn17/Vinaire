package securitychecks

// Roadmap Phase 86: checks about THIS installation of Vinaire — how it was configured — rather than about
// the cluster it looks at. They read nothing from a cluster: the facts come from Vinaire's own settings
// (and, for what only the Helm chart knows, from variables the chart sets). Each is a warning with the
// reason and the way to change it; none changes a default, blocks anything or writes anything.

// Installation is what Vinaire knows about its own installation.
type Installation struct {
	AuthMode     string // "token", "oidc" or "disabled"
	CookieSecure bool
	SessionStore string // "memory" or "secret"

	// Chart: the Helm chart said how it was installed (INSTALL_* variables), so the next two fields are known.
	Chart         bool
	AuditOnVolume bool // the audit log is on a persistent volume
	NetworkPolicy bool // the chart's NetworkPolicy is on

	HTTPKeycloakAllowed bool     // OIDC_ALLOW_HTTP_PRIVATE_NETWORK
	DevOptOuts          []string // names of the development-only switches that are on
	StoreUnencrypted    bool     // the cluster registry is kept unencrypted
}

func installationCheck(test func(i *Installation) (detail string, flagged bool)) func(*Snapshot) []Finding {
	return func(s *Snapshot) []Finding {
		if s.Installation == nil {
			return nil
		}
		if detail, flagged := test(s.Installation); flagged {
			return []Finding{{Kind: "Installation", Name: "vinaire", Detail: detail}}
		}
		return nil
	}
}

func init() {
	Checks = append(Checks,
		Check{
			ID: "inst-auth-disabled", Category: "This installation", Severity: High,
			Title:       "Sign-in is switched off",
			Description: "With AUTH_MODE=disabled nobody signs in and every request uses Vinaire's own credentials, so anyone who can reach the address can do what Vinaire can.",
			Remediation: "Set authMode to token or oidc in the chart's values (disabled is for a local test cluster only).",
			Needs:       []string{"installation"},
			Run: installationCheck(func(i *Installation) (string, bool) {
				return "authMode=disabled", i.AuthMode == "disabled"
			}),
		},
		Check{
			ID: "inst-dev-optouts", Category: "This installation", Severity: High,
			Title:       "A development-only switch is on",
			Description: "One of the switches that turn off a safety check for local development is set (plain http for git, Keycloak, SSH credentials or Vault). They are never meant for a real deployment.",
			Remediation: "Remove the variable named below from the deployment's environment.",
			Needs:       []string{"installation"},
			Run: func(s *Snapshot) []Finding {
				if s.Installation == nil {
					return nil
				}
				var out []Finding
				for _, n := range s.Installation.DevOptOuts {
					out = append(out, Finding{Kind: "Installation", Name: "vinaire", Detail: n})
				}
				return out
			},
		},
		Check{
			ID: "inst-store-unencrypted", Category: "This installation", Severity: High,
			Title:       "The cluster registry is stored unencrypted",
			Description: "Vinaire keeps the credentials of the clusters it manages in a Secret. Without a key it cannot seal them, so anyone who can read that Secret reads the credentials.",
			Remediation: "Let the chart manage the key Secret (rbac.clusterManagement.create=true, the default), or set CLUSTER_STORE_KEY, or use a key provider (keyProvider.type=vault).",
			Needs:       []string{"installation"},
			Run: installationCheck(func(i *Installation) (string, bool) {
				return "the registry's Secret is not encrypted", i.StoreUnencrypted
			}),
		},
		Check{
			ID: "inst-cookie-insecure", Category: "This installation", Severity: Medium,
			Title:       "The session cookie may travel over plain http",
			Description: "cookie.secure=false lets the browser send the session cookie without TLS, where anyone on the path can read it. It is only right for a home server reached over a private address without a certificate.",
			Remediation: "Put Vinaire behind HTTPS and set cookie.secure=true (the default).",
			Needs:       []string{"installation"},
			Run: installationCheck(func(i *Installation) (string, bool) {
				return "cookie.secure=false", !i.CookieSecure && i.AuthMode != "disabled"
			}),
		},
		Check{
			ID: "inst-http-keycloak", Category: "This installation", Severity: Medium,
			Title:       "Keycloak may be reached over plain http",
			Description: "keycloak.allowHttpPrivateNetwork lets Vinaire talk to Keycloak, and an administrator type the Keycloak password, over http on a private network. Anyone on that network can read it.",
			Remediation: "Give Keycloak a certificate and an https address, then set keycloak.allowHttpPrivateNetwork=false (the default).",
			Needs:       []string{"installation"},
			Run: installationCheck(func(i *Installation) (string, bool) {
				return "OIDC_ALLOW_HTTP_PRIVATE_NETWORK=true", i.HTTPKeycloakAllowed
			}),
		},
		Check{
			ID: "inst-audit-ephemeral", Category: "This installation", Severity: Medium,
			Title:       "The audit log is lost when the pod restarts",
			Description: "The audit file is kept in the pod's own temporary storage. Every action is also a line in the container's log, but if nothing collects that log, the history goes with the pod.",
			Remediation: "Turn on audit.persistence.enabled in the chart's values, or ship the container's stdout to your log system.",
			Needs:       []string{"installation", "installation-chart"},
			Run: installationCheck(func(i *Installation) (string, bool) {
				return "audit.persistence.enabled=false", !i.AuditOnVolume
			}),
		},
		Check{
			ID: "inst-session-memory", Category: "This installation", Severity: Low,
			Title:       "Sessions live in the pod's memory",
			Description: "With the memory session store everyone is signed out whenever Vinaire restarts, and a second replica could not share the sessions.",
			Remediation: "Set sessions.store=secret (or auto) in the chart's values; the sessions are then kept sealed in a Secret.",
			Needs:       []string{"installation"},
			Run: installationCheck(func(i *Installation) (string, bool) {
				return "sessions.store=memory", i.SessionStore == "memory" && i.AuthMode != "disabled"
			}),
		},
		Check{
			ID: "inst-no-networkpolicy", Category: "This installation", Severity: Low,
			Title:       "Vinaire has no NetworkPolicy of its own",
			Description: "Without one, any pod in the cluster can reach Vinaire and Vinaire can reach anything. Vinaire holds credentials and can run commands in pods, so it is worth limiting.",
			Remediation: "Turn on networkPolicy.enabled in the chart's values (your CNI must enforce NetworkPolicy) and list who may reach Vinaire under networkPolicy.ingressFrom.",
			Needs:       []string{"installation", "installation-chart"},
			Run: installationCheck(func(i *Installation) (string, bool) {
				return "networkPolicy.enabled=false", !i.NetworkPolicy
			}),
		},
	)
}
