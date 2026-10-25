# Vinaire — threat model

Material for an independent review of authorisation and sessions (Roadmap Phase 78). It states what is
protected, who may attack it, where the trust boundaries are, and which control answers which threat — each
with the place in the code and the test that keeps it true. [security.md](./security.md) holds the verified
properties and the residual risks; [api-routes.md](./api-routes.md) is the generated list of every route.
The rules a change must keep are the checklist in [AGENTS.md](../AGENTS.md).

## What is protected

| Asset | Why it matters |
|---|---|
| The Kubernetes bearer token of a signed-in user (held in a server-side session) | It is the user's whole authority over their clusters. |
| The cluster registry: server URLs and credentials of every registered cluster | One store holds the means to reach many clusters. |
| Vinaire's own keys: the registry's data key, the OIDC client secret, GitOps tokens | They unseal the above. |
| Secret values of browsed clusters | Only the audited `reveal` returns them. |
| The data of applications Vinaire installs and backs up | Dumps are copies of a database. |
| The audit log | The record of who did what. |

## Actors

- **Anonymous network client** — can reach the listener, nothing more.
- **Signed-in user** — a real Kubernetes identity; may do exactly what the cluster's RBAC allows them.
- **Malicious or compromised user** — tries to exceed their RBAC through Vinaire (confused deputy).
- **Malicious web page** — in a signed-in user's browser; tries CSRF, cross-origin WebSocket, framing.
- **Hostile cluster data** — names, labels, annotations, events, logs and chart text written by anyone with
  write access to a browsed cluster; rendered in Vinaire's UI and parsed by its backend.
- **Hostile remote content** — chart repositories, registries, git hosts, icons, Keycloak responses.
- **Operator** — configures Vinaire (environment, chart values, catalog, backup targets); trusted, but a typo
  must not become a hole.
- **Compromised Vinaire pod** — the assumed worst case the design limits.

## Trust boundaries

1. Browser ⇄ Vinaire: cookie session, CSP, `OriginGuard`, rate limits.
2. Vinaire ⇄ browsed cluster: **always the caller's own `rest.Config`** (`resolveRESTConfig`); Vinaire's own
   identity only for the documented informers and its own namespace.
3. Vinaire ⇄ outside world (Helm repositories, OCI, git, registries, icons, S3 endpoints, Keycloak): one guarded
   HTTP client (`internal/netguard`); targets come from operator configuration, never from a request.
4. Vinaire ⇄ nodes over SSH: host key pinned, credentials in memory only.
5. Pods Vinaire starts (dump, restore, install helpers) ⇄ the instance's Secret: referenced, never read.

## Threats and controls

| Threat | Control | Where | Kept true by |
|---|---|---|---|
| Use Vinaire to do more than the user's RBAC allows | Every call uses the caller's identity; no app-level permission layer to bypass | `resolveRESTConfig`, `callerConfig` | codebase rule tests; integration suite |
| Stolen / guessed session | 256-bit random id, `HttpOnly` `Secure` `SameSite=Strict`, new id at login, expiry, revocation list | `internal/authn` | `security.md` #6, #21 |
| CSRF, cross-origin WebSocket | Cross-origin or `null` `Origin` refused on state-changing routes; every upgrader checks origin | `origin.go`, `originChecker` | `TestEveryStateChangingRouteIsRateLimited`, codebase rule tests |
| Brute force / write flood | Login throttle; per-identity write limiter on every state-changing route | `ratelimit.go`, `router.go` | `router_test.go` |
| Credential leak into logs, errors, audit | Credentials wrapped in types that print `[REDACTED]`; request logger records no body/query/headers | `Secret` types, `requestLogger` | canary test (`security_test.go`) |
| Escalation through role bindings | Kubernetes' own escalation check applies (calls are the caller's); Vinaire never creates a Role | `rbac_actions.go` | `rbac_actions_test.go` |
| Arbitrary code via a chart / YAML | Helm and apply run as the caller; the caller's RBAC bounds the result; preflight says what is missing first | `internal/helm`, `apply.go` | `security.md` #13 |
| SSRF through a repository, icon, registry, S3 or git URL | Operator-configured targets; guarded dialer (no loopback / link-local / multicast, checked on the connected address); https only | `internal/netguard`, `backup.Config.Validate` | `Fuzz*` of the parsers; `TestInsecureSkipVerifyOnlyWhereReviewed` |
| Command injection through a typed value | Fixed scripts and templates; values pass as validated environment variables or single-quoted parts | `internal/backup`, `internal/provision` | `FuzzRequestValues`, `FuzzPlanUpgradeTag`, `FuzzLoadConfig` |
| Backup / restore / upgrade used to reach another image or workload | The image is the instance's own; only the tag may change; label and image repository must agree; a restore never targets its own source; delete needs Vinaire's label | `internal/backup`, `internal/instances/upgrade.go` | `backup_test.go`, `upgrade_test.go`, integration lifecycle suite |
| Database password exposed by a dump Job | `secretKeyRef` only — Vinaire never reads the Secret; pod runs non-root, no escalation, no capabilities | `backup.PodSpec` | `TestPVCDumpUsesSecretRefsAndNoPlainPassword` |
| Hostile cluster text rendered as script | Cluster data is rendered as text; `dangerouslySetInnerHTML`, `innerHTML`, `eval`, web storage are lint errors; strict CSP | `frontend/eslint.config.mjs`, `security_headers.go` | lint; `security.md` #8, F1 |
| Hostile cluster text breaking a parser | Bounded parsers with fuzz tests stating what they must never accept | `Fuzz*` tests | `go test -fuzz` runs |
| Reading the audit log to learn others' activity | Right `get` on `audit.vinaire.io`, asked of the cluster as the caller | `audit_view.go` | `audit_view_test.go` |
| Changing Vinaire's own configuration without being allowed | `requireVinaireRight` first, as the caller; a test lists every gated handler | `vinaire_authz.go` | `codebase_rules_test.go` |
| Lockout by turning off sign-in | Token login cannot be disabled while OIDC is off | `keycloak.go` | `keycloak_test.go` |
| Plain http used where it should not be (Keycloak, administrator's password) | https by default; http only when the operator sets `OIDC_ALLOW_HTTP_PRIVATE_NETWORK`, and then only to/from private-network addresses, decided on the connected address and the forwarding chain | `internal/netguard`, `internal/api/keycloak.go` | `netguard_test.go`, `keycloak_private_test.go` |
| An OIDC sign-in used to obtain more than the person has | The credential is a ServiceAccount of the person's own in one namespace; Vinaire never creates a binding, so rights come only from RBAC an administrator set; an account not made by Vinaire for this subject is never reused | `internal/oidcbroker`, chart Role | `broker_test.go`, `integration/broker_test.go` |
| The same OIDC subject from a different Keycloak or realm reaching a person's account | The identity is issuer + subject; the issuer is verified by the OIDC library against the configured one; accounts from before are claimed once, only for the pinned issuer | `internal/oidcbroker` | `TestTheSameSubjectUnderTwoIssuersIsTwoPeople`, `TestALegacyAccountIsNeverClaimedByAnotherIssuer` |
| A person disabled (or signed out) at the identity provider keeps working | Short credential + renewal through the refresh token; the provider's refusal ends the session, an unreachable provider is tolerated for an hour only; deleting the account ends it too; each call of a renewal has its own deadline and the refresh token the provider rotated is always saved, so a slow provider does not end a session early | `internal/authn/renew.go` | `renew_test.go`, live run |
| Two people with the same display name (in different realms, or after a rename) see each other's background jobs | A job's owner is a hash of issuer + subject (of the token for a pasted one), never the name | `Session.OwnerKey` | `ownerkey_test.go` |
| A moving or hijacked CI tool runs in the build that signs releases | Tools are installed at pinned versions, binaries checked against a sha256 kept in the repository; no pipe-to-shell, no `@latest` | `.github/tool-versions.env`, `.github/scripts/install-tools.sh` | `TestWorkflowsInstallNothingUnpinned` |
| A weak setting nobody noticed (sign-in off, a development switch on, an unsealed registry…) | Settings → Security lists them with the reason and the chart value that changes them; unknown is never "pass" | `internal/securitychecks/installation.go` | `installation_test.go` |
| A client claims https with a forged `X-Forwarded-Proto` to get a credential accepted over plain http | **Not closed yet.** The credential is still checked by the cluster; keep Vinaire reachable only through the proxy. Trusted-proxy list: Roadmap Phase 96 | `transportIsSecure` in `internal/api/provision.go` | — (see security.md, residual risks) |
| Two replicas renew one session at once and one presents a spent refresh token, which the provider treats as theft | A lease in the shared session Secret (taken with a version check, lapsing by itself): only the holder presents the refresh token, the others wait for its result; if the lease can't be taken nothing is rotated | `internal/authn/renew.go` (`renewCoordinated`), `shared.go`, `internal/clusters/sessions_kv.go` | `renew_shared_test.go` (two replicas on one store, a strict provider), `sessions_lease_test.go` |
| An http Keycloak that Kubernetes would refuse, silently leaving every call a 401 | The wizard refuses the API-server mode for an http Keycloak and does not print `--oidc-*` flags for it | `chooseIdentityMode` | `TestAnHTTPKeycloakIsNeverPaired…` |
| Compromised Vinaire pod | No Secrets access and no write verbs for its own ServiceAccount in token/OIDC mode; read-only root filesystem; registry sealed at rest; key optionally in Vault | chart `clusterrole.yaml`, `internal/clusters/crypto.go` | `security.md` #3, #11, I1 |

## Where authorisation is decided

- **Kubernetes RBAC, as the caller** — every route under `/api/v1` except the sign-in flow. The route list is
  [api-routes.md](./api-routes.md); the rights each feature needs are the chart's roles
  (`reader`, `viewer`, `installer`, `node-admin`, `rbac-admin`, `lifecycle-admin`, `admin`).
- **Vinaire's own state** — `requireVinaireRight` on the virtual resources `clusters.vinaire.io`,
  `gitops.vinaire.io`, `auth.vinaire.io`, `audit.vinaire.io`, again asked of the cluster as the caller.
- **Nothing else.** There is no second permission system; adding one is a decision to raise first.

## What an independent review should look at first

1. `internal/authn` — session creation, rotation, storage (memory and the sealed shared Secret), logout, expiry.
2. `resolveRESTConfig` and its callers — any handler that reaches for `entry.Clientset` instead of the caller's
   config is a finding. `go vet` plus the rule tests cover the known shapes, not a new one.
3. The writes in AGENTS.md's list — each is one call, one audit entry, one validated input.
4. `internal/netguard` — the single dialer everything outside is fetched through.
5. The Phase 77 additions: `internal/backup`, `internal/instances`, `internal/api/backups*.go`,
   `instance_upgrade.go`.

## Re-check against the AGENTS.md checklist (Phase 78, Phase 77 code)

| Checklist item | Result |
|---|---|
| Never log, return or store a credential in the clear | Backup/restore/upgrade audit entries carry kind, target name, schedule, image tags — no credential; the S3 credentials are a Secret *name*; the database password is a `secretKeyRef`. |
| Check the caller's permissions before returning data | Every new handler uses `resolveCluster` / `resolveDynamic` (the caller's config); no `entry.Clientset`. |
| Validate any input that reaches a path, URL, command or query | Tags, schedules, sizes, file names, claim and snapshot names, secret names: strict patterns, fuzzed. S3 endpoints: operator configuration only. |
| Audit-log every mutating action | `backup-create`, `backup-delete`, `backup-restore`, `backup-restore-snapshot`, `instance-upgrade`, `instance-revert`. |
| Changes to Vinaire's own configuration call `requireVinaireRight` | None of the new routes changes Vinaire's own configuration. |
| New WebSocket endpoint | None. |
| Cluster data is untrusted input in the UI | Labels (instance name, app, version, images) are validated before being returned and rendered as text. |
| Don't weaken a guard | None changed. |
| New routes named, rate-limited, inventory regenerated, bodies bounded | Routes in `router.go`, state-changing ones behind the write limiter, `docs/api-routes.md` regenerated, `http.MaxBytesReader` on every body. |
| A test that fails if the property is lost; `govulncheck`, `gosec`, `npm audit` | Unit, fuzz and the integration lifecycle suite; `govulncheck` (go.mod now requires Go 1.27.3) and `gosec` clean. |

An external audit remains the owner's decision; this document and the rest of `docs/` are its starting
material.
