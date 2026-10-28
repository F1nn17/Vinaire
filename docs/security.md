# Vinaire — security

**English** · [Русский](./security.ru.md)

This is the outcome of the Phase 36 audit: a short threat model, the properties that were
**checked** (and how, so you can check them again), and the risks that remain. It is a
set of claims that can be verified, not a guarantee that there are no vulnerabilities —
automated scanners don't find logic flaws, so the critical paths below should get an
independent manual review before Vinaire is reachable from outside a trusted network.

## What Vinaire is, security-wise

Vinaire is a web UI in front of one or more Kubernetes clusters. Almost everything it
does runs **as the signed-in user**: the user's own Kubernetes token (a pasted
ServiceAccount token, or an OIDC identity) is held server-side behind an opaque session
cookie and used for every call, so the cluster's RBAC — not Vinaire — decides what they
can see and do. The exceptions are deliberate and listed in `AGENTS.md`:

- the live-update informers read pods, deployments, statefulsets, daemonsets, jobs and
  nodes with Vinaire's *own* ServiceAccount (and each event is then checked against the
  connected user's RBAC before it is sent);
- Vinaire's own configuration — its registry of clusters and the GitOps sources — which
  holds credentials, is sealed at rest, and is changed only with a permission the cluster
  grants (below);
- native GitOps sync, which runs when nobody is logged in and therefore applies as a
  ServiceAccount whose token the user supplied for that source.

**Because Vinaire holds the credentials of every cluster registered in it, it is a single
point with access to all of them.** Treat it like a bastion host.

### Assets

| Asset | Where it lives | Protection |
|---|---|---|
| A user's Kubernetes token | server memory, for the life of the session (≤ 12 h) | opaque 256-bit session id in an HttpOnly+Secure+SameSite=Strict cookie; never in a log, URL or response |
| Registered clusters' credentials (tokens, kubeconfigs) | a Secret (in-cluster) or a file (`VINAIRE_DATA_DIR`) | AES-256-GCM; key from `CLUSTER_STORE_KEY` or a separate Secret/0600 file; rotatable |
| GitOps git/ServiceAccount tokens | the same sealed store | same; refused if the store is not encrypted |
| SSH passwords/keys for provisioning | memory, for the duration of one job | wrapped in `Secret` (prints `[REDACTED]`), scrubbed from every log line, never on disk |
| Secret values | the cluster | only through the audited `reveal` endpoint, as the caller |

### Who could attack, and from where

| Actor | What they could try | Main defences |
|---|---|---|
| Anyone on the network | guess a login, flood sessions, scan | token validated against the cluster before a session exists; login throttle (per address and per token, with backoff); session cap |
| A logged-in, low-privilege user | see other namespaces; add a cluster; use Vinaire's identity | every call runs as them; stream filtered per event; changes to Vinaire itself need `vinaire.io` permission |
| A malicious website the user visits | CSRF; opening a WebSocket with the user's cookie | SameSite=Strict, `Origin`/`Sec-Fetch-Site` check on every state-changing request and every WebSocket handshake |
| Content from the cluster (pod names, labels, event text, log lines) | XSS | rendered as text only; ESLint forbids HTML sinks; CSP refuses inline script |
| A hostile Helm repository / git repository / registry | install something malicious; SSRF; huge downloads | repositories only from operator config; chart digest verified; guarded dialler; size and count limits |
| A compromised Vinaire pod | read other namespaces' Secrets; write | minimal ServiceAccount (no Secrets, no writes), hardened container, NetworkPolicy (optional) |

## Verified properties

Each row says what is claimed, where it is enforced, and how it was checked. "Test" means
`go test ./...` in `backend/` (also run by the `Security` workflow); "live" means checked
against a real k3d cluster during the audit.

| # | Property | Enforced in | Checked by |
|---|---|---|---|
| 1 | **A user sees only what their RBAC lets them list — also in the live stream**, although the informers watch the whole cluster. Each event is checked with a `SelfSubjectAccessReview` as that user (cached 10 s; fails closed). | `internal/stream/authz.go` | Test: a token that may list pods only in `team-a` receives only that namespace's events over a real WebSocket. **Live:** a ServiceAccount scoped to one namespace got `403` from every REST endpoint tried (pods, deployments, topology, nodes, namespaces) for other namespaces and only that namespace's events on the stream; a cluster-admin token got all of them. |
| 1b | The optional metrics and Gateway API clients act as the caller (they used Vinaire's own credentials before the audit — a leak of Gateway/HTTPRoute names regardless of RBAC). | `internal/api/vinaire_authz.go` (`metricsFor`, `gatewayFor`) | Test: requests carry the caller's token, never the backend's. |
| 2 | The informer cache holds no Secret values (it holds no Secrets at all: pods, deployments, statefulsets, daemonsets, jobs, nodes). Decoded Secret values leave only through the audited `reveal` endpoint, as the caller. | `internal/stream/informers.go`, `internal/api/secrets.go` | Code review of what the informers register; the reveal endpoint is audited as `reveal_secret`. |
| 3 | The cluster registry is encrypted at rest (AES-256-GCM); a wrong key fails to decrypt instead of returning garbage; **the key can be rotated** (new key as `CLUSTER_STORE_KEY`, the old one in `CLUSTER_STORE_KEY_PREVIOUS`; everything is re-encrypted in place on the first load). A kubeconfig never appears in a log or an error response. | `internal/clusters/crypto.go`, `store.go` | Test: rotation re-encrypts the registry and documents, the old key no longer opens them, a storage dump holds no secret in the clear. Canary test (see 5) covers kubeconfig errors. |
| 4 | SSH host keys are always verified against a fingerprint the user confirmed; no `InsecureIgnoreHostKey` anywhere; credentials never reach a log; commands run remotely are fixed templates with strictly validated, single-quoted input; Kubernetes/CNI versions come only from the backend's own config. | `internal/provision` | Test: wrong host key aborts before login; shell metacharacters rejected; **codebase rule test** `TestNoInsecureIgnoreHostKey` fails the build if `InsecureIgnoreHostKey` appears; `TestInsecureSkipVerifyOnlyWhereReviewed` allow-lists the three places TLS verification is skipped. |
| 5 | **Logs carry no secrets.** The request logger records method, path, status and timing only — no headers, query string or body. | `main.go` `requestLogger` | **Canary test**: a recognisable marker is put in an `Authorization` header, a cookie, a query string, a request body, a login token and every credential field of a kubeconfig; it appears in no log line (captured at debug level) and in no error response. |
| 6 | **Sessions:** 256-bit random id (`crypto/rand`), `HttpOnly` + `Secure` + `SameSite=Strict` by default, a fresh id at every login and the previous session dropped, full invalidation at logout and at expiry (also purged by a background janitor), lifetime capped at 12 h whatever the token's own, store bounded. **Login throttle:** 5 failures per token / 20 per address per 10 min, then a block that doubles up to 15 min. A pasted token is validated against the cluster *before* a session exists. | `internal/authn` | Test (11 cases) and **live**: cookie flags read from a real browser context; the 6th bad login returned `429` with `Retry-After`. |
| 7 | **CSRF / WebSocket origin:** every state-changing request carrying a cross-origin `Origin` (or `null`) is refused with `403` before any handler; every WebSocket upgrader (live stream, logs, exec, provisioning) checks the handshake `Origin`. | `internal/api/origin.go`, `internal/stream/origin.go` | Test: table of origins; a test that fails the build if a `websocket.Upgrader` lacks the origin check; **live:** `curl` with a foreign `Origin` → `403`, same-origin → passes. |
| 8 | **Headers:** `Content-Security-Policy` (scripts allowed only by the SHA-256 of the page's own inline scripts — no `unsafe-inline`/`unsafe-eval`; connections only back to this host), `X-Frame-Options: DENY` + `frame-ancestors 'none'`, `nosniff`, `Referrer-Policy: no-referrer`, HSTS over HTTPS. CORS only from an explicit allow-list; a wildcard in `CORS_ALLOWED_ORIGINS` stops the server from starting. | `backend/security_headers.go`, `static.go`, `internal/config` | Test; **live in Chromium** (Playwright): every page and the Monaco editor (including its Web Workers) load with no violation, and an injected inline `<script>` / `onerror` handler is blocked. Also `frontend/e2e/security.spec.ts`, run in CI. |
| 9 | **SSRF / path traversal:** port-forward only reaches pods through the Kubernetes API; Helm/OCI/git fetches go through one guarded HTTP client (https only, no loopback/link-local/multicast, checked on the address actually dialled so DNS can't redirect it, no redirect out of https, size caps); a cluster's server URL is validated (https, not link-local) and every connection to a registered cluster uses the same dial guard; git paths and the static-file handler can't leave their directory. | `internal/netguard`, `internal/clusters`, `internal/gitops`, `backend/static.go` | Test: `..`, encoded and backslash traversal attempts, metadata addresses, oversize downloads; gosec path-traversal findings resolved or justified. |
| 10 | **Dangerous actions** each run as the caller *or* pass an explicit permission check, and are audited. exec (start **and** end of every session — added by this audit; it was not audited before), YAML apply, Helm install/upgrade/rollback/uninstall, SSH provisioning, scale/delete, Secret reveal and port-forward all run as the caller. **Actions on Vinaire itself** — add/remove a cluster, change its type, import or discover one, GitOps mode and sources, SSH cluster create/import — additionally need a permission on `clusters.vinaire.io` / `gitops.vinaire.io`, asked of the cluster with a `SelfSubjectAccessReview` as the caller (see "Granting administrators" below). | `internal/api/vinaire_authz.go` | Test: every one of the 20 gated handlers answers `403` to an ordinary user and `401` to no session and passes for a granted administrator; one right doesn't imply another. **Live:** a scoped ServiceAccount got `403` adding a cluster; a cluster-admin got past the gate. |
| 11 | **Vinaire's own ServiceAccount** has no wildcard rule and is not cluster-admin; in token/OIDC mode it has **no Secrets access and no write verbs** — only what the informers read, plus `get` on one namespace and one ConfigMap by name. | `charts/vinaire/templates/clusterrole.yaml` | **Live:** `kubectl auth can-i --list --as=system:serviceaccount:<ns>:<name>` shows only get/list/watch on six kinds; `get secrets -A`, `delete pods -A`, `patch deployments -A`, `create pods -A`, `delete namespaces` all `no`. The CI job repeats it. |
| 12 | **Dependencies and repository secrets:** `govulncheck` (calls into a known-vulnerable dependency), `gosec` (medium+, every accepted finding annotated `#nosec <rule> -- <why>`), `npm audit`, `gitleaks` over the whole history, Trivy on the built image, a weekly schedule, Dependabot. CI installs only pinned tools: k3d and Helm as release binaries checked against a sha256 kept in the repository, the Go scanners at a fixed version (`.github/tool-versions.env`, `.github/scripts/install-tools.sh`). | `.github/workflows/security.yml`, `.gitleaks.toml`, `.github/dependabot.yml` | **Run during the audit:** govulncheck — 0 reachable vulnerabilities; gosec — clean at medium+ after fixes and annotations; gitleaks — 4 findings, all test fixtures (one was a real, long-expired kubeadm token from a throwaway test container; replaced in the tests, its string allow-listed because it is in history); npm audit — 2 low advisories (below). Trivy and ZAP are configured but were **not run locally**. `TestWorkflowsInstallNothingUnpinned` fails the build if a workflow pipes a download into a shell, fetches from a branch or installs `@latest`. |
| 13 | **A Marketplace install can't exceed the caller's rights, and says so before it starts.** Helm runs with the caller's own `rest.Config`; a preflight renders the chart client-side (nothing is written) and asks the cluster, with a `SelfSubjectAccessReview` as the caller, whether they may `get`/`create` every kind in it. An install they can't finish is refused (audited `helm-install-refused`). | `internal/helm/preflight.go`, `jobs.go`, `internal/api/catalog.go` | Test (`preflight_test.go`, `jobs_test.go`) + live: a viewer-only token got 7 missing rights for PostgreSQL (incl. `networkpolicies`), a cluster-admin token got none, 123 objects were checked for kube-prometheus-stack |
| 14 | **Background installs are private to whoever started them**: the job id is unguessable and is only returned to the same identity (`Session.OwnerKey`: a hash of issuer + subject for a person who signed in through Keycloak, a hash of the token for a pasted one — never the display name, which two people in different realms can share); per-identity and global caps; a job can be cancelled only before Helm starts creating objects; the outcome is audited like a direct install. | `internal/helm/jobs.go`, `internal/api/catalog.go` | Test (`TestJobsAreScopedToTheirOwnerAndLimited`) + live: a request without the session got 401 for another identity's job |
| 15 | **Chart icons can't be used to reach internal hosts or run script.** The browser never fetches them (the CSP allows images from Vinaire's origin only); the backend fetches the URL the repository's index names for that chart through the guarded client (https only, no loopback/link-local), caps it at 256 KiB, accepts only a sniffed raster image or a script-free SVG, and serves it `nosniff` and sandboxed. | `internal/helm/icons.go`, `internal/api/catalog.go` | Test (`icons_test.go`: scheme/userinfo rules, content sniffing, one fetch cached) |
| 16 | **Deprecated charts are not installed by accident**: hidden from search, refused by every install path unless the request carries `allowDeprecated`. | `internal/helm/catalog.go`, `releases.go`, `preflight.go` | Test (`deprecated_test.go`) |
| 17 | **The audit log is readable only with an explicit permission, and only from its own directory.** `GET /api/v1/audit` needs `get` on the virtual resource `audit.vinaire.io` (asked of the cluster as the caller); the reader opens only the configured `AUDIT_LOG_PATH`'s own file and rotated copies, with caps on files, bytes and records. | `internal/api/audit_view.go`, `internal/audit/query.go` | Test (gate test lists the handler; `query_test.go`) + live: an account without the right got 403, the file held no tokens |
| 18 | **Log search is as private as `kubectl logs`.** The history search reads Loki (which holds every pod's logs and can't tell who asks), so before each query the caller is checked for `get pods/log` on that pod with a SelfSubjectAccessReview; namespace and pod name are validated before they reach a LogQL selector. Loki itself is reached through the API server's service proxy as the caller (`services/proxy` for Loki's own names only). | `internal/api/observability.go`, `internal/api/logging.go` | Test (`TestLogSearchRequiresPermissionToReadThatPodsLogs`: Loki is not even contacted for a caller without the right) + live: a pod's history returned 284 lines; an account without the right to reach Loki got a readable refusal |
| 19 | **Metrics are as private as `kubectl top`/the cluster's own RBAC.** With no `PROMETHEUS_URL`, Vinaire finds Prometheus (by Service labels) and queries it through the API server's service proxy **as the caller** — so a user needs `get services/proxy` for Prometheus's own Service names (the chart's `-viewer` grants it) and an account without it gets a readable refusal. The metrics-stack install is the same Helm path as the Marketplace (caller's rights, preflight, audit); the chart and version are pinned in the backend and a request can't name another. Before this, `/metrics/*` and the per-resource charts returned data to any signed-in user from the operator's `PROMETHEUS_URL`. | `internal/api/metricsstack.go`, `internal/metricsstack` | Test (`TestMetricsAreReachedAsTheCaller`: every request to the cluster carries the caller's token; `TestValuesAreValidYAML`; option validation) + live: Prometheus 29.36.0 installed from the UI, a pod's chart drawn through the proxy, `helm-install` audited |
| 20 | **Setting up Keycloak sign-in from the UI can't leak the administrator or lock everyone out.** The Keycloak administrator's password goes in a POST body over a secure transport only, is wrapped in `keycloak.Secret`, is used for one token request and is never stored, logged, audited or returned; the one thing kept — the new client's secret — is sealed in the registry's store, which **refuses to take it unless it is encrypted**. The Keycloak URL comes from the request, so every call (admin API, OIDC discovery, code exchange, keys) uses the guarded client (https, no loopback/link-local). The client is confidential, PKCE-only, with Vinaire's exact redirect URI. Pasting a token can only be turned off while OIDC is on, and "turn off" forgets Vinaire's side only. All four endpoints need a right on `auth.vinaire.io`. | `internal/keycloak`, `internal/api/keycloak.go`, `internal/authn` | Tests (`TestSecretNeverPrints`, `TestKeycloakSetupSwitchesSignInOnAndKeepsSecretsOutOfSight`, `TestKeycloakSetupRefusesWhatItShould`, `TestOIDCCanBeSwitchedOnAndOffAtRunTime`, gate test) + live against Keycloak 26: set up from the UI, signed in through Keycloak, survived a restart, no credential in any log |
| 21 | **Sessions shared between replicas hold no readable token and no usable cookie.** With `SESSION_STORE=secret` each session is one key of the Secret `vinaire-sessions`, sealed with the registry's key (AES-256-GCM); the key is the SHA-256 of the session id, so a copy of the Secret gives no one a cookie, and the store refuses to run without an encryption key. A login while the store is down fails (it doesn't fall back to memory silently). Revoking a session (`auth.vinaire.io`, audited `session-revoke`) reaches every replica within seconds, and is answered "ok" only once the deletion is saved in the Secret (otherwise `503`, and the session stays valid and listed; `TestRevokeFailsWhenSharedStoreCannotPersistDeletion`); the list shows a fingerprint, never the cookie or token. Only the elected replica runs the scheduled GitOps poll. | `internal/authn/shared.go`, `internal/clusters/sessions_kv.go`, `internal/leader`, `internal/api/sessions.go` | Tests (two replicas sharing logins, the stored values hold no token or id, store outage, cap, revoke across replicas, leader election, gated handlers) + live: two replicas in k3d, login on A accepted by B, both restarted without signing anyone out, one leader |
| 22 | **The store's master key can be kept outside the cluster, and the credentials of the clusters it holds can be rotated.** With `KEY_PROVIDER=vault` only the data key *wrapped* by a Vault/OpenBao Transit key is stored (a Secret or `store.key.wrapped`); the raw key exists in Vinaire's memory only, so reading every Secret of Vinaire's namespace gives an attacker a blob that is useless without Vault (which has its own access control and audit log). A provider that can't be reached, or a wrapped store opened without its provider, is a refusal to start — never a fresh key or an unencrypted store. The provider's address comes from operator configuration only. A cluster's bearer token is classified (never expires / expires within a week / expired) and can be replaced: the new token is verified with a real call, must belong to the same cluster, and the replacement is audited without the token. | `internal/keyprovider`, `internal/clusters/keyring.go`, `internal/api/clusters.go` | Tests (wrap/unwrap/rewrap, wrong token, Kubernetes auth cached, strict config, migration keeps the data, outage and wrong provider refuse, no raw key left, rotation replaces the token without echoing it) + live against Vault 1.18: migration, restart, Transit key rotation + rewrap, Vault stopped, no token in `clusters.enc` or the logs |
| 23 | **No route skips the session or the write rate limit, no handler reads an unbounded body, and the text parsers keep their promises.** The router is walked: every route except six named sign-in routes is behind the session middleware (and answers 401 without or with a junk cookie), every state-changing route is rate limited or named with a reason, `docs/api-routes.md` is generated from it; a rule test rejects a request body read without `http.MaxBytesReader`; 19 fuzz targets (kubeconfig, backup configuration, upgrade tags, account names, pod diagnosis, server URL, YAML, proxy target, node labels/taints, RBAC bindings, shell quoting, SSH-install validators, metrics options, Keycloak parameters) check what each accepts. Images are released signed (cosign, keyless) with an SBOM and provenance. | `backend/router.go`, `backend/router_test.go`, `backend/codebase_rules_test.go`, `*/fuzz_test.go`, `.github/workflows/release.yml` | Tests + fuzzing in CI (30 s per target); the first run of the route and body rules found six unlimited write routes and five unbounded bodies, all fixed |
| 24 | **Backups, restores and version changes of an installed application stay inside that application.** A dump runs in the instance's own image with a fixed script; values reach it only as validated environment variables; the database password is a `secretKeyRef` the pod reads (Vinaire never does); pods run non-root with no capabilities. An S3 target is named by the operator (https, no loopback/link-local), a request names only a target and a Secret. A restore goes into a *new* instance and never over its source; a version change keeps the running image's repository, needs an acknowledgement for a major change and refuses a lower one; only objects labelled as Vinaire's are deleted. All as the caller, all audited. | `internal/backup`, `internal/instances`, `internal/api/backups*.go`, `instance_upgrade.go` | Tests and fuzz tests (`FuzzRequestValues`, `FuzzLoadConfig`, `FuzzPlanUpgradeTag`, `FuzzPlanRevertAnnotation`); the lifecycle integration suite on a real cluster (install, pod recreated, dump, restore into a new instance, delete) |
| 25 | **Keycloak over plain http is possible only on a private network, and only if the operator asked for it.** `OIDC_ALLOW_HTTP_PRIVATE_NETWORK` (chart `keycloak.allowHttpPrivateNetwork`, off by default) lets Vinaire reach a Keycloak over http and accept its administrator's password over http, but the address connected to must be RFC 1918, 100.64.0.0/10 or IPv6 unique-local (checked in the dialer, so a name that resolves elsewhere is refused), an https → http redirect is never followed, and the password is accepted only from a client whose address and forwarding chain are private. No request can enable it. | `internal/netguard`, `internal/api/keycloak.go` | `TestIsPrivate`, `TestPlainHTTPToLoopbackIsRefused…`, `TestPlainHTTPToAPrivateAddress…`, `TestRedirectFromHTTPSToHTTPIsRefused`, `TestClientOnPrivateNetwork`, `TestKeycloakCredentialsNeedTLS…` |
| 26 | **Signing in through Keycloak never needs the API server to trust Keycloak, and never grants a right.** After Vinaire has verified the sign-in it gives the person a ServiceAccount of their own in one dedicated namespace and a short-lived token for it; the cluster's RBAC decides everything and a new account has none. Vinaire's Role is namespaced to that one namespace (ServiceAccounts get/list/create, their token create) — proven on a real cluster to be unable to create accounts elsewhere, read Secrets, list bindings or mint a token in `kube-system`. A person is **issuer + subject**: the same subject under another Keycloak or realm gets another account, and accounts made before that (keyed by the subject alone) are claimed once, only for the issuer Vinaire was set up with then. An account is reused only if it carries this identity's label and Vinaire's; deleting it ends its tokens in seconds. The wizard refuses to pair an http Keycloak with the API-server mode (Kubernetes accepts https issuers only). | `internal/oidcbroker`, `internal/authn/oidc.go`, `charts/vinaire/templates/users-namespace.yaml` | `broker_test.go` (unit and `integration/broker_test.go` on a real cluster), `FuzzAccountName`, `TestAnHTTPKeycloakIsNeverPaired…` |
| 27 | **A session through Keycloak is renewed only while Keycloak still vouches for the person, and is cut when it stops.** The Kubernetes credential is short (`sessionTokenTTL`, 30 min) and the session long; shortly before the credential ends, the refresh token (server-side only, sealed in the shared store) is presented to Keycloak: `invalid_grant` (disabled user, ended SSO session, revoked token) ends the session at once, a refreshed token for another subject ends it, an unreachable Keycloak is tolerated for one hour since it last said yes, and a renewal never creates an account — deleting one cuts a person off. One renewal per session at a time; each call of a renewal (to Keycloak, to Kubernetes) has its own deadline, and the refresh token Keycloak rotated is saved even when the Kubernetes credential could not be minted, so a slow Keycloak does not end a session early and a spent token is never presented twice. **Between replicas** a lease in the shared Secret (taken with a version check, lapsing by itself) makes one replica the only one that presents the refresh token — the others wait for its result — and when the lease can't be taken nothing is rotated. | `internal/authn/renew.go`, `oidcbroker.RenewToken` | `renew_test.go` (disabled person, unreachable provider and its grace, deleted account, wrong subject, concurrent requests, shared store sealing), `TestRenewalMintsForAnExistingAccountAndNeverCreatesOne`; a live run against a real Keycloak |
| 28 | **The installation says where it is weak, without guessing.** Settings → Security has a group "This installation": sign-in switched off, a development-only switch on, an unencrypted registry, `cookie.secure=false`, Keycloak over plain http, an audit log that doesn't survive a restart, sessions in memory, no NetworkPolicy — each with the reason and the chart value that changes it. The facts are Vinaire's own settings and two informational variables the chart sets (`INSTALL_AUDIT_PERSISTENT`, `INSTALL_NETWORK_POLICY`); what isn't known is `unknown`, never `pass`; nothing is blocked, written or changed by default. | `internal/securitychecks/installation.go`, `internal/api/securitychecks.go` | `installation_test.go` (a hardened install passes every check, each weakness is reported with its reason, unknown stays unknown), `securitychecks_install_test.go` |
| 29 | **A credential is accepted over plain http only from a proxy the operator named.** An SSH password, a token, the Keycloak administrator's password and the like are accepted when the connection is TLS, or arrives from an address in `trustedProxies` (`TRUSTED_PROXIES`) that says `X-Forwarded-Proto: https` (the last entry of the header, which the nearest proxy wrote), or is loopback to loopback (local development), or the operator set the development opt-out. **The default list is empty**, so a forged header from any client counts for nothing; `0.0.0.0/0` stops Vinaire from starting, a range wider than a /24 (IPv4) or /64 (IPv6) is logged and shown in Settings → Security. The refusal names the address Vinaire saw, so the operator knows what to list. (Strict-Transport-Security is still sent on the header alone: a browser ignores it over http.) | `internal/trustedproxy`, `internal/api/provision.go`, `main.go` | `trustedproxy_test.go` (nobody by default, only the listed address, IPv6 and mapped addresses, a client can't prepend `https`), `FuzzParseTrustedProxies`, `TestKeycloakCredentialsNeedTLS…`, `installation_test.go` |
| F1 | **Frontend:** `dangerouslySetInnerHTML`, `innerHTML` assignment, `insertAdjacentHTML`, `eval` and `new Function` are lint errors; web storage is a lint error everywhere except the one file that remembers *which cluster* was selected (a name, not a secret); credentials in the SSH and token forms are cleared from state right after submit; a revealed Secret value is hidden again after 30 s, when the tab is hidden, and on leaving the page. | `frontend/eslint.config.mjs`, `secrets/page.tsx` | Lint run proves the rules fire on a probe file; the real code base has none. |
| I1 | **Container:** non-root (uid 65532), `readOnlyRootFilesystem`, all capabilities dropped, `allowPrivilegeEscalation: false`, `RuntimeDefault` seccomp, explicit requests/limits, writable `/audit` and `/tmp` only; the OIDC client secret is read from a Secret, not a plain env value. | `charts/vinaire` | **Live:** the Linux binary run in a container with exactly those settings (`--read-only --user 65532 --cap-drop ALL --security-opt no-new-privileges`) served the UI and API, synced its informers, and wrote an audited action; the chart renders and is accepted by the API server (server-side dry-run) in token, disabled and OIDC configurations. |

## Granting administrators

Changing Vinaire itself is not a Kubernetes action, so no Kubernetes rule covers it. Instead
Vinaire asks the cluster whether the signed-in user may `create`/`update`/`delete` the
*virtual* resources `clusters.vinaire.io` and `gitops.vinaire.io` (they don't exist in the
cluster; they are only names for the permission), on its default cluster. The chart creates
the roles, unbound:

```bash
# look, don't touch (no Secrets, no shell in pods, nothing that changes)
kubectl create clusterrolebinding alice-reader --clusterrole=<fullname>-reader --user=alice
# use Vinaire fully — NOT read-only: reads Secrets, opens a shell in pods, deletes workloads and namespaces
kubectl create clusterrolebinding alice-viewer --clusterrole=<fullname>-viewer --user=alice
# change Vinaire itself (whoever administers it)
kubectl create clusterrolebinding alice-admin  --clusterrole=<fullname>-admin  --user=alice
```

`AUTH_MODE=disabled` (a disposable local cluster, no login at all) skips the check.

## Operating it safely

- **Don't expose Vinaire directly to the internet.** Reach it over a VPN (WireGuard, NetBird,
  Tailscale). If it must be public, only behind HTTPS with a real certificate (cert-manager),
  `cookie.secure: true`, and ideally behind an additional authenticating proxy.
- Set `trustForwardedFor: true` **only** when a proxy you control sets `X-Forwarded-For`, and name that proxy under
  `trustedProxies`: the header is read from the right through the listed proxies and ignored from every other
  address, and with `trustedProxies` empty it is ignored altogether (the throttle then counts the proxy's address for
  everyone — "This installation" warns about it). Kept true by `TestClientIPIsReadFromTheRightThroughNamedProxies`.
- Keep `CLUSTER_STORE_KEY` out of the repository and the image; rotate it by setting the new
  key and moving the old one to `CLUSTER_STORE_KEY_PREVIOUS`, then restart once.
- Grant the `-admin` role to as few people as possible: it can register a cluster whose
  credentials Vinaire will then hold.
- With `keycloak.allowHttpPrivateNetwork` and `networkPolicy.enabled` the chart opens TCP port 80 to **all** of 10.0.0.0/8,
  172.16.0.0/12 and 192.168.0.0/16 so Vinaire can reach a Keycloak on http - not only the Keycloak. For the stricter setting
  use `networkPolicy.httpKeycloakEgress: false` and name the Keycloak under `networkPolicy.extraEgress`. A policy with an empty
  `ingressFrom` lets any source in ("This installation" warns). Rendering of both cases is kept by
  `TestTheNetworkPolicyOpensPort80ToPrivateRangesOnlyForAnHTTPKeycloak`; a Keycloak inside the cluster behind a Service is not verified.
- Turn on `networkPolicy.enabled` if your CNI enforces it (inbound only from the ingress,
  outbound only to DNS, 443/6443 and — if you provision over SSH — 22, never the metadata
  address), and keep the audit log: either set `audit.persistence.enabled: true` (a volume for the
  file, see "Keeping the audit log" below) or ship the container's stdout to your log system — by
  default the audit file lives on an `emptyDir` and is lost when the pod is replaced.
- `PROVISION_ALLOW_INSECURE` and `GITOPS_ALLOW_INSECURE_GIT` exist only for local development.

### Defaults to review for a permanent install

None of these is a mistake — each is a default that suits a first try — but each is worth a decision before Vinaire holds anything you care about. Settings → Security → "This installation" lists the ones that apply to *your* installation.

| Setting | Default | What it means | Change it with |
|---|---|---|---|
| Audit log on a volume | off | the log lives on an `emptyDir` and is lost when the pod is replaced | `audit.persistence.enabled: true`, or ship stdout to your log system |
| NetworkPolicy | off | any pod in the cluster may reach Vinaire; Vinaire may reach anything | `networkPolicy.enabled: true` on a CNI that enforces it |
| Sessions | in memory (one replica) | a restart signs everyone out | `sessions.store: secret` (the default for more than one replica) |
| `cookie.secure` | `true` | right for https; over plain http a browser drops the cookie | leave it; `false` only for a trial or an http home server |
| Keycloak over http | off | Keycloak and its administrator's password stay on https | `keycloak.allowHttpPrivateNetwork: true` — private networks only |
| Signed-in lifetime with Keycloak | session 12 h with the chart (`sessionMaxTTL`), credential 30 min (`sessionTokenTTL`) renewed while Keycloak vouches for the person | a person disabled in Keycloak (or whose Keycloak session ended) loses access at the next renewal — within one credential lifetime; Keycloak being down is tolerated for an hour, then sessions end | `sessionMaxTTL`, `sessionTokenTTL` (Go durations; shorter is safer) |
| Reverse proxy that ends TLS | `trustedProxies` empty | a password, token or SSH key sent through it is refused until the proxy is named | `trustedProxies: [<the proxy's own address>/32]` — never a whole network |
| Keycloak groups → roles | not supported, by decision (Phase 87, see [decisions](decisions.md)) | a ServiceAccount token carries no groups, and mapping them would make Vinaire grant rights on its own authority | bind roles per account, or use https and the API server's own OIDC |

## Keeping the audit log

Every audited action is written twice: as a JSON line to the audit file (`AUDIT_LOG_PATH`, `/audit/audit.log`, rotated at
10 MB, five files kept; the Audit log page reads it) and as a log line on the container's stdout.

- **The file** lives on an `emptyDir` by default: it survives a restart of the container and is lost when the pod is
  replaced. `audit.persistence.enabled: true` puts it on a PersistentVolumeClaim instead (`audit.persistence.size`,
  `storageClass`, or your own claim in `existingClaim`). The volume is ReadWriteOnce with one writer, so this needs
  `replicaCount: 1`, the Deployment then uses the `Recreate` strategy, and the claim is **kept** on `helm uninstall` (it is
  the record of what was done — delete it yourself to remove it).
- **stdout** is what a log collector (Promtail/Grafana Alloy, Fluent Bit, a node agent) reads by default, and it works with
  several replicas. Each audited action is a line with the message `audited action` and the fields `action`, `namespace`,
  `name`, `identity`, `remoteAddr` and the action's detail — never a secret value. In Loki, for example:
  `{namespace="vinaire"} |= "audited action"`; to keep only what was changed, filter on `action`. Send it to a place the
  people whose actions are logged cannot edit.

The two are not exclusive: persist the file for the Audit log page and ship stdout for retention and alerting.

## Known residual risks

- **One credential store.** Compromising Vinaire exposes every registered cluster. This is inherent to a
  multi-cluster console; mitigate with the points above, keep the master key in Vault (`keyProvider.type: vault`),
  give clusters short-lived tokens and replace them before they run out.
- **Sessions are in memory by default.** With one replica a restart signs everyone out. Set `sessions.store: secret`
  (the default for more than one replica) to keep them sealed in a shared Secret. A session holds the user's real
  Kubernetes token for up to 12 h either way; the login throttle and running install jobs stay per replica.
- **TLS verification is skipped in three reviewed places:** a port-forwarded HTTPS pod (its
  certificate is usually self-signed; the connection is a tunnel through the Kubernetes API), the
  identity probe of a locally discovered cluster (nothing is sent), and a cluster added with a
  token but no CA certificate (the UI says so and asks).
- **CSP keeps `style-src 'unsafe-inline'`** because the UI sets style attributes and Monaco injects
  styles. Scripts are hash-restricted.
- **`npm audit` reports two low advisories** on DOMPurify bundled inside `monaco-editor` (the fix
  needs a breaking major upgrade). The editors only ever show the user's own YAML; they don't render
  untrusted markdown.
- **The stream filter is per kind and namespace**, using `list` permission. RBAC finer than that
  (`resourceNames`, label selectors) isn't evaluated for a row; a revoked permission takes up to 10 s
  to take effect on an open stream.
- **Anyone who may read a Secret in the cluster may reveal it** through Vinaire (audited). Vinaire adds
  no restriction beyond RBAC.
- **SSH provisioning runs commands as root on a server by design.** It is gated (create `nodes`, and
  `clusters.vinaire.io` for cluster create/import), HTTPS-only, host-key-pinned and audited.
- **Dumps are copies of the data, unencrypted.** A dump written to a volume claim is readable by anyone who may mount that claim; an upload to S3 is protected by https and the store's own access control, not by Vinaire. Encrypt the store, or the volume, if the data needs it. The upload image is pinned by tag, not by digest.
- **Plain http on a private network is a deliberate opt-in for home servers.** With `keycloak.allowHttpPrivateNetwork` the Keycloak traffic and the administrator's password can cross the LAN or the overlay network (NetBird) unencrypted. Anyone who can sniff that network sees them. Use TLS wherever the network is not yours alone; the setting refuses public addresses, it cannot make a private network trustworthy. The session cookie needs `cookie.secure=false` over http, which also stops browsers protecting it.
- **Vinaire can issue a token for any person it has an account for.** Its Role in the users' namespace lets it mint a token for every ServiceAccount there — including an administrator who isn't signed in. Today Vinaire only holds tokens of people who are. A compromised Vinaire therefore reaches everything those accounts may do; the limits are the short token lifetime, the audit (`oidc-token-issued`), and that it can't widen any account's rights. Keep other ServiceAccounts out of that namespace. Whoever administers Keycloak is trusted (they can create a user with a chosen subject); with an http Keycloak the keys it signs with travel unprotected on the private network.
- **Renewal depends on Keycloak being reachable.** Within an hour of its last confirmation a session carries on without it; after that sessions end until Keycloak answers. Back-channel logout (Keycloak telling Vinaire at once) is not implemented: a disabled person is cut at the next renewal, i.e. within one credential lifetime (30 min by default).
- **Keycloak groups are not carried over** in the ServiceAccount mode — a ServiceAccount token has no groups, so roles are bound per account.
- **A replica that dies in the middle of a renewal can cost a person their session.** Renewals between replicas are kept apart by a lease that lapses by itself after about 40 seconds, but if the replica dies after Keycloak has rotated the refresh token and before it saved it, the new token is lost and the next renewal ends the session — the person signs in again. Nobody gains access from it.
- **Not verified locally:** the Trivy image scan, the OWASP ZAP baseline, and NetworkPolicy
  *enforcement* (the policy renders and is accepted by the API server). They are in the `Security`
  workflow. Argo CD mode was exercised against a stand-in CRD only.
- **History:** the git history contains one expired kubeadm bootstrap token from a throwaway test
  container (commit `1bef880`). It is dead (kubeadm tokens last 24 h and that cluster no longer exists).

## Recommended before exposing Vinaire outside a trusted network

An independent manual review of: login and sessions (`internal/authn`), exec (`internal/stream/exec.go`),
Secret reveal, YAML apply, SSH provisioning (`internal/provision`) and kubeconfig/registry storage
(`internal/clusters`) — the places where a logic flaw would matter most and no scanner looks.
