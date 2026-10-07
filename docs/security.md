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
| 10 | **Dangerous actions** each run as the caller *or* pass an explicit permission check, and are audited. exec (start **and** end of every session — added by this audit; it was not audited before), YAML apply, Helm install/upgrade/rollback/uninstall, SSH provisioning, scale/delete, Secret reveal and port-forward all run as the caller. **Actions on Vinaire itself** — add/remove a cluster, change its type, import or discover one, GitOps mode and sources, SSH cluster create/import — additionally need a permission on `clusters.vinaire.io` / `gitops.vinaire.io`, asked of the cluster with a `SelfSubjectAccessReview` as the caller (see "Granting administrators" below). | `internal/api/vinaire_authz.go` | Test: every one of the 12 gated handlers answers `403` to an ordinary user and `401` to no session and passes for a granted administrator; one right doesn't imply another. **Live:** a scoped ServiceAccount got `403` adding a cluster; a cluster-admin got past the gate. |
| 11 | **Vinaire's own ServiceAccount** has no wildcard rule and is not cluster-admin; in token/OIDC mode it has **no Secrets access and no write verbs** — only what the informers read, plus `get` on one namespace and one ConfigMap by name. | `charts/vinaire/templates/clusterrole.yaml` | **Live:** `kubectl auth can-i --list --as=system:serviceaccount:<ns>:<name>` shows only get/list/watch on six kinds; `get secrets -A`, `delete pods -A`, `patch deployments -A`, `create pods -A`, `delete namespaces` all `no`. The CI job repeats it. |
| 12 | **Dependencies and repository secrets:** `govulncheck` (calls into a known-vulnerable dependency), `gosec` (medium+, every accepted finding annotated `#nosec <rule> -- <why>`), `npm audit`, `gitleaks` over the whole history, Trivy on the built image, a weekly schedule, Dependabot. | `.github/workflows/security.yml`, `.gitleaks.toml`, `.github/dependabot.yml` | **Run during the audit:** govulncheck — 0 reachable vulnerabilities; gosec — clean at medium+ after fixes and annotations; gitleaks — 4 findings, all test fixtures (one was a real, long-expired kubeadm token from a throwaway test container; replaced in the tests, its string allow-listed because it is in history); npm audit — 2 low advisories (below). Trivy and ZAP are configured but were **not run locally**. |
| 13 | **A Marketplace install can't exceed the caller's rights, and says so before it starts.** Helm runs with the caller's own `rest.Config`; a preflight renders the chart client-side (nothing is written) and asks the cluster, with a `SelfSubjectAccessReview` as the caller, whether they may `get`/`create` every kind in it. An install they can't finish is refused (audited `helm-install-refused`). | `internal/helm/preflight.go`, `jobs.go`, `internal/api/catalog.go` | Test (`preflight_test.go`, `jobs_test.go`) + live: a viewer-only token got 7 missing rights for PostgreSQL (incl. `networkpolicies`), a cluster-admin token got none, 123 objects were checked for kube-prometheus-stack |
| 14 | **Background installs are private to whoever started them**: the job id is unguessable and is only returned to the same identity (`Session.OwnerKey`); per-identity and global caps; a job can be cancelled only before Helm starts creating objects; the outcome is audited like a direct install. | `internal/helm/jobs.go`, `internal/api/catalog.go` | Test (`TestJobsAreScopedToTheirOwnerAndLimited`) + live: a request without the session got 401 for another identity's job |
| 15 | **Chart icons can't be used to reach internal hosts or run script.** The browser never fetches them (the CSP allows images from Vinaire's origin only); the backend fetches the URL the repository's index names for that chart through the guarded client (https only, no loopback/link-local), caps it at 256 KiB, accepts only a sniffed raster image or a script-free SVG, and serves it `nosniff` and sandboxed. | `internal/helm/icons.go`, `internal/api/catalog.go` | Test (`icons_test.go`: scheme/userinfo rules, content sniffing, one fetch cached) |
| 16 | **Deprecated charts are not installed by accident**: hidden from search, refused by every install path unless the request carries `allowDeprecated`. | `internal/helm/catalog.go`, `releases.go`, `preflight.go` | Test (`deprecated_test.go`) |
| 17 | **The audit log is readable only with an explicit permission, and only from its own directory.** `GET /api/v1/audit` needs `get` on the virtual resource `audit.vinaire.io` (asked of the cluster as the caller); the reader opens only the configured `AUDIT_LOG_PATH`'s own file and rotated copies, with caps on files, bytes and records. | `internal/api/audit_view.go`, `internal/audit/query.go` | Test (gate test lists the handler; `query_test.go`) + live: an account without the right got 403, the file held no tokens |
| 18 | **Log search is as private as `kubectl logs`.** The history search reads Loki (which holds every pod's logs and can't tell who asks), so before each query the caller is checked for `get pods/log` on that pod with a SelfSubjectAccessReview; namespace and pod name are validated before they reach a LogQL selector. Loki itself is reached through the API server's service proxy as the caller (`services/proxy` for Loki's own names only). | `internal/api/observability.go`, `internal/api/logging.go` | Test (`TestLogSearchRequiresPermissionToReadThatPodsLogs`: Loki is not even contacted for a caller without the right) + live: a pod's history returned 284 lines; an account without the right to reach Loki got a readable refusal |
| F1 | **Frontend:** `dangerouslySetInnerHTML`, `innerHTML` assignment, `insertAdjacentHTML`, `eval` and `new Function` are lint errors; web storage is a lint error everywhere except the one file that remembers *which cluster* was selected (a name, not a secret); credentials in the SSH and token forms are cleared from state right after submit; a revealed Secret value is hidden again after 30 s, when the tab is hidden, and on leaving the page. | `frontend/eslint.config.mjs`, `secrets/page.tsx` | Lint run proves the rules fire on a probe file; the real code base has none. |
| I1 | **Container:** non-root (uid 65532), `readOnlyRootFilesystem`, all capabilities dropped, `allowPrivilegeEscalation: false`, `RuntimeDefault` seccomp, explicit requests/limits, writable `/audit` and `/tmp` only; the OIDC client secret is read from a Secret, not a plain env value. | `charts/vinaire` | **Live:** the Linux binary run in a container with exactly those settings (`--read-only --user 65532 --cap-drop ALL --security-opt no-new-privileges`) served the UI and API, synced its informers, and wrote an audited action; the chart renders and is accepted by the API server (server-side dry-run) in token, disabled and OIDC configurations. |

## Granting administrators

Changing Vinaire itself is not a Kubernetes action, so no Kubernetes rule covers it. Instead
Vinaire asks the cluster whether the signed-in user may `create`/`update`/`delete` the
*virtual* resources `clusters.vinaire.io` and `gitops.vinaire.io` (they don't exist in the
cluster; they are only names for the permission), on its default cluster. The chart creates
the roles, unbound:

```bash
# browse and act (the people who use Vinaire)
kubectl create clusterrolebinding alice-viewer --clusterrole=<fullname>-viewer --user=alice
# change Vinaire itself (whoever administers it)
kubectl create clusterrolebinding alice-admin  --clusterrole=<fullname>-admin  --user=alice
```

`AUTH_MODE=disabled` (a disposable local cluster, no login at all) skips the check.

## Operating it safely

- **Don't expose Vinaire directly to the internet.** Reach it over a VPN (WireGuard, NetBird,
  Tailscale). If it must be public, only behind HTTPS with a real certificate (cert-manager),
  `cookie.secure: true`, and ideally behind an additional authenticating proxy.
- Set `trustForwardedFor: true` **only** when a proxy you control sets `X-Forwarded-For`;
  otherwise the login throttle counts the proxy's address for everyone.
- Keep `CLUSTER_STORE_KEY` out of the repository and the image; rotate it by setting the new
  key and moving the old one to `CLUSTER_STORE_KEY_PREVIOUS`, then restart once.
- Grant the `-admin` role to as few people as possible: it can register a cluster whose
  credentials Vinaire will then hold.
- Turn on `networkPolicy.enabled` if your CNI enforces it (inbound only from the ingress,
  outbound only to DNS, 443/6443 and — if you provision over SSH — 22, never the metadata
  address), and ship the container's stdout to your log system: the durable audit file lives
  on an `emptyDir` and is lost when the pod is replaced.
- `PROVISION_ALLOW_INSECURE` and `GITOPS_ALLOW_INSECURE_GIT` exist only for local development.

## Known residual risks

- **One credential store.** Compromising Vinaire (or its `CLUSTER_STORE_KEY`) exposes every
  registered cluster. This is inherent to a multi-cluster console; mitigate with the points above.
- **Sessions are in memory.** A restart signs everyone out and Vinaire can't run as more than one
  replica without a shared store. A session holds the user's real Kubernetes token for up to 12 h.
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
- **Not verified locally:** the Trivy image scan, the OWASP ZAP baseline, and NetworkPolicy
  *enforcement* (the policy renders and is accepted by the API server). They are in the `Security`
  workflow. Argo CD mode was exercised against a stand-in CRD only.
- **History:** the git history contains one expired kubeadm bootstrap token from a throwaway test
  container (commit `1bef880`). It is dead (kubeadm tokens last 24 h and that cluster no longer exists).

## Recommended before exposing Vinaire outside a trusted network

An independent manual review of: login and sessions (`internal/authn`), exec (`internal/stream/exec.go`),
Secret reveal, YAML apply, SSH provisioning (`internal/provision`) and kubeconfig/registry storage
(`internal/clusters`) — the places where a logic flaw would matter most and no scanner looks.
