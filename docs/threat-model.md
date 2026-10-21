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
| Compromised Vinaire pod | No Secrets access and no write verbs for its own ServiceAccount in token/OIDC mode; read-only root filesystem; registry sealed at rest; key optionally in Vault | chart `clusterrole.yaml`, `internal/clusters/crypto.go` | `security.md` #3, #11, I1 |

## Where authorisation is decided

- **Kubernetes RBAC, as the caller** — every route under `/api/v1` except the sign-in flow. The route list is
  [api-routes.md](./api-routes.md); the rights each feature needs are the chart's roles
  (`viewer`, `installer`, `node-admin`, `rbac-admin`, `lifecycle-admin`, `admin`).
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
