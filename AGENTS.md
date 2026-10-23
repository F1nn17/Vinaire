# AGENTS.md

Instructions for AI coding agents (Claude Code and similar) working in this
repository. Key project decisions that are not rules for code: [docs/decisions.md](docs/decisions.md).

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
- Node lifecycle (Roadmap Phase 40, `internal/k8s/node_actions.go`,
  `internal/api/node_actions.go`) — all as the caller, each audited:
  `POST /api/v1/nodes/{name}/cordon` and `/uncordon` (merge patch of
  `spec.unschedulable`); `POST /api/v1/nodes/{name}/drain` (cordon, then pods
  evicted through the **Eviction API**, so PodDisruptionBudgets are honoured —
  a pod a budget protects is retried ≤ 75 s and reported as *pending*, never
  deleted instead; DaemonSet and mirror pods are skipped; pods with no controller
  need `evictUnmanaged` and pods with an emptyDir need `deleteLocalData`, the
  analogues of kubectl's `--force` / `--delete-emptydir-data`; the body must
  repeat the node's name; audited `drain-node` with counts);
  `PUT /api/v1/nodes/{name}/labels` (set/remove in one merge patch; keys and values
  validated, `kubernetes.io/hostname` refused); `PUT /api/v1/nodes/{name}/taints`
  (the whole list, replaced under the resourceVersion it was read at; key/value/effect
  validated); `DELETE /api/v1/nodes/{name}` (the Node *object* only — the body must
  repeat the name; the machine and kubelet are untouched). `GET
  .../drain-plan` is a read. Audited `cordon-node`, `uncordon-node`, `drain-node`,
  `label-node`, `taint-node`, `delete-node`. The chart ships these rights as the
  separate, unbound `<fullname>-node-admin` role (nodes get/list/watch/patch/delete,
  pods get/list, `pods/eviction` create) — not folded into viewer.
- RBAC bindings (Roadmap Phase 44, `internal/api/rbac_actions.go`,
  `internal/k8s/rbac_actions.go`) — as the caller, audited: `POST
  /api/v1/rbac/bindings` creates a **RoleBinding** or **ClusterRoleBinding** to a
  User, Group or ServiceAccount; `DELETE /api/v1/rbac/rolebindings/{namespace}/{name}`
  and `.../clusterrolebindings/{name}` remove one. **Vinaire never creates or edits a
  Role or ClusterRole** — it hands out permissions that already exist, it can't invent
  new ones. Guard rails: a cluster-wide grant needs the role's name typed
  (`confirmName`) and may only name a ClusterRole; names, subjects and namespaces are
  strictly validated; created bindings carry `app.kubernetes.io/managed-by: vinaire`
  and a `vinaire.io/created-by` annotation; deletes need the binding's name typed and
  refuse `system:*` and `cluster-admin`; and because every call is the caller's own,
  **Kubernetes' escalation check applies** (you can't bind a role with rights you lack
  unless the cluster gave you `bind` on it — the chart's `-rbac-admin` role grants
  `bind` only for the chart's own roles). `POST /api/v1/rbac/check` (a
  SubjectAccessReview) and `GET /api/v1/rbac/subject` are reads. Audited
  `rbac-bind`, `rbac-bind-failed`, `rbac-unbind`.
- `DELETE /api/v1/statefulsets/{namespace}/{name}`, `DELETE /api/v1/daemonsets/{namespace}/{name}` and
  `DELETE /api/v1/jobs/{namespace}/{name}` (owner's decision of 2026-10-09, so that the multi-select and the
  Delete button work on every Workloads tab, not only Deployments and Pods) — a plain `Delete()` as the caller, with
  **background propagation** (the pods are removed by the garbage collector, as with `kubectl delete`; a Job needs it
  spelled out because client-go's default for batch/v1 orphans them). A StatefulSet's volume claims stay. Names are
  validated, each call is audited (`delete-statefulset`/`delete-daemonset`/`delete-job`), and the UI's bulk run makes
  the user type the number of objects. The chart's `-installer` role grants these verbs; `-viewer` does not.
- "Install from an image" (Roadmap Phase 67, `frontend/src/lib/images/presets.ts`, `app/(shell)/marketplace/image`) adds **no
  write path**: the page builds a StatefulSet, a Secret and a Service and creates them one by one through the generic YAML create
  above, as the caller. Rules to keep: the presets name only the publishers' official images; every typed value is validated by a
  pattern before it reaches YAML; passwords are generated in the browser, written only into the Secret and never put in an
  environment value or shown in the preview; the Secret and the Service carry an `ownerReference` to the StatefulSet. The tag list
  (`GET /api/v1/images/tags`, `internal/imagetags`) is a read of two fixed public registries (Docker Hub, quay.io) through
  `internal/netguard`: the request names an image checked against a strict pattern, never a URL or a host, and a `next` link in a
  registry's answer is followed only if it stays on that registry.
- "Publish a Service outside" (Routing page, `frontend/src/components/network/PublishServiceModal.tsx`) adds **no write path**:
  it builds one more Service (NodePort or LoadBalancer, same selector and ports as an existing Service, never touching that one)
  and creates it through the generic YAML create above, as the caller.
- `DELETE /api/v1/services/{namespace}/{name}` and `DELETE /api/v1/ingresses/{namespace}/{name}` (owner's decision of
  2026-10-10, so that Services & Ingress has a multi-select and a Delete like the workload lists) — plain `Delete()` calls as the
  caller (`internal/api/workload_delete.go`); names validated, system namespaces refused, and so is the `kubernetes` Service of
  `default`. Audited as `delete-service` / `delete-ingress`. Editing needs no endpoint of its own: a row opens the generic
  resource page whose YAML tab uses Phase 25's apply. The chart's `-installer` role grants the verbs; `-viewer` does not.
- `DELETE /api/v1/secrets/{namespace}/{name}` (owner's decision of 2026-10-09, so that what an uninstalled chart
  leaves behind — a database's password Secret — can be removed) — a plain `Delete()` on one Secret, as the caller
  (`internal/api/secrets.go`). The body must repeat the Secret's name (`{"confirmName": ...}`), `kube-system`/
  `kube-public`/`kube-node-lease` are refused, and so is a Helm release record (type `helm.sh/release.v1`, `409`: deleting
  it makes Helm forget a release that is still running — uninstall it instead). Audited as `delete-secret` (namespace,
  name, type — never a value). The chart's `-installer` role grants the verb; `-viewer` does not.
- `DELETE /api/v1/pods/{namespace}/{name}` (Phase 33) — a plain `Delete()`
  on the Pod itself (not the Deployment). If the pod has a controller
  (`OwnerReferences` non-empty), the controller recreates it — effectively
  a restart; a bare pod is gone for good. The frontend tells the two cases
  apart (`PodDetailResponse.HasController`) and shows "Restart" vs.
  "Delete" accordingly — the backend's `Delete()` call is identical either
  way.
- `DELETE /api/v1/pvcs/{namespace}/{name}` (owner's decision of 2026-10-09, so that the volume claims
  Helm leaves behind after an uninstall or a rolled-back install can be removed) — a plain `Delete()` on one
  PersistentVolumeClaim, as the caller. Irreversible (with the usual `Delete` reclaim policy the data goes
  with the claim), so, in `internal/api/storage.go`: the body must repeat the claim's name
  (`{"confirmName": ...}` — the UI makes the user type it), `kube-system`/`kube-public`/`kube-node-lease`
  are refused, and a claim that a pod still mounts is refused with `409` naming the pods. Audited as
  `delete-pvc` (namespace, name, the PersistentVolume and the class — never anything from inside the
  volume). The chart's `-installer` role grants the verb; `-viewer` does not.
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
    direct install (`helm-install`/`-failed`, `-refused`, `-cancelled`). A job can be
    cancelled at any stage (Roadmap Phase 60): before Helm starts that is all; once it is
    creating objects Helm is interrupted and — after a short settle, because Helm keeps
    applying in the background — **the job's own release is uninstalled** (`helm
    uninstall`, as the caller, audited `helm-install-rollback`/`-failed`); the same
    `DELETE` undoes an install that already failed after Helm began. Guard rails: the
    release name must have been free just before the install (otherwise the install
    fails and an existing release is never touched), and **Vinaire never deletes the
    namespace or the volume claims itself** — it reports them (`namespaceCreated`,
    `leftovers`) and the UI offers the existing, audited namespace delete (Phase 27,
    name typed). Pod rows are read as the caller.
  - **Install recipes** (Roadmap Phase 59, `internal/helm/recipes.json`, `CATALOG_RECIPES`): data that turns a
    few answers ("my own PostgreSQL", "publish via Ingress") into **values for the same chart being installed** — they
    install, create and name nothing themselves. Like the repositories they come only from Vinaire's data or the operator's
    file, **never from a request**, and are validated (a field that can reach YAML text must carry a pattern; a password is
    never a field — only the name of an existing Secret and a key). The checks around the install (what it will create,
    default StorageClass, IngressClass, Service endpoints, Secret key names) are reads as the caller; a Secret's values are
    never read for them. The volume claims left after an uninstall are listed and can be deleted one by one through
    `DELETE /api/v1/pvcs/…` (above).
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
- Sign-in through Keycloak (Roadmap Phase 47, `internal/keycloak`, `internal/api/keycloak.go`)
  — Vinaire's **own** configuration again, plus **writes to a Keycloak** the operator
  names: `POST /api/v1/auth/keycloak/setup` takes a Keycloak administrator's
  credentials (in the POST body, over a secure transport only, wrapped in
  `keycloak.Secret`, used for **one token request** and never stored, logged, audited or
  returned) and creates — idempotently — a realm, a **confidential** OIDC client for
  Vinaire (authorization-code flow with PKCE only; no password grant, no service account,
  redirect URI and web origin exactly Vinaire's own, never a wildcard; an existing
  *public* client is refused) and, optionally, a first user. The new client's secret is
  the one thing kept: sealed in the same store as the cluster registry
  (`auth-oidc` document), and **refused when the store is not encrypted**. Sign-in is
  then switched on **at run time** (`authn.Manager.ConfigureOIDC`, after OIDC discovery
  proves the realm and secret work) and again after a restart (`RestoreOIDC`); pasting a
  token stays open as the way back unless the administrator turns it off
  (`PUT .../token-login` — refused while OIDC is off, so nobody can lock everyone out);
  `DELETE /api/v1/auth/keycloak` forgets Vinaire's side only and never touches Keycloak.
  Every handler calls `requireVinaireRight` on the virtual resource `auth.vinaire.io`
  (`get`/`create`/`update`/`delete`, in the chart's `-admin` role). The Keycloak URL comes
  from the request, so it is held to the guarded HTTP client (`internal/netguard`: https,
  no loopback/link-local; `OIDC_ALLOW_INSECURE=true` is the dev-only opt-out, never on a
  real deployment) — which is also what OIDC discovery, the token exchange and key
  fetches use. **Home server without a certificate** (owner's decision of 2026-10-21, Roadmap Phase 79): the operator may set `OIDC_ALLOW_HTTP_PRIVATE_NETWORK=true` (chart value `keycloak.allowHttpPrivateNetwork`, off by default; a request can never turn it on). Then Keycloak may be reached over plain http **only if the address the connection actually goes to is private** — RFC 1918, 100.64.0.0/10 (NetBird/Tailscale) or IPv6 unique-local, checked in the dialer (`netguard.Options.AllowHTTPOnPrivate`), so an http URL that resolves to a public, loopback or link-local address is refused, and an https → http redirect is never followed — and the administrator's password may arrive over plain http **only from a client on a private network** (the peer and every `X-Forwarded-For` entry; a guard against an administrator's mistake, not against an attacker). It does not touch `OIDC_ALLOW_INSECURE` (loopback, dev only) or `PROVISION_ALLOW_INSECURE`, and every other credential-taking route still needs TLS. Not available when `AUTH_MODE` is `disabled` or `oidc` (the operator owns
  that). Audited as `keycloak-setup` / `keycloak-setup-failed` (Keycloak host, realm,
  client, steps — no credentials), `keycloak-token-login`, `keycloak-disable`. What Vinaire
  cannot do is make the **cluster's API server trust the issuer** — the wizard shows the
  `--oidc-*` flags; without them sign-in works and every cluster call is a 401.
- Shared sessions and the sessions list (Roadmap Phase 48, `internal/authn/shared.go`,
  `internal/clusters/sessions_kv.go`, `internal/leader`, `internal/api/sessions.go`) — Vinaire's
  **own** state again: with `SESSION_STORE=secret` the sessions live in the Secret `vinaire-sessions`
  of Vinaire's own namespace, written with Vinaire's own identity (the narrow `-cluster-manager`
  Role: `get`/`patch` on that one name), **sealed** with the registry's key (never unsealed — the mode
  refuses to start without a key), one key per session named by the SHA-256 of the id. A
  leader-election Lease (`vinaire-leader`) keeps scheduled GitOps polling to one replica.
  `GET /api/v1/auth/sessions` and `DELETE /api/v1/auth/sessions/{ref}` (right `auth.vinaire.io`,
  audited `session-revoke`) list and end sessions; the list never carries a cookie or token.
- Key provider and credential rotation (Roadmap Phase 49, `internal/keyprovider`,
  `internal/clusters/keyring.go`): `KEY_PROVIDER=vault` keeps the store's data key only *wrapped* by a
  Vault/OpenBao Transit key (configuration from the environment, never from a request; https unless
  `VAULT_ALLOW_INSECURE=true`, dev only). Failing to obtain the key is **fatal** — never fall back to a new
  key or to an unencrypted store; a wrapped store is not opened without its provider. A raw key and a
  provider together (`CLUSTER_STORE_KEY` + `KEY_PROVIDER`) is refused. `PUT /api/v1/clusters/{name}/credentials`
  replaces the bearer token of a cluster added with a token: right `clusters.vinaire.io` `update`, secure
  transport only, verified by a real call, must be the **same** cluster (kube-system UID), audited as
  `rotate-cluster-credentials` — never the token. No endpoint ever returns a stored credential.
- "I don't have Keycloak yet" on the Sign-in tab (Roadmap Phase 69, `app/(shell)/settings/KeycloakInstall.tsx`, `lib/keycloakInstall.ts`, `lib/images/apply.ts`) adds **no write path**: it creates a namespace, a PostgreSQL and a Keycloak StatefulSet with their Secrets and Services (the "Install from an image" presets) and an Ingress through Phase 25's generic YAML create, as the caller. The passwords are generated in the browser and written only into those Secrets. It must not weaken Phase 47's rule that Vinaire talks to a Keycloak only over https.
- "This chart needs a value" on the install page (Roadmap Phase 71, `internal/helm/problems.go`, `components/marketplace/RequiredValues.tsx`) adds **no write path**: the preflight renders the chart client-side (a read) and describes what the chart asks for; the page writes the answer into the values text, and the only write it can make is a Secret through Phase 25's generic YAML create, as the caller ("Create the Secret with a generated password"). The chart author's error text is untrusted: a name counts only if it resolves in the chart's own default values (`FuzzClassifyRenderError`).
- "Dashboards follow what is installed" (Roadmap Phase 74a, `internal/apps`, `internal/api/detected_apps.go`) adds **no write path**: `GET /api/v1/apps/detected` lists pods as the caller (`k8s.ListPods`) and recognises PostgreSQL, Redis/Valkey, MinIO and Keycloak by the repository of their images — never by a label — and writes nothing. The image text is untrusted input (`FuzzRepoAndTag`).
- The exporter form on a dashboard (Roadmap Phase 74b, `components/dashboards/ExporterInstall.tsx`, `lib/dashboards/exporters.ts`) adds **no write path**: it starts the Marketplace's own install (`POST /api/v1/catalog/install-jobs`, as the caller, audited as any Helm install) of the Prometheus community exporter charts next to the database, with values built from what was found in the cluster. A password is never read or typed — the chart is given a Secret's name and key. MinIO and Keycloak get **instructions only** (`MetricsHowTo.tsx`): Vinaire does not edit a running application to switch its metrics on (owner's decision of 2026-10-16).
- Instances (Roadmap Phase 77a, `internal/instances`, `internal/api/instances.go`, `app/(shell)/instances`) add **no write path**:
  `GET /api/v1/instances` lists StatefulSets, Deployments, Services, PVCs and Secrets as the caller and groups them by
  `app.kubernetes.io/instance` + `managed-by` (`vinaire` or `Helm`), saying per object whether a delete removes it or leaves it
  (volume claims always stay). A Secret contributes its name and type only. The page's Delete is made of calls that already exist —
  Helm uninstall, `DELETE statefulsets|deployments`, `DELETE pvcs` — with the instance's name typed and each volume claim ticked by
  name; cluster namespaces are not deletable from it. The labels are untrusted (validated before they are returned).
- Pod diagnosis (Roadmap Phase 75a, `internal/diagnose`, `components/workloads/PodDiagnosis.tsx`) adds **no write path** and no route: the `diagnosis` field of the pod page's response is computed from the pod and its events, which the handler already reads as the caller. The cluster's messages are untrusted text (`FuzzPodDiagnosis`: bounded detail, never a finding without a title); titles and hints are fixed strings so they can be translated.
- Install warnings (Roadmap Phase 75b, `internal/helm/plan.go`, `internal/api/install_environment.go`, `components/marketplace/WizardWarnings.tsx`) add **no write path**: the chart plan's new warnings come from the client-side render the preflight already makes plus reads as the caller; `GET /api/v1/install/environment` lists StorageClasses, IngressClasses and looks for a load balancer's pods and nodes' providerID — all reads, a part the caller may not read is "not known", never "missing".
- Dashboards (Roadmap Phase 68, `frontend/src/lib/dashboards`, `app/(shell)/dashboards`) add **no write path**: the panels are reads of the cluster's Prometheus through the existing `/api/v1/metrics/query[_range]` proxy (as the caller); a person's own dashboard and "Add to Grafana" are a ConfigMap created or updated through Phase 25's generic YAML apply (as the caller, audited as `apply-create`/`apply-update`), written as JSON strings so typed text can't change the document. Don't add a delete endpoint for them without raising it.
- Projects (Roadmap Phase 53, `internal/api/projects.go`) add **no write path**: a project is the label
  `vinaire.io/project` on namespaces, `GET /api/v1/projects[/{name}]` only read (as the caller), and the UI changes
  a project exclusively through Phase 25's YAML apply (labels, namespaces, ResourceQuota) and Phase 44's
  role-binding endpoints (members) — each already audited, each decided by the caller's own RBAC.
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

- Identity broker for Keycloak sign-in (Roadmap Phase 80, owner's decision of 2026-10-22; `internal/oidcbroker`, `internal/authn/broker.go`,
  `internal/api/keycloak.go`, chart `templates/users-namespace.yaml`) — the one place where Vinaire acts **with its own identity at
  sign-in**, because there is no caller yet. Kubernetes' API server accepts an OIDC issuer only over https (`url` and `discoveryURL` are
  both validated as https), so with a Keycloak on http it can't be made to trust the ID token. Instead, after Vinaire has verified the
  sign-in itself, `Broker.TokenFor` finds or creates **one ServiceAccount per person** (labelled `vinaire.io/oidc-sub=<sha256 of the
  OIDC issuer and subject>` and `managed-by: vinaire`, annotated with the issuer, named `oidc-<username>`) in **one dedicated namespace** (`<fullname>-users`,
  `OIDC_USER_NAMESPACE`) and asks the TokenRequest API for a short-lived token for it (lifetime = `SESSION_MAX_TTL` when set — the chart always sets it, `sessionMaxTTL`, 12h by default — otherwise 4 hours, `oidcbroker.DefaultTTL`; at least 10 min).
  The session holds that token instead of the ID token; the cluster sees `system:serviceaccount:<ns>:oidc-<name>` and **RBAC decides
  everything**. Rules to keep: the broker creates ServiceAccounts and tokens and **never a binding or a role** — a new account has no
  rights; Vinaire's Role (`<fullname>-user-accounts`) is a namespaced Role on `serviceaccounts` get/list/create/**patch** and
  `serviceaccounts/token` create **in that namespace only** (tested against a real cluster: it can't create an account elsewhere, read a
  Secret, list bindings or mint a token in `kube-system`); an account is used only if it carries both labels for this subject — a
  same-named account that isn't ours is never taken over (`-<hash>` suffix instead), and two accounts claiming one subject are refused;
  names are derived and validated (`FuzzAccountName`); **no other ServiceAccount may live in the users' namespace**, because Vinaire can
  mint a token for any account in it. **The identity is issuer + subject** (Roadmap Phase 82): the same subject under another Keycloak or realm is another person and gets
  another account (`TestTheSameSubjectUnderTwoIssuersIsTwoPeople`). Accounts made by 1.2.0–1.2.2, keyed by the subject alone, are **claimed
  once, by relabelling** (the `patch` above — name and RoleBindings stay), and only for the issuer Vinaire had configured when it first
  started with this code (`LegacyAdoptIssuer`, pinned in the sealed settings by `RestoreOIDC`); any other issuer never touches them, and
  claiming is audited as `oidc-account-adopted`. Deleting an account ends its tokens within seconds and a recreated account of the same name does
  not revive them. The mode is chosen per setting (`identityMode`: `serviceaccount` | `apiserver`; settings saved before keep
  `apiserver`): an **http Keycloak is refused in `apiserver` mode** (the wizard used to print `--oidc-*` flags Kubernetes would refuse,
  and every call came back 401), and `serviceaccount` is refused where Vinaire doesn't run in a cluster or has no users' namespace.
  `GET /api/v1/auth/me` (a read) says how the session is known (`kubernetesUser`, no token). Audited as `oidc-user-provisioned` and
  `oidc-token-issued` (account, subject, expiry — never the token). After sign-in the browser is sent to `FRONTEND_URL`, which is `/` whenever this process serves the UI (`STATIC_DIR`; the chart always
  sets it) — never the `localhost:3100` development default. The wizard offers the first person **sets of the chart's roles** (viewer, admin,
  rbac-admin…; `cluster-admin` only behind a warning), not `cluster-admin`. Not done: Keycloak groups (a ServiceAccount token carries none —
  roles are bound per account), refreshing a token while the Keycloak session lives (Roadmap Phase 84).
- Backups of an application installed from an image (Roadmap Phase 77b, owner's decision of 2026-10-21; `internal/backup`,
  `internal/k8s/backup_actions.go`, `internal/api/backups.go`) — all as the caller, so the cluster's RBAC decides:
  `POST /api/v1/instances/{namespace}/{name}/backups` creates a **dump** (a `Job`, or a `CronJob` with a `schedule`; one
  schedule per instance) or **VolumeSnapshots** of the instance's claims; `DELETE /api/v1/backups/{namespace}/{kind}/{name}`
  (`job`|`cronjob`|`snapshot`, body `{"confirmName": name}`) removes one backup object Vinaire made (label
  `app.kubernetes.io/managed-by: vinaire` + `vinaire.io/backup-of`; files already written to a claim or bucket stay).
  `GET /api/v1/instances/{ns}/{name}/backups` and `GET /api/v1/backups/config` are reads. Rules to keep: only PostgreSQL, MySQL,
  MariaDB, Redis and Valkey instances installed from an image are dumped, and the instance's **label and its image repository must
  agree**; the dump runs in the instance's **own image** with a **fixed script** — nothing typed is spliced into a script, values
  travel as validated environment variables; the database password is **never read by Vinaire**, the Job gets a `secretKeyRef` to
  the instance's Secret; pods run non-root, no privilege escalation, all capabilities dropped. A **PVC target** creates the claim
  `<instance>-backups` if it is missing (size validated, e.g. `5Gi`). An **S3 target** comes **only from the operator's
  configuration** (`BACKUP_TARGETS`, chart value `backup.targets`: https, no credentials, no loopback/link-local endpoint) — a
  request names a target and a Secret holding `AWS_ACCESS_KEY_ID`/`AWS_SECRET_ACCESS_KEY`, never an endpoint; the upload image is
  pinned. A backup carries no `app.kubernetes.io/instance` label, so deleting an instance never deletes its backups. Snapshots need
  the CSI snapshot CRDs and a VolumeSnapshotClass; otherwise the API says "unavailable". Audited as `backup-create` (kind, target,
  schedule — never a credential) and `backup-delete`. The chart ships the rights as the separate, unbound `<fullname>-lifecycle-admin`
  role (jobs/cronjobs get/list/create/delete, PVC get/list/create, volumesnapshots get/list/create/delete, volumesnapshotclasses
  get/list).
- Restore (Roadmap Phase 77c, `internal/backup/restore.go`, `internal/api/backups_restore.go`) — never over a running instance, as the
  caller: `POST /api/v1/instances/{namespace}/{name}/restore` loads a dump (`source`, optional `file`; empty = the newest) **into the
  instance {name}**, which must be another image-installed instance — the body must repeat its name (`confirmName`) and the source may
  not be the target. It is a `Job` in the target's own image, mounting the claim `<source>-backups` **read-only** (refused unless
  that claim carries `managed-by: vinaire` and `vinaire.io/backup-of: <source>`), signing in through a `secretKeyRef` to the target's
  Secret; the dump file name is validated (a plain name, no path). Redis/Valkey dumps are not loaded by a Job (refused: use a
  snapshot). `POST .../restore-snapshot` creates a **new** claim from a snapshot Vinaire took of that instance (`dataSource`; the
  snapshot's labels are checked) and touches no existing claim. Audited as `backup-restore` and `backup-restore-snapshot`. Restore Jobs
  carry `vinaire.io/restore-into`/`restore-of` (no instance label) and can be deleted through `DELETE /api/v1/backups/.../job/...`.
- Version upgrade of an instance installed from an image (Roadmap Phase 77d, `internal/instances/upgrade.go`,
  `internal/k8s/instance_upgrade.go`, `internal/api/instance_upgrade.go`) — as the caller:
  `POST /api/v1/instances/{namespace}/{name}/upgrade` (`tag`, `acceptMajor`, or `revert`; the body must repeat the instance's name)
  is one strategic merge patch of the StatefulSet: the `app` container's image and the annotation `vinaire.io/previous-image`.
  **Only the tag changes** — the repository is the running image's, a request never names an image; the tag is validated by a
  pattern; a digest-pinned image is refused; a change of major version (or a tag that cannot be compared, like `latest`) needs
  `acceptMajor`, and a **lower major version is refused** outright; `revert` restores the recorded image only if it is of the same
  repository. Charts are upgraded from the Marketplace (Phase 28), not here. The UI offers a dump first and waits for it. Audited as
  `instance-upgrade` / `instance-revert` (from, to, major). The chart's `-lifecycle-admin` role grants `patch` on StatefulSets.

Every other resource kind (Pods, ConfigMaps, Secrets, Ingress, ...) and
every other verb, on any cluster a user browses, stays read-only.

> **Reading is broad, writing is not.** The "All resources" page
> (`internal/api/allresources.go`, Roadmap Phase 39) *lists and gets* any kind the
> cluster's discovery shows the caller — as the caller, with strictly validated path
> parts, never touching Vinaire's own credentials. It writes nothing; editing goes
> through Phase 25's generic YAML apply above. A core Secret's values are redacted
> there, and apply refuses a Secret that still carries the `<redacted>` placeholder.

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
- **A new route** is named in `backend/router.go`; `TestEveryRouteNeedsASessionExceptTheSignInFlow` and
  `TestEveryStateChangingRouteIsRateLimited` (backend/router_test.go) fail if it skips the session or, being
  state-changing, the write rate limiter — unless it is added, with the reason, to the lists there — and
  `docs/api-routes.md` must be regenerated (`UPDATE_ROUTES=1 go test . -run TestRouteInventory`). Every handler
  that reads a request body bounds it with `http.MaxBytesReader` (a rule test enforces it). A new parser of outside
  text gets a `Fuzz*` test stating what it must never accept.
- **Add a test that fails if the property is lost**, and run `govulncheck`, `gosec -severity medium
  -confidence medium` and `npm audit` (the `Security` workflow does).

## Security checks (Phase 54, light) — read-only

`GET /api/v1/security/checks` (`internal/api/securitychecks.go`, engine in `internal/securitychecks`)
lists pods, services, ingresses, network policies, quotas, roles and bindings **as the caller** and
evaluates a fixed list of checks over that snapshot. It writes nothing and must stay that way: a
kube-bench-style scan needs a privileged Job on every node and is a deliberate decision of its own
(declined for now). A kind the caller can't list makes the checks that need it `unknown` — never
"pass". A new check is a function over `Snapshot` plus a test; don't add a code path that reads with
Vinaire's own identity.

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
configuration MUST call it first (a test lists every gated handler). Reading the audit
log (`GET /api/v1/audit`, Roadmap Phase 42) is gated the same way — it shows every
user's activity — by `get` on `audit.vinaire.io`. Otherwise:
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
        "endpoints",
        "ingresses",
        "ingressclasses",
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
  # Phase 45: history search reaches Loki through the API server's service proxy,
  # as the caller — only for Loki's own Services. The API server authorizes a service proxy
  # by "scheme:service:port" (what Vinaire asks for), so that is what resourceNames lists.
  - apiGroups: [""]
    resources: ["services/proxy"]
    resourceNames: ["http:loki:3100", "http:loki-gateway:80", "http:loki-read:3100", "http:loki-query-frontend:3100"]
    verbs: ["get"]
  # Phase 37: metrics are read from the Prometheus found in the cluster the same
  # way — as the caller, through the service proxy, for Prometheus's own names only.
  - apiGroups: [""]
    resources: ["services/proxy"]
    resourceNames: ["http:prometheus-server:80", "http:prometheus:9090", "http:prometheus-operated:9090", "http:kube-prometheus-stack-prometheus:9090", "http:vmsingle:8429", "http:prometheus-alertmanager:9093", "http:alertmanager:9093", "http:kube-prometheus-stack-alertmanager:9093"]
    verbs: ["get"]
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
  # Roadmap Phase 42: reading the audit log (the audit viewer).
  - apiGroups: ["vinaire.io"]
    resources: ["audit"]
    verbs: ["get"]
  # Roadmap Phase 47: setting up sign-in through Keycloak from the UI.
  - apiGroups: ["vinaire.io"]
    resources: ["auth"]
    verbs: ["get", "create", "update", "delete"]
  # Phase 29's Argo CD mode: listing Applications needs read on them (grant
  # the group like any other CRD above), "Sync now" a patch. Native mode needs
  # nothing here — it applies as the source's own ServiceAccount.
  - apiGroups: ["argoproj.io"]
    resources: ["applications"]
    verbs: ["patch"]
  # Phase 40's node lifecycle (cordon/drain/labels/taints/delete node) — the chart
  # ships this as the separate <fullname>-node-admin role.
  - apiGroups: [""]
    resources: ["nodes"]
    verbs: ["patch", "delete"]
  - apiGroups: [""]
    resources: ["pods/eviction"]
    verbs: ["create"]
  # Phase 44's RBAC management — the chart ships this as <fullname>-rbac-admin.
  - apiGroups: ["rbac.authorization.k8s.io"]
    resources: ["rolebindings", "clusterrolebindings"]
    verbs: ["create", "delete"]
  - apiGroups: ["authorization.k8s.io"]
    resources: ["subjectaccessreviews"]
    verbs: ["create"]
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
├── backend/                Go module: github.com/f1nn17/vinaire/backend
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
- Dark is the default theme; a light theme and a ☀ / Auto / ☾ switch exist
  (Roadmap Phase 56). Every colour comes from a token in `globals.css` (one set of values
  for each theme) — no hex/rgb in components; `npm run check:theme` fails on one, and
  on a text colour below WCAG AA (4.5:1) on its surface. Colours that a canvas needs
  (xterm) live in `src/lib/theme.ts`. The theme is a cookie, set on `<html data-theme>`
  by `public/theme-init.js` before first paint.
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

The Go module is `github.com/f1nn17/vinaire/backend`. If the actual GitHub
org/repo location changes, update `go.mod`'s module line and all internal
import paths together in one commit.
