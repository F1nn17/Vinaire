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
- `POST /api/v1/clusters` / `DELETE /api/v1/clusters/{name}` — cluster
  management via the web UI (Settings page): create/get/update on exactly
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
in this codebase): `internal/clusters.Entry.ClientsetForToken` builds a
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
        "events",
        "replicasets",
        "serviceaccounts",
        "persistentvolumeclaims",
        "persistentvolumes",
      ]
    verbs: ["get", "list", "watch"]
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
  # Phase 9's three write actions — omit this block for a read-only login.
  - apiGroups: ["apps"]
    resources: ["deployments"]
    verbs: ["patch", "delete"]
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
files or answers JSON/WebSocket on `/api/*`. `next dev` is the one
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

There is no test suite yet (see ROADMAP.md — this is pre-MVP). When adding
the first real feature, add tests alongside it rather than retrofitting
later.

## Git module path

The Go module is `github.com/shiraku/vinaire/backend`. If the actual GitHub
org/repo location changes, update `go.mod`'s module line and all internal
import paths together in one commit.
