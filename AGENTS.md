# AGENTS.md

Instructions for AI coding agents (Claude Code and similar) working in this
repository.

## The core rule: writes are narrow, explicit, and always audited

Vinaire's UI defaults to **read-only** against the Kubernetes API. As of
Roadmap Phase 9, a specific, deliberately narrow set of write actions is
the exception, implemented as **direct client-go calls** — not a GitOps-PR
flow.

> This supersedes an earlier version of this rule, which required every
> write to go through a pull request against a GitOps manifests repository.
> That requirement was dropped for Phase 9's actual scope, as an explicit
> decision by the project owner — see ROADMAP.md's Phase 9 section for the
> history. This file is the source of truth for the current rule; anything
> elsewhere (old comments, prior discussion) describing the GitOps-PR
> version as current is stale.

**What's actually allowed to write, today** — and nothing beyond this list:
- `POST /api/v1/deployments/{namespace}/{name}/restart` — annotation patch
  (`kubectl rollout restart`'s mechanism).
- `DELETE /api/v1/deployments/{namespace}/{name}` — a plain `Delete()`.
- `POST /api/v1/deployments/{namespace}/{name}/rollback` — patches
  `spec.template` from a prior revision's ReplicaSet.
- `POST /api/v1/deployments/{namespace}/{name}/scale` and
  `POST /api/v1/statefulsets/{namespace}/{name}/scale` (Roadmap Phase 33)
  — via the Scale subresource (`UpdateScale`), not a direct `spec.replicas`
  patch.
- `POST /api/v1/deployments/{namespace}/{name}/pause` /
  `.../resume` (Phase 33) — patches `spec.paused`
  (`kubectl rollout pause`/`resume`'s mechanism).
- `DELETE /api/v1/pods/{namespace}/{name}` (Phase 33) — a plain `Delete()`
  on the Pod itself (not the Deployment). If the pod has a controller
  (`OwnerReferences` non-empty), the controller recreates it — effectively
  a restart; a bare pod is gone for good. The frontend tells the two cases
  apart (`PodDetailResponse.HasController`) and shows "Restart" vs.
  "Delete" accordingly — the backend's `Delete()` call is identical either
  way.
- `POST /api/v1/resources`, `PUT /api/v1/resources`, and the non-mutating
  `POST /api/v1/resources/dry-run` (Roadmap Phase 25) — generic YAML
  create/update for **any kind the cluster serves** (dynamic client), used
  by the Monaco editor on every detail page's YAML tab and by the "+ Create"
  buttons. This is the deliberately broadest write in this list, approved by
  the project owner as "everything Vinaire can see" rather than an allowlist;
  what actually limits it is the caller's own Kubernetes RBAC (an
  ungranted `create`/`update` comes back as a real 403). Guard rails, all in
  `internal/api/apply.go` / `internal/k8s/apply.go`: exactly one YAML
  document, 1 MiB body cap, strict field validation (a typo'd field is
  rejected, not silently dropped), always a server-side dry-run before the
  real write, a real update must carry the `resourceVersion` the reviewed
  diff was computed against (409 if the object changed since), Secret values
  are redacted from the diff response, and every real write is audited as
  `apply-create`/`apply-update` with kind/apiVersion.
- `DELETE /api/v1/namespaces/{name}` (Roadmap Phase 27) — a plain
  `Delete()` on a Namespace, which cascades to everything inside it and
  can't be undone. Guard rails in `internal/api/namespaces.go`: the request
  body must repeat the namespace's name (`{"confirmName": ...}` — the UI
  makes the user type it), and `default`/`kube-system`/`kube-public`/
  `kube-node-lease` are refused outright. Everything else on the Namespaces
  page — creating a namespace, adding or editing its ResourceQuota/
  LimitRange — deliberately goes through Phase 25's generic YAML apply
  above rather than getting duplicate write endpoints. Audited as
  `delete-namespace`.
- Cluster management (Roadmap Phases 24 and 34) — Vinaire managing **its
  own** registry of clusters, not a write to any browsed cluster:
  `POST /api/v1/clusters` (server + token), `POST
  /api/v1/clusters/import-kubeconfig`, `POST /api/v1/clusters/add-discovered`,
  `POST /api/v1/clusters/import-ssh`, `POST /api/v1/clusters/create-ssh`,
  `PUT /api/v1/clusters/{name}/type` and `DELETE /api/v1/clusters/{name}`.
  Removing a cluster only removes Vinaire's registration of it — never touches
  the cluster. Rules: nothing is stored until Vinaire has actually connected
  to the cluster; the registry is sealed with AES-256-GCM before it reaches a
  Secret or a file (`internal/clusters/crypto.go`), the key coming from
  `CLUSTER_STORE_KEY`, else a separate Secret `vinaire-clusters-key`
  (in-cluster) or a 0600 `store.key` in `VINAIRE_DATA_DIR` (a home server) —
  and an in-cluster install without access to that key Secret still works but
  **warns loudly and stores unencrypted**, so check the logs after upgrading
  the Role; a kubeconfig is parsed by `ParseKubeconfig`, which keeps **only the
  chosen context**, accepts only inline certificate/token data, and **refuses
  `exec`/`auth-provider` plugins (they'd run commands on the Vinaire host),
  file references, proxy-url and non-https or link-local servers** — don't
  loosen any of that; the same cluster under two names is refused by comparing
  the `kube-system` namespace UID; adds are audited as `add_cluster` with
  server/type/origin, never a credential. The SSH modes follow the Phase 30
  rules below (HTTPS only, host-key pinning, in-memory credentials, caller
  needs RBAC `create` on `nodes`, audited as `provision-cluster*`).
  Where the registry lives: a Secret when Vinaire runs in-cluster, an encrypted
  file when `VINAIRE_DATA_DIR` is set, otherwise cluster management is
  unavailable (503) since there is nowhere to persist to.
- Helm releases (Roadmap Phase 28) — `POST /api/v1/catalog/install`,
  `POST /api/v1/catalog/releases/{namespace}/{name}/upgrade`,
  `POST .../rollback` and `DELETE /api/v1/catalog/releases/{namespace}/{name}`,
  all through the Helm SDK (`internal/helm`, `internal/api/catalog.go`), never
  by shelling out to `helm`. A chart can create **any** kind of object, so this
  is as broad as Phase 25's generic apply, and it is bounded the same way: Helm
  is handed the **caller's own** `*rest.Config` (`resolveRESTConfig`), so the
  cluster's RBAC decides what a chart may create — Vinaire's own ServiceAccount
  is never used for it, and it must stay that way (otherwise anyone who can open
  the Marketplace could install a ClusterRole with Vinaire's rights). Guard
  rails: the repositories come only from `catalog.json` / `CATALOG_CONFIG`
  (https, no credentials) — **a request can never name a repository or a chart
  URL**; the catalog's HTTP client refuses loopback/link-local/multicast
  addresses (checked on the connected address, so DNS can't redirect it), never
  follows a redirect out of https, and caps index (48 MiB) and chart (32 MiB)
  sizes; a chart is used only if its download matches the **sha256 in the
  repository index** (no digest, no install); library charts are refused;
  an `oci://` source (a container registry) is bound by the same rules — TLS
  only, **anonymous only**, the chart layer's size is checked from the manifest
  before a byte of it is downloaded and `oras-go` verifies the bytes against
  the manifest's sha256 — and it is read with `oras-go` directly, **not with
  Helm's registry client**, because that one falls back to the Docker
  credentials of the machine Vinaire runs on; an OCI source must list its
  charts explicitly (the catalog never enumerates a registry);
  uninstall needs `{"confirmName": ...}` repeating the release name; upgrade
  refuses to swap in a different chart; every action is audited as
  `helm-install`/`helm-upgrade`/`helm-rollback`/`helm-uninstall` (and `-failed`,
  since a failed install can leave objects behind) with repo/chart/version/
  namespace/release — **never the values**, which can hold passwords. Reading a
  release's values (`GET .../values`, needed to pre-fill an upgrade) is audited as
  `helm-reveal-values`, the same way a Secret reveal is. The release list never
  carries values or the manifest. Helm keeps a release as a Secret in its
  namespace, so the caller needs Secret access there. Around the install:
  - `POST /api/v1/catalog/install/preflight` renders the chart **client-side**
    and asks the cluster, with a `SelfSubjectAccessReview` **as the caller**,
    whether they may `get`/`create` every kind it contains — nothing is written.
    Every install path (direct and background) runs it first and refuses to start
    when something is missing (audited `helm-install-refused`); a failure of the
    check itself doesn't block, the cluster's RBAC still decides.
  - `POST /api/v1/catalog/install-jobs`, `GET`/`DELETE .../install-jobs/{id}` run
    the *same* install in the background as the same caller, so the UI can show
    progress (download bytes/speed, checks, install, pods). A job is visible only
    to the identity that started it (`Session.OwnerKey`), capped per identity,
    forgotten 15 minutes after it ends; the outcome is audited exactly like a
    direct install (`helm-install`/`-failed`, `-refused`, `-cancelled`), and a job
    can only be cancelled before Helm starts creating objects. Pod rows are read
    as the caller.
  - A **deprecated** chart is hidden from search and refused on install unless the
    request carries `allowDeprecated` (the UI's explicit "install anyway").
  - `GET /api/v1/catalog/icons/{repo}/{chart}` serves a chart's icon so the browser
    never contacts the icon's host (the CSP allows images from Vinaire's own origin
    only). The URL is the one the repository's index names — a request can name a
    repository and a chart, never a URL — fetched through `netguard` (https only,
    guarded addresses), capped at 256 KiB, content-sniffed (a script-free SVG or a
    raster image, never the Content-Type header), served `nosniff` + sandboxed.
- GitOps (Roadmap Phase 29) — `PUT /api/v1/gitops/mode`, `POST
  /api/v1/gitops/sources`, `DELETE /api/v1/gitops/sources/{id}`, `POST
  /api/v1/gitops/sources/{id}/sync` and `POST
  /api/v1/gitops/argocd/applications/{namespace}/{name}/sync`
  (`internal/gitops`, `internal/api/gitops.go`). Two different things:
  - **Argo CD mode** only lists Applications and "Sync now" sets the
    Application's `operation` (a merge patch, what `argocd app sync` does) —
    **as the caller**, so the cluster's RBAC decides. Creating an Application
    goes through Phase 25's generic apply with a template; there is no endpoint
    of its own.
  - **Native mode** is the sensitive one: a background worker applies manifests
    from git with server-side apply (field manager `vinaire-gitops`) when
    **nobody is logged in**. It therefore must **never** use Vinaire's own
    identity (anyone who could create a source could then apply anything with
    Vinaire's rights) and cannot use the caller's short-lived token. Each source
    carries the token of a **ServiceAccount the user supplies**, which the
    cluster checks when the source is created (`SelfSubjectReview`; a
    cluster-admin token earns a warning on the source) — so the cluster's RBAC
    bounds what a repository can do. Rules to keep: it is **poll + apply only —
    no pruning** (an object removed from git stays in the cluster and is listed
    as "no longer in git"), no hooks, no health checks, no sync waves beyond
    "Namespaces, then CRDs, then the rest"; git is read with `go-git` **in
    memory** (shallow, one commit, only `*.yaml/*.yml/*.json` under the source's
    path, hidden directories skipped, bounded in files/size/objects) through the
    guarded HTTP client (`internal/netguard`: no loopback/link-local, https
    only; `GITOPS_ALLOW_INSECURE_GIT=true` is the dev-only opt-out, never on a
    real deployment) and a repository URL can't carry credentials, a query or a
    non-https scheme; the git token and the ServiceAccount token arrive in a POST
    body over a secure transport only (`requireSecureTransport`), live in
    `gitops.Secret` (prints `[REDACTED]`), are stored **sealed** in a document next
    to the cluster registry (`DocStore`; `AddSource` refuses to save tokens when
    the store is not encrypted) and are never returned by the API, logged or
    audited; "in sync" is decided by a server-side **dry-run** apply compared with
    the live object (not by resourceVersion — a dry-run never changes it);
    `force` ("take over fields other tools set") is off by default so a field
    changed by kubectl/another controller shows as a conflict rather than being
    overwritten; the cluster's mode must be `native` for a source to be added or
    to run. Audited as `gitops-mode`, `gitops-source-create` /
    `gitops-source-delete` (repo, revision, path, who it applies as — no
    tokens), `gitops-sync-now`, `gitops-argocd-sync`, and — from the background
    worker, as `gitops:<source>` or the person who clicked Sync now —
    `gitops-apply` with the commit and counts, only when something was applied
    or failed (not on every no-op poll). Creating/removing sources is, like the
    cluster registry, not gated by Kubernetes RBAC: Phase 36 should revisit it.
- (Phase 24, unchanged:) cluster management via the web UI (Settings page): create/get/update on exactly
  one Secret (`vinaire-clusters`, in this backend's own namespace, via a
  dedicated narrow `Role`/`RoleBinding` — never the broad, cluster-wide
  read-only `ClusterRole` every *browsed* cluster's resources go through).
  This is a fundamentally different kind of write from the three above:
  it's Vinaire managing its **own** infrastructure config (which clusters
  it's aware of), using its **own** in-cluster identity, not a per-user
  action against a target cluster's resources with the caller's own RBAC.
  It's unavailable (503) when the backend isn't running in-cluster, since
  there's nowhere to durably persist to otherwise.

Every other resource kind (Pods, ConfigMaps, Secrets, Ingress, ...) and
every other verb, on any cluster a user browses, stays read-only.

**Rules for touching this list:**
- Do not add a write verb to `internal/k8s` (`Create*`, `Update*`,
  `Patch*`, `Delete*`, `Apply*`) for a resource kind or action not already
  covered above without raising it as a deliberate decision first — the
  same way Phase 9 itself was an explicit, discussed change, not a quiet
  addition. If a task seems to call for a new write path, stop and flag it
  instead of implementing it.
- Every write action — existing or new — MUST be structured-logged (who
  called it, what action, which object). Non-negotiable; this is the hook
  Phase 10's real authentication plugs into later without touching this
  code again.
- A *browsed* cluster's Secrets stay list/describe metadata only. Decoded
  values are returned **only** through the explicit, audited
  `POST /api/v1/secrets/{namespace}/{name}/reveal` endpoint (Phase 6) —
  never from a list/get endpoint. This part of the original rule is
  unchanged. (The `vinaire-clusters` Secret above is a different thing
  entirely — Vinaire's own config, in its own namespace, never exposed
  through any browse/list endpoint at all.)

If a request conflicts with this — asks for a write action on a resource
kind or verb not already covered above — stop and flag it rather than
silently extending write access.

## Security checklist for every phase

Roadmap Phase 36 audited the project against these; every future phase keeps them. `docs/security.md`
records what was verified and how.

- **Never log, return or store in the clear** a token, Secret value, kubeconfig, SSH credential, git
  token or session id. Wrap credentials in a type that prints `[REDACTED]`, scrub them from job output,
  keep them out of URLs and error messages. The canary test (`backend/security_test.go`) is the model:
  add your flow to it.
- **Check the caller's permissions before returning data.** Use the caller's own `rest.Config`
  (`resolveRESTConfig` / `callerConfig`) — never `entry.Clientset`, `entry.MetricsClientset` or
  `entry.GatewayClientset`, which carry Vinaire's own identity, except for the documented informers. Data
  that comes out of a shared cache (the live stream) is checked per event as the user.
- **Validate any input that ends up in a path, a URL, a command or a query**, with a strict pattern; build
  paths with a containment check; fetch only through `internal/netguard` (or `rest.Config.Dial` for
  clusters); take repositories and registries from operator configuration, never from a request.
- **Audit-log every mutating action** (who, what, which object — never the secret), including the
  background ones (`AuditBackground`) and long-lived sessions (start *and* end, as `exec` does).
- **Changes to Vinaire's own configuration** call `requireVinaireRight` first.
- **A new WebSocket endpoint** must use `originChecker` (a test fails the build otherwise); a new
  state-changing REST route is covered by `OriginGuard` automatically.
- **Cluster data is untrusted input in the UI**: render as text; `dangerouslySetInnerHTML`, `innerHTML`,
  `eval` and web storage are lint errors.
- **Don't weaken a guard to make something work** (CSP, SameSite, origin check, TLS verification, the
  dial guard). If a feature seems to need it, raise it as a decision. `TestInsecureSkipVerifyOnlyWhereReviewed`
  and the other codebase rule tests make a quiet weakening fail the build.
- **Add a test that fails if the property is lost**, and run `govulncheck`, `gosec -severity medium
  -confidence medium` and `npm audit` (the `Security` workflow does).

## Port-forward proxy (Phase 26) — not a write, but a new access path

`/proxy/{cluster}/{namespace}/{pod}/{port}/…` (or `{https:port}`) is an
authenticated reverse **HTTP** proxy into a pod, opened from Pod detail's
"Forward port" button in a new tab. It is outside the write list above
because it mutates nothing in the cluster, but it does reach inside a pod,
so its guard rails live in `internal/api/portforward.go` and
`internal/k8s/portforward.go` — keep them when touching it:

- Reaches the pod through the Kubernetes `pods/portforward` subresource with
  the **caller's own** identity (real RBAC decides; an ungranted caller gets a
  403). No OS port is opened on the server — the SPDY stream is dialed
  directly and used as the HTTP transport's `net.Conn`.
- Keep-alives are off, so each connection is tied to the request that needed
  it: closing the tab cancels the request and tears the connection down.
  Concurrency is capped, and `k8s.ActivePortForwardConns` is the gauge the
  tests read to prove nothing leaks.
- The proxied page is served from Vinaire's own origin, so it is sandboxed:
  `Content-Security-Policy: sandbox` (opaque origin — it can't use the user's
  Vinaire session or API), the session `Cookie`/`Authorization` are **never**
  forwarded to the pod, and the pod's `Set-Cookie` is dropped (it could
  overwrite Vinaire's own cookie). Don't relax these to make a pod UI "just
  work" — apps that need cookies/localStorage or root-relative asset paths
  are a known, documented limitation.
- Each distinct (user, pod, port) reach is audited as `portforward` (deduped
  per minute — a page load is many requests).

## Node provisioning over SSH (Phase 30) — the most sensitive path

The Nodes page's "Add node" takes SSH credentials from the browser, logs into
a server outside the cluster and runs the k3s installer there **as root**.
It is not a Kubernetes write, but it is the highest-risk thing Vinaire does —
a compromised Vinaire could use it to run code on any machine whose
credentials a user types in. Code: `internal/provision` (SSH, `ClusterProvisioner`,
k3s provider, jobs), `internal/api/provision.go`, `internal/stream/provision.go`.
Keep every one of these rules when touching it:

- **Credentials never leave memory and never reach a log.** They arrive in a
  POST body (never a URL/query string, so not in access logs), are wrapped in
  `provision.Secret` (prints as `[REDACTED]` through `fmt`, `slog` and JSON),
  are scrubbed from every line that is stored or streamed (`Redactor`, which
  also learns the join token mid-run), are never written to disk and never go
  into the audit trail. A unit test asserts the `Secret` formatting; re-run
  the "grep the backend log for the password/token" check after any change.
- **HTTPS only.** `requireSecureTransport` refuses the request unless it's TLS,
  a proxy says `X-Forwarded-Proto: https`, or it's loopback-to-loopback (local
  dev). `PROVISION_ALLOW_INSECURE=true` is the explicit opt-out for a dev
  cluster — never set it on a real deployment.
- **Host key pinning, no `InsecureIgnoreHostKey`.** `POST
  /nodes/provision/hostkey` fetches the server's fingerprint (key exchange
  only, no auth); the UI makes the user confirm it; the run's SSH callback
  compares against that exact fingerprint and aborts the handshake — before
  any credential is sent — on a mismatch (`TestDialRefusesWrongHostKey…`).
- **Who may do this is the caller's own Kubernetes RBAC:** `requireNodeCreate`
  runs a `SelfSubjectAccessReview` for `create` on `nodes`. There is no
  Vinaire-level permission check, same as everywhere else.
- **The target can't be Vinaire itself or the cloud's metadata service:**
  `ValidateTarget` refuses loopback, link-local (169.254.169.254), unspecified
  and multicast addresses (private ranges are allowed — that's where nodes
  live) and dials the address it resolved, so DNS can't change underneath it.
- **Nothing the user types reaches the remote shell unchecked:** every field
  is validated against a strict pattern *and* single-quoted into the install
  script, which travels over stdin (so the join token is never in a process
  listing). The installer is downloaded to a file first so a failed download
  can't masquerade as success.
- Every run is audited as `provision-node` / `provision-node-succeeded|failed`
  with host, user, auth *type* (never the secret), job id and result. Jobs are
  scoped to the identity that started them (`Session.OwnerKey`), capped at two
  concurrent, and forgotten 15 minutes after they end.

`ClusterProvisioner` also declares `PreflightControlPlane`,
`InstallControlPlane` and `FetchKubeconfig`, which Phase 34 uses to **create a
new single-server k3s cluster** (installed with `--tls-san <address>` and
refused if `/etc/rancher/k3s` exists or port 6443 is taken) or to **import** an
existing one by reading its admin kubeconfig over SSH. That kubeconfig is
returned as a `Secret` and never logged.

**kubeadm provider (Phase 35, `internal/provision/kubeadm*.go`)** — upstream
Kubernetes on Debian/Ubuntu behind the same interface: preflight (OS, CPU/RAM,
ports, reachability of `pkgs.k8s.io`/`registry.k8s.io`, leftovers of a previous
install), prepare (swap off, kernel modules/sysctls, containerd with
`SystemdCgroup`, held kubelet/kubeadm/kubectl packages), `kubeadm init` with the
CNI's own pod CIDR, join and reset. Rules to keep: what may be installed —
Kubernetes minors and CNI manifests **with pinned versions** — comes only from
`kubeadm.json` (or the file named by `KUBEADM_CONFIG`), validated at startup:
manifests must be https URLs carrying their pinned tag, never "latest"; a worker
accepts only the exact `kubeadm join <host>:<port> --token … --discovery-token-ca-cert-hash …`
shape (no control-plane flags) and the command is **rebuilt from its parsed,
single-quoted parts** rather than run as pasted; kubeadm's own output is scrubbed
of the bootstrap token, CA hash and certificate key (`scrubKubeadm`) — they let
anyone with a route to the API add a node for 24h; a new node is refused if its
hostname/product_uuid collides with an existing node or its clock is more than
90s off. **Reset** (`POST /api/v1/nodes/reset`) is destructive: it needs the SSH
login again, a fresh host-key confirmation and the server address typed out
(`confirmHost`, re-checked server-side), and is audited as `provision-reset*`.

**Local discovery (Phase 34)** — `GET /api/v1/clusters/discover` — reads the
kubeconfig files the Vinaire process can already open (`$KUBECONFIG`,
`~/.kube/config`, `/etc/rancher/k3s/k3s.yaml`, `DISCOVERY_KUBECONFIG_PATHS`) and
nothing else. It is read-only and **never escalates**: no `sudo`, no chmod, no
Docker socket (k3d/kind are found through their kubeconfig contexts). A
candidate is added by its *id* from a fresh scan, never by a client-supplied
path, and only that one context's credentials are extracted. It returns names,
URLs, types and "is cluster-admin" — never a credential — and reports itself
unavailable when Vinaire runs as a pod.

## Authentication (Phase 10) and multi-cluster (Phase 11)

Every `/api/v1/*` request (except `/api/v1/auth/*` itself) requires a
session — `internal/authn.Manager.Middleware` reads an opaque, HttpOnly
`vinaire_session` cookie, looks it up in a server-side session store, and
attaches the resulting identity's real Kubernetes bearer token to the
request context. `AUTH_MODE` selects how that identity was established:
- `"token"` (default): `POST /api/v1/auth/login` takes a pasted Kubernetes
  ServiceAccount token directly; the session's TTL is read from the
  token's own JWT `exp` claim.
- `"oidc"`: a real Authorization Code + PKCE flow against Keycloak (or any
  OIDC provider) — entirely server-side in this Go process
  (`internal/authn/oidc.go`), never in the frontend. See `/keycloak` at the
  repo root for the dev instance.
- `"disabled"`: no login at all (local k3d dev only) — every request uses
  the backend's own kubeconfig-derived credentials, like every phase before
  Phase 10.

There is no separate app-level policy layer (no group→namespace allow-list
in this codebase) — with **one deliberate, narrow exception** (Phase 36): changes to
Vinaire's *own* state (add/remove/import/discover a cluster, change its type, GitOps mode
and sources, SSH cluster create/import) aren't Kubernetes actions, so no Kubernetes RBAC
rule covers them. They are gated by `Handler.requireVinaireRight`, which keeps the same
philosophy: it asks the cluster, with a `SelfSubjectAccessReview` **as the caller**, whether
they may `create`/`update`/`delete` the virtual resources `clusters.vinaire.io` /
`gitops.vinaire.io` on the registry's default cluster (the chart ships an unbound
`<fullname>-admin` ClusterRole). Any new handler that changes Vinaire's own
configuration MUST call it first (a test lists every gated handler). Otherwise:
`internal/clusters.Entry.ClientsetForToken` builds a
per-request Kubernetes clientset authenticated as the caller's own
token/identity, so **the target cluster's real RBAC is what decides**
what a request can do — an unauthorized call gets a real `403`/`401` back
from the Kubernetes API itself (`internal/api.respondK8sError` maps that to
the right HTTP status), the same as `kubectl` would show. Adding an
app-level permission check on top of that is the kind of change that
should be raised first, same spirit as the core rule above.

Multi-cluster (Phase 11) lives in `internal/clusters` (`Registry`/`Entry`
per configured kubeconfig context, via `CLUSTER_CONTEXTS`). Every handler
resolves its target cluster from the `X-Cluster` request header (or the
first configured cluster if absent) before doing anything else — see
`Handler.resolveCluster`.

With `CLUSTER_CONTEXTS` unset (single-cluster mode), the one cluster's
display name in Vinaire's own UI comes from `CLUSTER_NAME` if set,
otherwise the resolved kubeconfig's current-context name (meaningless when
running in-cluster, where there's no kubeconfig file to read at all), and
only falls back to the literal word `"default"` if neither is available —
see `k8s.CurrentContextName` and `main.go`'s `buildClusterRegistry`.
Kubernetes has no self-reported cluster name the API exposes, so
`CLUSTER_NAME` is the only way to get something meaningful for a real
in-cluster deployment; the Helm chart (Phase 21) defaults it to the
release name.

**Cluster management via the web UI** extends this: `Registry` is no
longer populated once at startup and left read-only — `AddDynamic`/
`Remove` let a cluster be added or removed while the app is serving
traffic (guarded by `Registry`'s own mutex), each triggering a lifecycle
hook `main.go` wires up to start/stop that cluster's live-update informers
(`internal/stream.HubRegistry`, itself mutex-guarded for the same reason).
A cluster added this way (`POST /api/v1/clusters` — name + server URL +
bearer token + optional CA cert, from the Settings page) is durably
persisted via `internal/clusters.Store`, a Secret (`vinaire-clusters`) in
this backend's own namespace, written with its own in-cluster identity —
see the core rule above for the RBAC this needs. `Store` is only
constructed when the backend detects it's actually running in-cluster
(`clusters.NewStore`); outside a cluster, `Handler.ClusterStore` is `nil`
and `AddCluster`/`RemoveCluster` report themselves unavailable (503) rather
than pretending to work with nowhere to persist to. `Entry.Removable`
distinguishes an env-var-configured cluster (`false`, the Settings page
won't offer to remove it — it would just reappear on the next restart)
from one added dynamically (`true`, whether just added or restored from
`Store` at startup).

### Example: a minimal ClusterRole for Vinaire (not cluster-admin)

Since Phase 10 makes the *caller's own* RBAC the only authorization check,
whoever logs in needs a real RBAC binding — pasting a token for a
ServiceAccount with no role bound to it just gets `403` on everything. This
mirrors what `internal/api`'s handlers actually call — read-only, matching
this file's core rule, plus the three write verbs Phase 9 allows:

```yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: vinaire-viewer
rules:
  - apiGroups: ["", "apps", "batch", "networking.k8s.io"]
    resources:
      [
        "pods",
        "deployments",
        "statefulsets",
        "daemonsets",
        "jobs",
        "configmaps",
        "secrets",
        "services",
        "ingresses",
        "networkpolicies",
        "cronjobs",
        "events",
        "replicasets",
        "serviceaccounts",
        "persistentvolumeclaims",
        "persistentvolumes",
        "nodes",
        # Phase 27's Namespaces page.
        "namespaces",
        "resourcequotas",
        "limitranges",
      ]
    verbs: ["get", "list", "watch"]
  # metrics-server's aggregated API (Phase 3's Pod CPU/Memory gauges, the
  # Dashboard's cluster-wide usage, and Phase 19's per-node usage) is a
  # separate API group from core Pods/Nodes above — granting "pods"/"nodes"
  # doesn't imply this. Every metrics call already degrades to "unavailable"
  # rather than failing outright, so a missing grant here silently shows "—"
  # everywhere instead of an obvious error — easy to miss.
  - apiGroups: ["metrics.k8s.io"]
    resources: ["pods", "nodes"]
    verbs: ["get", "list"]
  # pods/log and pods/exec are separate RBAC subresources — granting
  # "pods" above does NOT imply either of these. Without this block, Pod
  # detail's Logs/Terminal tabs open a WebSocket that connects fine and
  # then immediately gets a 403 from the Kubernetes API itself, which
  # looks like a Vinaire bug rather than a missing RBAC grant.
  - apiGroups: [""]
    resources: ["pods/log"]
    verbs: ["get"]
  - apiGroups: [""]
    resources: ["pods/exec"]
    verbs: ["create"]
  # Phase 26's Forward port: also a separate subresource, and a read-style
  # access path (nothing is mutated) — but it reaches inside a pod, so it's
  # granted explicitly. Omit it to disable the feature for this identity.
  - apiGroups: [""]
    resources: ["pods/portforward"]
    verbs: ["create"]
  - apiGroups: ["storage.k8s.io"]
    resources: ["storageclasses"]
    verbs: ["get", "list", "watch"]
  - apiGroups: ["rbac.authorization.k8s.io"]
    resources: ["roles", "rolebindings", "clusterroles", "clusterrolebindings"]
    verbs: ["get", "list", "watch"]
  - apiGroups: ["gateway.networking.k8s.io"]
    resources: ["gateways", "httproutes"]
    verbs: ["get", "list", "watch"]
  - apiGroups: ["apiextensions.k8s.io"]
    resources: ["customresourcedefinitions"]
    verbs: ["get", "list", "watch"]
  # Phase 15's generic CRD viewer can only list a CRD's *instances* if this
  # role also grants read on that CRD's own API group — there's no way to
  # grant this generically without a full apiGroups: ["*"] wildcard, which
  # would cross the "not cluster-admin" line this file draws elsewhere. List
  # the actual groups installed on your cluster here instead (this example:
  # a k3d dev cluster with Traefik Hub, Gateway API, and k3s/Helm-controller
  # CRDs) and add more as new CRDs show up — the Custom Resources page will
  # 403 on any CRD instance whose group isn't listed here, same as any other
  # RBAC gap.
  - apiGroups: ["hub.traefik.io", "traefik.io", "helm.cattle.io", "k3s.cattle.io"]
    resources: ["*"]
    verbs: ["get", "list", "watch"]
  # Phase 9's three write actions (restart/delete/rollback all patch or
  # delete the Deployment object itself) plus Phase 33's Pause/Resume
  # (also a plain Deployment patch) — omit this whole block for a
  # read-only login.
  - apiGroups: ["apps"]
    resources: ["deployments"]
    verbs: ["patch", "delete"]
  # Phase 33's Scale control — a separate subresource from "deployments"/
  # "statefulsets" above, needs its own grant (the same way pods/log and
  # pods/exec are separate from "pods" earlier in this file).
  - apiGroups: ["apps"]
    resources: ["deployments/scale", "statefulsets/scale"]
    verbs: ["get", "update"]
  # Phase 33's pod-level Delete (owned pods: effectively a restart once
  # the controller recreates them; bare pods: gone for good).
  - apiGroups: [""]
    resources: ["pods"]
    verbs: ["delete"]
  # Phase 28's Marketplace: Helm keeps each release as a Secret
  # (type helm.sh/release.v1) in the release's namespace, so installing,
  # upgrading, rolling back and uninstalling need these on Secrets — on top of
  # whatever the chart itself creates (Deployments, Services, ClusterRoles,
  # CRDs...), which is the caller's RBAC too. Listing releases needs the
  # cluster-wide list on Secrets already granted above. Omit for a catalog that
  # can browse but not install.
  - apiGroups: [""]
    resources: ["secrets"]
    verbs: ["create", "update", "delete"]
  # Phase 36: permission to change Vinaire ITSELF. These resources don't exist in
  # the cluster — they are names for the permission, asked with a
  # SelfSubjectAccessReview as the caller. The chart creates this role unbound
  # (<fullname>-admin); bind it to whoever administers Vinaire. It is NOT cluster
  # admin and grants nothing inside Kubernetes: bind it IN ADDITION to the viewer
  # role (and to <fullname>-installer to install charts), never instead of them.
  - apiGroups: ["vinaire.io"]
    resources: ["clusters", "gitops"]
    verbs: ["create", "update", "delete"]
  # Phase 29's Argo CD mode: listing Applications needs read on them (grant
  # the group like any other CRD above), "Sync now" a patch. Native mode needs
  # nothing here — it applies as the source's own ServiceAccount.
  - apiGroups: ["argoproj.io"]
    resources: ["applications"]
    verbs: ["patch"]
  # Phase 27's namespace delete — irreversible and cascading; omit unless
  # this identity should be able to remove whole namespaces.
  - apiGroups: [""]
    resources: ["namespaces"]
    verbs: ["delete"]
  # Phase 30's "Add node": the gate is a SelfSubjectAccessReview for `create`
  # on `nodes` — omit it and the Add node dialog returns a 403. (Joining a
  # node doesn't call this API itself — the node registers on its own — so
  # this grant is purely "may this identity grow the cluster".)
  - apiGroups: [""]
    resources: ["nodes"]
    verbs: ["create"]
  # Phase 25's generic YAML create/update. Vinaire imposes no allowlist of
  # its own here — grant only the kinds this identity should be able to
  # write; anything not granted returns a real 403. Example: Deployments
  # and ConfigMaps only. (Secrets `create`/`update` here also allows the
  # "+ Create" Secret template — omit it if that's not wanted.)
  - apiGroups: ["apps"]
    resources: ["deployments"]
    verbs: ["create", "update"]
  - apiGroups: [""]
    resources: ["configmaps"]
    verbs: ["create", "update"]
```

Bind it cluster-wide or to a single namespace (`RoleBinding` instead of
`ClusterRoleBinding`, same `roleRef`) depending on how much of the cluster
that identity should see — Vinaire enforces nothing beyond what the binding
already grants. Then create a ServiceAccount and a **bounded-duration**
token to paste into Vinaire's login screen:

```bash
kubectl create serviceaccount vinaire-viewer -n <namespace>
kubectl create clusterrolebinding vinaire-viewer \
  --clusterrole=vinaire-viewer --serviceaccount=<namespace>:vinaire-viewer
kubectl create token vinaire-viewer -n <namespace> --duration=1h
```

`--duration` directly sets the session's own TTL (Phase 10 reads it from
the token's `exp` claim) — don't hand out tokens with no expiry for
anything but a throwaway local test.

## Rendering model

The frontend uses Next.js purely as a build tool, not a runtime server.
`next build` runs with `output: "export"`, which renders every page once,
at build time, into static HTML/JS/CSS files under `/frontend/out`. At
runtime, Next.js never runs as a server — not in dev-via-Docker, and
especially not in the single combined production image (Phase 10's second
half): the Go backend serves that `out/` directory as-is
(`backend/static.go`'s `http.FileServer`-based handler, mounted only when
`STATIC_DIR` is set) from the same port as the API. All dynamic
content loads client-side, via `fetch`/WebSocket calls to the Go backend —
Go never does HTML templating or rendering; it only ever serves static
files, answers JSON/WebSocket on `/api/*`, or (Phase 26) reverse-proxies
an authenticated pod port under `/proxy/*`. `next dev` is the one
exception: local development still runs a normal Next.js dev server with
hot reload, exactly as before — `output: "export"`'s constraints (no
Route Handlers with server logic, no Middleware, no Server Actions, no
`next/image` without a custom loader) only bind at `next build` time. This
is also why dynamic routes like a pod's `[namespace]/[name]` don't exist
as Next.js route segments — a static export can't pre-render a path for a
resource name it doesn't know at build time — these pages instead read
`?namespace=&name=` query params client-side.

## Repository structure

```text
.
├── frontend/               Next.js (App Router) + TypeScript + Tailwind CSS
│   └── src/app/            Pages, layouts, route handlers
├── backend/                Go module: github.com/shiraku/vinaire/backend
│   ├── main.go             HTTP server (chi), route wiring, graceful shutdown
│   ├── internal/config/    env-driven config (PORT, KUBECONFIG, CORS, OIDC,
│   │                       CLUSTER_CONTEXTS)
│   ├── internal/k8s/       client-go clientset + listers, read-only except
│   │                       Phase 9's Deployment restart/delete/rollback
│   ├── internal/authn/     sessions + token/oidc/disabled auth modes
│   │                       (Phase 10) — see the section above
│   ├── internal/clusters/  per-kubeconfig-context client registry (Phase 11)
│   ├── internal/api/       HTTP handlers: response shaping, status/color logic
│   └── internal/stream/    informers + WebSocket fan-out for live updates
├── keycloak/               Throwaway dev-only Keycloak (docker-compose) for
│                           local OIDC login — see its README
├── charts/vinaire/         Helm chart for installing onto a real cluster
│                           (Phase 21) — Deployment/Service/ServiceAccount/
│                           ClusterRole templates for the same combined
│                           image as the repo-root Dockerfile
├── package.json            Root orchestrator only (`npm run dev` = both apps)
├── ROADMAP.md              Staged plan
├── CHANGELOG.md            Keep a Changelog format
└── .github/workflows/      CI
```

## Conventions

### Go (`/backend`)

- Format with `gofmt` (tabs, standard Go style) before committing.
- Lint with [`golangci-lint`](https://golangci-lint.run/) if available
  locally (`golangci-lint run ./...`); CI runs `go build` + `go vet` at
  minimum.
- `internal/k8s` stays read-only except the exact Phase 9 write actions
  already covered by the core rule above — don't add another write verb
  there without raising it first. New listers go there; new routes get
  wired up in `main.go`.
- Errors are wrapped with context (`fmt.Errorf("...: %w", err)`), not
  swallowed.

### Frontend (`/frontend`)

- Lint with ESLint (`npm run lint`), format with Prettier
  (`npm run format` / `npm run format:check`).
- Tailwind CSS for styling; theme tokens (background, surface, border,
  foreground, muted, accent) are defined in `src/app/globals.css` — reuse
  them instead of hardcoding colors.
- Dark theme is the default and, for now, the only theme. Don't add a light
  theme toggle unless asked.
- Fonts are wired through `next/font` in `src/app/layout.tsx`
  (`--font-sans` = IBM Plex Sans, `--font-mono` = JetBrains Mono). Use the
  Tailwind `font-sans` / `font-mono` utilities rather than importing fonts
  elsewhere.
- `frontend/AGENTS.md` and `frontend/CLAUDE.md` are auto-generated/managed by
  the Next.js CLI (`next dev` regenerates them) — leave them as-is.
- **Known local issue:** Next.js 16's Turbopack compiler currently panics on
  this Windows dev machine while processing `globals.css`. `dev` and `build`
  scripts in `package.json` are pinned to `--webpack` as a workaround. If you
  hit a Turbopack panic mentioning `evaluate_webpack_loader` or "node process
  exited before we could connect to it", that's this issue — don't spend time
  debugging the CSS, just confirm the `--webpack` flag is still in place.
- **Resolved-by-architecture, formerly a known local issue:** on this dev
  machine, Proxifier (a running background app, unrelated to this project)
  intercepts `node.exe`'s outbound TCP connections and breaks them even to
  `localhost`, confirmed via `tasklist` and by testing plain Node `fetch`/
  `http`/raw `net.Socket` against several local targets (Docker-published
  ports and native Windows processes alike) — all failed the same way. This
  first surfaced when Phase 10's OIDC flow briefly ran through next-auth
  (a Node-side OIDC client in the Next.js server), and cost real time to
  diagnose as a Windows/Proxifier issue rather than a Keycloak or
  Docker-networking bug. Phase 10's current design (see "Rendering model"
  below and `internal/authn`) does the entire OIDC exchange in the Go
  backend and ships the frontend as a static export with no Next.js server
  at runtime at all — so there's no Node process left making outbound calls
  for Proxifier to intercept, and this class of bug can't recur here. Kept
  as a record in case a future change reintroduces server-side Node code
  (an API route, middleware, etc.) on this machine.

## Running tests / checks

```bash
# Backend
cd backend
go build ./...
go vet ./...
gofmt -l .          # should print nothing
golangci-lint run ./...   # if installed

# Frontend
cd frontend
npm run lint
npm run format:check
npm run build
```

There's no unit test suite yet (see ROADMAP.md — this is pre-MVP) — but
Roadmap Phase 23 added a real integration/e2e suite, run against an actual
disposable cluster rather than mocked client-go:

```bash
# Backend integration tests (needs a real cluster + a running backend —
# see .github/workflows/ci.yml's `integration` job, or run
# backend/integration/testdata/manifests.yaml + `go run .` locally first)
cd backend
go test -tags=integration -count=1 ./integration/...

# Frontend e2e (needs E2E_TOKEN — a real ServiceAccount token — and
# E2E_BASE_URL pointed at a running frontend/backend pair)
cd frontend
npm run test:e2e
```

Both are excluded from the fast `go build`/`go vet`/`npm run build` path
above (the integration tests sit behind a `go:build integration` tag; the
e2e test skips itself when `E2E_TOKEN` is unset) and only run in CI on
pull requests into main, not every push — they need a real k3d cluster,
which is noticeably slower than the lint/build jobs. When adding the first
unit test, add it alongside its own feature rather than retrofitting
later.

## Git module path

The Go module is `github.com/shiraku/vinaire/backend`. If the actual GitHub
org/repo location changes, update `go.mod`'s module line and all internal
import paths together in one commit.
