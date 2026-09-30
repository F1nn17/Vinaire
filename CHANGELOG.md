# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- Initial monorepo scaffold: `/frontend` (Next.js + TypeScript + Tailwind) and
  `/backend` (Go + client-go) with a placeholder dashboard page and a
  `/healthz` endpoint.
- Project documentation: README, AGENTS.md, ROADMAP.md.
- CI workflow for backend (`go build` + `go vet`) and frontend
  (`npm install` + `npm run build` + `eslint`).
- Frontend UI built out to match the design concept: dark theme, Sidebar/
  Topbar shell, and all 8 screens (Dashboard, Topology, Workloads, Pod/
  Deployment detail, ConfigMaps, Secrets, Network) wired to mock data behind
  an SWR-based data layer that mirrors the shape of the future backend API.
- Root `package.json` orchestrator: `npm run dev` runs backend and frontend
  together via `concurrently`.
- **Roadmap Phase 0 (dev infrastructure):** backend now has env-driven
  config (`internal/config`: `PORT`, `KUBECONFIG`, `CORS_ALLOWED_ORIGINS`),
  structured logging via `log/slog`, CORS restricted to the frontend origin,
  and graceful shutdown on `SIGINT`/`SIGTERM`. Frontend gained a shared
  `apiFetch` wrapper (`lib/api/http.ts`) and a live "backend: connected /
  unreachable" status pill in the Topbar, backed by a `useHealthz()` SWR
  hook polling `GET /healthz`.
- **Roadmap Phase 1 (Pods & Deployments, read-only):** new
  `GET /api/v1/{pods,deployments,statefulsets,daemonsets,jobs}?namespace=`
  endpoints (`internal/k8s` listers + `internal/api` handlers that derive
  status/color/age server-side). The Workloads page's five tabs now show
  real cluster data instead of `lib/mock/workloads.ts`, with a namespace
  filter input. Pod/Deployment detail routes moved from
  `/workloads/{pods,deployments}/[name]` to `[namespace]/[name]` to avoid
  name collisions across namespaces (detail data itself is still mocked
  until Phase 3). Verified end-to-end against a real local k3d cluster.
- **Roadmap Phase 2 (live updates via informers + WebSocket):** new
  `internal/stream` package — a `SharedInformerFactory` watches Pods/
  Deployments/StatefulSets/DaemonSets/Jobs across all namespaces and
  broadcasts added/modified/deleted diffs (reusing `internal/api`'s row
  mappers) to every connected client over `GET /api/v1/stream/workloads`
  (WebSocket, via `gorilla/websocket`). Frontend's new
  `useWorkloadsStream()` hook opens one WS connection per Workloads page
  visit and merges diffs straight into the active `useWorkloads()` SWR
  cache entry via `mutate(..., { revalidate: false })` — no polling, no
  reconnect on tab/namespace-filter changes. Verified end-to-end against a
  real k3d cluster: scaled a test Deployment up/down live and watched the
  Ready column and Pods table update within ~1s with zero page reloads,
  including catching a newly created pod mid-"Pending".
- **Roadmap Phase 3 (Pod detail: Overview + Events):** new
  `GET /api/v1/pods/{namespace}/{name}` returns real metadata, labels,
  per-container status, and CPU/memory usage — metrics-server was already
  installed on the target k3d cluster, so real `k8s.io/metrics` usage is
  wired directly (degrades to `"—"` if a cluster has no metrics-server,
  rather than failing the request). Real pod events now come from
  `clientset.CoreV1().Events()` with an `involvedObject` field selector,
  newest first. The YAML tab is also real (free once the pod object is
  fetched, via `sigs.k8s.io/yaml`); Logs/Terminal stay on mock/empty data
  until Phases 4/5. `NewClientset` now also returns the `*rest.Config` it
  built, so `internal/api` can construct the metrics client against the
  same cluster without re-resolving kubeconfig. Verified end-to-end against
  a real k3d pod with resource limits, including confirming non-ASCII
  characters (event metadata's "·" separator) render correctly end-to-end.
- **Roadmap Phase 4 (live logs):** new
  `GET /api/v1/pods/{namespace}/{name}/logs` — a dedicated point-to-point
  WebSocket (`internal/stream.ServeLogs`, separate from Phase 2's broadcast
  `Hub`) streaming one container's `kubectl logs -f` equivalent output live.
  Requests `PodLogOptions.Timestamps` so each line gets a real per-line
  time; log level is a best-effort `error`/`warn`/info keyword heuristic
  (arbitrary container output has no structured level to parse). Frontend's
  `useLogsStream()` hook + new `LogsTab` component add a container picker
  (for multi-container pods) and a Follow/Pause toggle that actually
  opens/closes the WebSocket, with autoscroll that backs off once the user
  scrolls up and a 1000-line render cap. Verified end-to-end against a real
  k3d pod emitting timestamped ticks every 2s: compared side-by-side with
  `kubectl logs -f`, confirmed Pause genuinely stops new lines and Follow
  resumes them, zero console errors.
- **Roadmap Phase 5 (Terminal/exec):** new
  `GET /api/v1/pods/{namespace}/{name}/exec` — a WebSocket bridge
  (`internal/stream.ServeExec`) to a real TTY shell inside the container via
  `remotecommand.NewSPDYExecutor`, with small adapters translating between
  the WebSocket and the exec stream's stdin/stdout/stderr/resize. Frontend's
  `TerminalTab` replaces the old scripted `TerminalPane` demo with a real
  `xterm.js` terminal (`@xterm/xterm` + `@xterm/addon-fit`), forwarding
  keystrokes and resize events over the socket. Verified end-to-end against
  a real k3d pod: `whoami` and `ls /` in the UI matched `kubectl exec`
  byte-for-byte, and a distroless/no-shell pod
  (`registry.k8s.io/pause:3.9`) surfaces a clear "no such file or directory"
  error in the terminal instead of hanging. Shipped without an auth gate —
  deliberate, accepted for local-dev-only use (see ⚠ note in ROADMAP.md);
  Phase 10 needs to close this properly before any non-local use.
- **Roadmap Phase 6 (ConfigMaps & Secrets):** new
  `GET /api/v1/configmaps?namespace=` (values included — ConfigMap data
  isn't sensitive) and `GET /api/v1/secrets?namespace=` (metadata + key
  *names* only, values never included, not even masked) endpoints, plus
  `POST /api/v1/secrets/{namespace}/{name}/reveal` — the only path that
  ever returns a decoded secret value, audit-logged on every call
  (namespace/secret/key/remote address — never the value) with
  `Cache-Control: no-store`. Frontend's ConfigMaps/Secrets pages swap mock
  data for real fetches; Secrets' "Show" button now calls the reveal
  endpoint per key with a loading state, keeping decoded values in local
  component state only (never the SWR cache) so switching secrets re-masks
  them. Both pages gained a namespace filter. Verified end-to-end against a
  real k3d cluster: confirmed via the raw network response body that
  `GET /api/v1/secrets` contains no plaintext anywhere (checked against
  real cluster secrets too — TLS keys, Helm release data, node-password
  hash — not just the test one), watched the audit log line appear the
  instant "Show" was clicked, and confirmed the revealed value matches what
  was actually stored.
- **Roadmap Phase 7 (Topology graph):** new `GET /api/v1/topology?namespace=`
  computes the whole graph server-side from owner references and label
  selectors — Ingress→Service (backend service name match), Service→Pod and
  Deployment→Pod (selector match, skipping the ReplicaSet hop),
  Deployment→ConfigMap/Secret (scanned from the pod template's envFrom/
  env/volumes). ConfigMap/Secret nodes get an "In use"/"Unused" status from
  a post-pass over the collected edges. Frontend's `TopologyNode`/
  `TopologyData` types changed from a fixed 7-node shape to a real
  `nodes[]`/`edges[]` graph; the Topology page got a full rewrite with a
  manual layered-column layout (one row per node kind, empty rows
  skipped, nodes spread evenly within a row) instead of 7 hardcoded pixel
  positions, plus a namespace input replacing the decorative "namespace:
  auth" button. Verified end-to-end against a real k3d namespace with a
  full Ingress→Service→Deployment(2 pods)→ConfigMap+Secret chain — traefik
  picked up the Ingress and reported it Active, and the auto-created
  per-namespace `kube-root-ca.crt` ConfigMap correctly showed "Unused"
  (nothing references it) right next to the real ConfigMap/Secret showing
  "In use" — then switched to `kube-system` and confirmed a completely
  different shape rendered correctly (no Ingress row at all, 3 services, 4
  deployments, 7 pods), zero console errors either time.
- **Roadmap Phase 8 (Ingress / Gateway API):** new
  `GET /api/v1/ingresses?namespace=` with *real* TLS validity checking —
  fetches each referenced Secret, parses the actual `tls.crt` certificate,
  and reports "Valid" / "Expiring in Nd" / "Expired" from its real
  `NotAfter`, not just "a tls block exists". New
  `GET /api/v1/gateway-routes?namespace=` via `sigs.k8s.io/gateway-api`'s
  typed client, returning `{ available: false, reason }` instead of a 500
  or empty list when the CRDs aren't installed (detected via
  `apierrors.IsNotFound` on the `List()` call). Frontend's `NetworkPage`
  swaps mock data for real fetches on both tabs, with a namespace filter
  and a dedicated "Gateway API isn't available" panel for the unavailable
  case. Verified end-to-end: two self-signed certs (365-day and 5-day) came
  back "Valid" and "Expiring in 4d" respectively; and — since this cluster
  turned out to already have Gateway API CRDs installed — spun up a second,
  genuinely vanilla throwaway k3d cluster specifically to verify the
  "not installed" path for real, then tore it down.
- **Roadmap Phase 9 (Write actions: Restart / Delete / Rollback):** new
  `POST /api/v1/deployments/{namespace}/{name}/restart` (strategic merge
  patch of the pod template's `kubectl.kubernetes.io/restartedAt`
  annotation), `DELETE /api/v1/deployments/{namespace}/{name}`, and
  `POST /api/v1/deployments/{namespace}/{name}/rollback` (finds the target
  `ReplicaSet` by its `deployment.kubernetes.io/revision` annotation and
  patches the Deployment's `spec.template` from it) — the three direct
  `client-go` write actions the architecture pivot below named as the
  deliberate exception to the read-only rule. Every call is
  structured-logged (`slog.Warn("write action", ...)`) with the action,
  namespace, name, caller address, and (for rollback) the target revision.
  Frontend gained reusable `ConfirmModal`/`Toast` components; the
  Deployment detail page's Restart/Delete/Rollback buttons are now real,
  each behind a confirmation modal and followed by a success/error toast,
  with Delete redirecting to `/workloads` on success. Verified end-to-end
  against `k3d-vinaire-dev` with a real 3-revision Deployment: Restart
  triggered a real rollout, Rollback reverted `spec.template` to revision
  1's image, and Delete removed the object — all three confirmed via the
  backend's audit log and `kubectl`, with zero browser console errors.
- **Roadmap Phase 10 (Authentication + single image):** `internal/authn` —
  three login modes via `AUTH_MODE`: `token` (default; paste a Kubernetes
  ServiceAccount token, session TTL read from its own JWT `exp` claim),
  `oidc` (a real Authorization Code + PKCE exchange against Keycloak,
  entirely server-side in this Go process — never in the frontend), and
  `disabled` (no login, local k3d dev only). The browser only ever holds an
  opaque, HttpOnly `vinaire_session` cookie; every Kubernetes API call runs
  as the *caller's own* identity (`internal/clusters.Entry.ClientsetForToken`),
  so authorization is whatever the target cluster's real RBAC decides — a
  denied call now correctly comes back as `403`/`401` from the backend
  (`internal/api.respondK8sError` maps the underlying `apierrors` instead of
  flattening every failure to `502`). Frontend: a `/login` page (token paste
  or a Keycloak redirect, depending on `/api/v1/auth/config`), and — since
  the frontend is now a static export with no server of its own (see
  below) — every backend call goes out cross-origin with
  `credentials: "include"`, using a `SameSite=None; Secure` cookie
  (confirmed working over plain `http://localhost` in a real browser,
  which treats `localhost` as a secure context). Verified end-to-end with a
  real, RBAC-scoped (not cluster-admin) ServiceAccount against
  `k3d-vinaire-dev`, and with a real Keycloak login through the dev
  instance in `/keycloak`.
  **Single combined image:** a root `Dockerfile` (multi-stage: Node builds
  the frontend, Go builds the backend, the final layer is
  `gcr.io/distroless/static-debian12:nonroot`) — `docker build .` produces
  one ~50 MB image with no shell, Node, or npm in it; `docker run` serves
  the API and the static frontend from the same port (`STATIC_DIR`, see
  `backend/static.go`), verified directly against a running container.
  **Rendering model:** the frontend now builds with `output: "export"`
  (Next.js used purely as a build tool — see AGENTS.md's "Rendering model")
  ; the two resource-detail pages moved from `[namespace]/[name]` dynamic
  route segments to `?namespace=&name=` query params, since a static export
  can't pre-render a path for a resource name it doesn't know at build
  time. This design replaces an earlier, briefly-shipped version of this
  phase that used a Keycloak-only OIDC bearer-token flow with an app-level
  `internal/authz` policy layer and `next-auth` — removed as part of this
  change, along with the local-Proxifier-networking workaround that
  version needed (moving OIDC server-side removed the only Node code path
  that ever made an outbound call, so there's nothing left for it to break).
- **Roadmap Phase 11 (Multi-cluster):** new `internal/clusters.Registry` —
  one client set per configured kubeconfig context (`CLUSTER_CONTEXTS`),
  selected per-request via an `X-Cluster` header (`Handler.resolveCluster`,
  defaulting to the first configured cluster). New `GET /api/v1/clusters`
  lists them. Chose to start a live-update informer hub eagerly for every
  configured cluster (not lazily per-selection) per the phase's own risk
  note. Frontend: a real Topbar cluster dropdown backed by that endpoint,
  a `lib/clusterStore.ts` (localStorage-backed) read synchronously by
  `apiFetch`/`buildStreamURL` outside of React, every SWR hook's cache key
  and every WebSocket URL (workload stream, logs, exec) now includes the
  selected cluster so switching it refetches/reconnects cleanly. Verified
  against two real clusters (`k3d-vinaire-dev` and a second
  `k3d-vinaire-dev-2`), both over curl and live in a real browser after
  Phase 10's rework: switching the Topbar dropdown swaps the Workloads
  table over to the other cluster's own real data with no stale rows
  lingering.
- **Roadmap Phase 12 (StatefulSet / DaemonSet / Job detail pages):** new
  `GET /api/v1/{statefulsets,daemonsets,jobs}/{namespace}/{name}` endpoints
  (mirroring Phase 3's Pod-detail `Get()` + events + YAML pattern, minus the
  CPU/Memory/Containers fields that don't naturally apply to a controller
  object). `StatefulSetRow`/`DaemonSetRow`/`JobRow`'s `Href` now point at
  real detail pages instead of the `"/workloads"` placeholder. Frontend:
  three new query-param-routed detail pages
  (`/workloads/{statefulsets,daemonsets,jobs}/detail?namespace=&name=`),
  following the same static-export routing convention as Pod/Deployment
  detail. Jobs are scoped to plain `batch/v1` `Job` objects only, not
  `CronJob`. Verified end-to-end against `k3d-vinaire-dev`, including a
  live browser click-through from the Workloads list to a real StatefulSet's
  detail page.
- **Roadmap Phase 13 (Storage: PVC / PV / StorageClass):** new
  `GET /api/v1/{pvcs,pvs,storageclasses}` list endpoints and
  `GET /api/v1/pvcs/{namespace}/{name}` detail (metadata, real provisioning
  events, YAML, and which pods mount it — a pod-spec scan for
  `volumes[].persistentVolumeClaim.claimName`, the same style Phase 7's
  topology graph uses for ConfigMap/Secret references). PVCs are namespaced;
  PVs and StorageClasses are cluster-scoped — the first cluster-scoped
  resource kind in the app, so their list pages correctly have no namespace
  filter. "Near capacity" is requested/provisioned capacity, not real
  bytes-used (no metrics source for that yet — Phase 16's territory).
  Frontend: a new sidebar "Storage" section, a tabbed list page for all
  three kinds, and a PVC detail page linking each mounting pod to its own
  real Pod detail page. Verified end-to-end against `k3d-vinaire-dev`,
  including a live browser click-through from the PVC list through to a
  mounting pod's own detail page.
- **Roadmap Phase 14 (RBAC viewer, read-only):** new
  `GET /api/v1/{roles,clusterroles,rolebindings,clusterrolebindings,serviceaccounts}`
  list endpoints and `GET /api/v1/rbac/summary?namespace=` — a static
  "what's configured" rollup correlating each namespace's RoleBindings
  against the rules their referenced Role/ClusterRole actually grants
  (never a live SubjectAccessReview). Role/RoleBinding/ServiceAccount are
  namespaced; ClusterRole/ClusterRoleBinding are cluster-scoped, so those
  two tabs correctly have no namespace filter. Frontend: a new sidebar
  "Access control" section with six tabs (five list tabs plus "Who can do
  what"). Verified end-to-end against `k3d-vinaire-dev` with a throwaway
  Role/RoleBinding/ServiceAccount and the real `vinaire-viewer` ClusterRole/
  ClusterRoleBinding from Phase 10 — the summary correctly resolved a test
  binding down to its role's exact rule.
- **Roadmap Phase 15 (Custom Resources — generic CRD viewer):** new
  `GET /api/v1/crds` (discovers every installed CRD via
  `k8s.io/apiextensions-apiserver`'s typed clientset),
  `GET /api/v1/crds/{crdName}/instances?namespace=`, and
  `GET /api/v1/crds/{crdName}/instances/{name}?namespace=` — fully generic,
  driven by `k8s.io/client-go/dynamic` against each CRD's own storage
  version, with no hardcoded per-CRD Go code. Refactored `resolveCluster`
  into a shared `resolveRESTConfig` plus a new `resolveDynamic`, so the
  dynamic/apiextensions clients authenticate as the caller's own identity
  exactly like every typed-clientset handler already does. Frontend: a
  dynamic sidebar "Custom Resources" section built from the discovered CRD
  list itself (no static nav entries), a generic instance list (name/
  namespace/age — not each CRD's own printer columns), and a YAML detail
  view reusing the existing `CodeBlock` pattern. Verified end-to-end
  against `k3d-vinaire-dev`'s real installed CRDs (33 of them, no
  throwaway test CRD needed) — a namespaced CRD with real instances
  (`HelmChart`) and a cluster-scoped CRD with none (`GatewayClass`) both
  behaved correctly, confirmed via a live browser click-through from the
  dynamic sidebar to a real YAML view matching `kubectl get ... -o yaml`.
- **Roadmap Phase 16 (Prometheus / Loki / Alertmanager integration):** new
  `GET /api/v1/metrics/{query,query_range}`, `GET /api/v1/alerts`, and
  `GET /api/v1/pods/{namespace}/{name}/logs/search?query=&since=` — plain
  HTTP proxies to an already-running observability stack, configured via
  `PROMETHEUS_URL`/`LOKI_URL`/`ALERTMANAGER_URL` (all optional; Vinaire
  never installs or manages these itself). Any unconfigured or unreachable
  integration responds with an explicit `{"available": false, "reason"}`
  instead of erroring — the same pattern Phase 3 (metrics-server) and
  Phase 8 (Gateway API) already use. Frontend: the Topbar bell is now a
  real dropdown sourced from Alertmanager, and the Pod detail Logs tab
  gained a "History" mode (LogQL search via Loki) alongside the existing
  live tail. Dashboard CPU/Memory/Pods-down wiring is deliberately left to
  Phase 18, per that phase's own scope note. Verified both the
  unconfigured (`available: false`, no crash) and configured paths — the
  latter against minimal mock HTTP servers replaying real Prometheus/Loki/
  Alertmanager response shapes — including a live browser click-through
  showing real mock alerts in the Topbar bell and a real pod's historical
  logs correctly parsed and level-highlighted.
- **Roadmap Phase 17 (Production hardening):** the three Phase 9 write
  endpoints are now rate-limited per logged-in identity (1/sec sustained,
  burst 5, real `429` past that — never a silent drop); Phase 9's writes
  and Phase 6's secret reveal now go through one shared `Handler.auditLog`
  that durably records every action (`internal/audit`, rotated/compressed,
  10MB/5 backups/30 days) in addition to the existing stdout log line, so
  the trail survives a pod restart; and a Forbidden response's message is
  now Kubernetes' own clean explanation instead of this backend's internal
  wrapping prefix tacked in front of it. Frontend: a new shared
  `TableSkeleton`/`ListSkeleton` replaced every list page's bare
  "Loading…" text (wired into `DataTable` itself, plus the two
  `ListDetailSplit` pages), and every list's empty state is now specific
  (e.g. `No deployments found in namespace "x".`) rather than generic.
  `Toast`/`ConfirmModal` turned out to already be properly shared
  components — nothing to change there. Verified end-to-end against
  `k3d-vinaire-dev`: real `429`s after a burst of 5, audit entries
  surviving a backend process restart, a real RBAC-restricted
  ServiceAccount getting the clean Forbidden message, and a real browser
  showing both the loading skeleton and a specific empty state.
- **Roadmap Phase 18 (Dashboard / Overview — finishing with real data):**
  new `GET /api/v1/dashboard/summary` (pods-down/healthy-pods counts and
  per-namespace pod counts, aggregated from the same pod list Phase 1
  already fetches — no new Kubernetes API surface) and
  `GET /api/v1/events?namespace=&limit=` (a general recent-events feed).
  ArgoCD detection reuses Phase 15's generic-CRD discovery/dynamic-list
  machinery against one specific, well-known CRD
  (`applications.argoproj.io`) to report both whether ArgoCD is installed
  and how many Applications are `OutOfSync`. PVC-near-capacity and CPU/
  Memory stay explicit frontend-side "not available yet" stubs — Phase
  13/16 built the real data sources, but wiring them into the Dashboard
  is intentionally out of this phase's scope. `StatCardData.value` is now
  `number | null`, rendered as a visibly muted "—" rather than a bold
  number, so "unavailable" can never be confused with a real 0; the
  Drifted-from-Git card hides entirely (not a zero) when ArgoCD isn't
  detected. This retires the last of the mock dashboard data
  (`lib/mock/dashboard.ts` deleted). Verified end-to-end against
  `k3d-vinaire-dev`, including through a real cluster restart (correctly
  reflecting pods transiently down during recovery, settling to match
  `kubectl get pods -A` exactly) and a throwaway ArgoCD-like CRD with a
  `Synced` and an `OutOfSync` Application, confirmed both installed
  (correct drifted count) and removed (card fully absent) in a live
  browser. Along the way, fixed a real bug: an empty Go slice declared
  with `var x []string` marshals to JSON `null`, not `[]`, which crashed
  the frontend's `.length` check — fixed with `make([]string, 0, n)`.
- **Roadmap Phase 24 (Cluster management via the web UI):** connect a
  second (or third, ...) cluster entirely from the Settings page — no
  editing Deployment env vars, no mounting a kubeconfig Secret by hand, no
  redeploy. `internal/clusters.Registry` gained `AddDynamic`/`Remove`
  (mutex-guarded — it previously had none, safe only because nothing
  mutated it after startup) and a lifecycle hook that starts/stops a
  cluster's live-update informers (`internal/stream.HubRegistry`, also
  newly mutex-guarded); persistence is a Secret (`vinaire-clusters`) in
  this backend's own namespace, written with its own in-cluster identity
  via a dedicated, narrow `Role`/`RoleBinding` — a genuinely new write
  path, flagged and confirmed with the project owner first per AGENTS.md's
  core rule, and clearly distinct from Phase 9's three (this is Vinaire
  managing its own config, not a per-user action on a browsed cluster's
  resources). `POST /api/v1/clusters` verifies submitted credentials
  against a real API call before persisting anything. Verified end-to-end:
  added a real second cluster live through the UI, saw its distinct real
  data immediately after a Topbar cluster switch, survived a full pod
  restart with zero reconfiguration, and removed cleanly through the same
  UI. Also fixed two real bugs surfaced along the way: `apiFetch` was
  showing raw `{"error":"..."}` JSON blobs as error text everywhere (now
  parsed into a clean message, plus a new `friendlyErrorMessage` helper
  mapping status codes to human copy, wired into all sixteen list/detail
  pages); and `vinaire-viewer`'s `ClusterRole` had never been updated past
  Phase 10's original resource list, so Phases 8/13/14/15's endpoints were
  all silently 403ing on the real in-cluster deployment — fixed in both
  AGENTS.md's documented example and the live cluster.
- **Roadmap Phase 19 (Nodes: cluster machine list + detail):** new
  `GET /api/v1/nodes` and `GET /api/v1/nodes/{name}`, a new "Nodes" sidebar
  entry, a nodes table (Name, Status, Roles, CPU/Memory, Age), and a detail
  page (Capacity/Allocatable, Conditions, Taints, scheduled pods via
  `spec.nodeName`). Live updates reuse the existing informer/hub/WebSocket
  pipeline — `internal/stream`'s `Event.Row`/`registerInformer` were
  generalized from a hardcoded `WorkloadRow` to a generic type parameter so
  Nodes' differently-shaped row fits without a parallel pipeline; the
  frontend does a partial-field merge on live diffs (not a full replace)
  so a status-only event like a cordon doesn't blank out the last known
  CPU/Memory reading. Verified against `k3d-vinaire-dev`: `kubectl cordon`/
  `uncordon` flips the list's Status live with no reload, exactly this
  phase's Definition of done.
- Fixed a batch of issues found alongside Phase 19: Phase 15's Custom
  Resources sidebar section (33 entries) moved into Settings as its own
  tab instead of cluttering the main nav; `vinaire-viewer`'s ClusterRole
  was missing the `metrics.k8s.io` API group entirely, so every
  metrics-server call (Pod detail, the Dashboard, this phase's Node usage)
  was silently 403ing and degrading to "—" — fixed in both AGENTS.md's
  example and the live cluster, and the Dashboard's Cluster CPU/Memory
  cards wired to real metrics-server data as a result (switched from a
  planned Sparkline to a percentage bar, since there's no historical time
  series to draw a real line from); the Dashboard's Namespaces card no
  longer stretches to match the Events column's height; Healthy pods now
  precedes Pods down; Settings gained a Russian-language "Справка" tab
  walking through adding a cluster; Deployment detail was still 100% mock
  data (`Deployment "default/other" not found in mock data"`) despite
  Phase 9's write actions being real — replaced with a real
  `GET /api/v1/deployments/{namespace}/{name}`; and the Gateway API tab
  crashed the whole page on a cluster with zero Gateways/HTTPRoutes,
  caused by Go's `omitempty` dropping an empty (but non-nil) slice's JSON
  key entirely — fixed on that field and two others with the same latent
  bug (Alertmanager's `alerts`, Loki search's `lines`).
- **Roadmap Phase 20 (Global search / ⌘K):** the topbar's previously
  decorative search field is now a real command palette — `GET
  /api/v1/search?q=` substring-matches on name across Pods, Deployments,
  StatefulSets, DaemonSets, Jobs, Nodes, and PVCs (deep-linking to their
  real detail pages) plus ConfigMaps/Secrets/Ingresses (to their list
  pages), capped at 50 results. Opens via Ctrl/Cmd+K from anywhere or by
  clicking the search field; arrow keys move the selection, Enter
  navigates and closes it, Escape closes it.
- Rewrote Settings' "Справка" (help) tab after feedback that it read like
  internal dev notes: dropped the bare "Phase 15" reference, added a plain
  explanation of what Custom Resources actually are and what an empty
  list vs. an access error means, split it into a two-item side nav
  ("Подключение кластера" / "Кастомные ресурсы"), and rewrote the
  cluster-connection steps to cover multiple local clusters (`kubectl
  config get-contexts`/`use-context`), choosing or creating a namespace,
  the complete ready-to-apply ClusterRole YAML (not a "customize this
  example" framing) alongside a clearly-labeled `cluster-admin` shortcut
  for disposable local clusters, and finding the API server address.
- **Roadmap Phase 21 (Helm chart):** an installable chart under
  `/charts/vinaire` — Deployment/Service/ServiceAccount/ClusterRole/
  ClusterRoleBinding, plus a Role/RoleBinding scoped to the one Secret
  Phase 24's cluster-management feature needs. `values.yaml` switches
  `authMode` (token/oidc/disabled) and exposes `rbac.extraRules` for a
  cluster's own installed CRDs. Added two more Settings -> Справка
  sections: "Вход по логину и паролю" (connecting Keycloak/OIDC) and
  "Метрики, логи, алерты" (connecting Prometheus/Loki/Alertmanager, and
  clarifying Grafana isn't integrated at all). Verified with a real
  `helm install` on `k3d-vinaire-dev`: token login and a real pod list
  worked from a clean namespace with zero manual manifest edits.
- Fixed two real bugs found while building the chart's RBAC from scratch,
  both silently broken on the actual deployed instance the whole time:
  every WebSocket endpoint (Pod logs, Pod terminal, Phase 2's live table
  updates) 403'd because `originChecker` never exempted a same-origin
  request, only the `CORS_ALLOWED_ORIGINS` allowlist (which defaults to
  the local dev frontend's origin, never overridden on the real
  deployment) — masked because plain `fetch()` calls are naturally
  same-origin and unaffected; and `pods/log`/`pods/exec` (separate RBAC
  subresources from `pods` itself) were never granted at all, so Logs and
  Terminal got an explicit 403 straight from Kubernetes even once the
  WebSocket connected. Both fixed and confirmed live: real streamed nginx
  logs and a working in-pod shell.
- **Roadmap Phase 22 (Capacity planning):** `GET /api/v1/capacity` — total
  Requests vs. Allocatable (CPU, memory, pod count) per node and
  cluster-wide, as its own `CapacityUsage` type distinct from the existing
  metrics-server-backed one, to keep "what's requested" and "what's
  actually running" from being conflated. Shows as a new "Capacity" tab on
  the Nodes page, color-coded (warn at 80%, err at 95%). Fixed a real bug
  found while building it: constructing a `resource.Quantity` from an
  already-millicore value with `NewQuantity` instead of `NewMilliQuantity`
  made every CPU figure 1000x too high. Verified end-to-end: a real
  throwaway pod requesting 10Gi memory turned that node's memory bar
  yellow live in the browser, then cleaned up.
- Fixed three more issues: the Dashboard's Recent events list could drag
  the whole page down instead of scrolling on its own (no bounded height
  for its `overflow-auto` to clip against — fixed with a fixed max-height
  on the card); the cluster picker showed the literal word "Default"
  instead of anything real (added a `CLUSTER_NAME` env var, defaulted to
  the Helm release name, plus auto-derived the kubeconfig current-context
  name as a fallback for local dev). Topology and the four "Create"
  buttons were investigated and left as-is: Topology turned out to already
  show real live data (Phase 7) — it just looked sparse because the dev
  cluster has little deployed — and the "Create" buttons are inert mockup
  leftovers with no handler at all, which the project owner asked to leave
  alone for now rather than deciding a new write path on the spot.
- **Roadmap Phase 23 (Automated tests against a real k3d cluster in CI):**
  `backend/integration/` — real HTTP requests against a real running
  backend pointed at a real cluster (never mocked client-go), behind a
  `go:build integration` tag so it's invisible to the normal build/test
  path; fixture manifests covering one of each major workload kind plus
  Service/ConfigMap/Secret/PVC/Ingress. `frontend/e2e/` — a Playwright
  smoke test doing a real token login and clicking through every sidebar
  section. `.github/workflows/ci.yml` gained a PR-only `integration` job
  that stands up k3d, applies the fixtures, runs both suites, and tears
  the cluster down. Verified for real: ran the full backend suite against
  `k3d-vinaire-dev` (17/17 pass), deliberately broke the namespace filter
  to confirm this phase's own Definition of done (immediate, specific
  failure), caught two real bugs in the test suite itself along the way,
  ran the e2e suite against a live token-mode backend, and dry-ran the
  CI job's k3d cluster setup on a genuinely fresh cluster.
- **Roadmap Phase 33 (Scale and a consolidated resource action bar):**
  four new write actions extending AGENTS.md's core rule beyond Phase 9's
  original three and Phase 24's cluster management — Deployment/
  StatefulSet Scale (via the Scale subresource's `GetScale`/`UpdateScale`,
  not a direct `spec.replicas` patch), Deployment Pause/Resume rollout,
  and pod-level Delete (owned pods effectively restart once their
  controller recreates them; bare pods are gone for good —
  `PodDetailResponse.HasController` is what the frontend uses to show
  "Restart" vs. "Delete"). Every detail page's actions (Scale, Restart,
  Pause/Resume, jump-to-Revisions, Delete) are now a compact icon toolbar
  instead of scattered text buttons. `vinaire-viewer`'s ClusterRole
  (AGENTS.md's example and the live cluster) gained the new subresource
  grants this needed. Verified end-to-end: scaling 1→3 replicas matched
  `kubectl get deployment` within seconds; pausing a rollout and then
  changing the image via `kubectl set image` left every pod on the old
  image until Resume was clicked; an owned pod's action bar showed only
  "Restart" (confirmed it gets recreated), a bare pod's showed only
  "Delete" (confirmed gone via `kubectl get pod` returning NotFound); all
  six write calls landed in the audit log.
- Full YAML editor — create and edit (Roadmap Phase 25). Every detail page's
  YAML tab (Pod, Deployment, StatefulSet, DaemonSet, Job, Node, PVC, custom
  resource instance) is now an editable Monaco editor (bundled with the app,
  not loaded from a CDN); "Apply" first runs a server-side dry-run through
  the Kubernetes API itself, shows a before/after diff, and only a second
  click performs the real write. The "+ Create" buttons (Workloads,
  ConfigMaps, Secrets, Network) open the same editor pre-filled with a
  template. New endpoints `POST /api/v1/resources/dry-run`,
  `POST`/`PUT /api/v1/resources` work for any resource kind the cluster
  serves — the project owner's explicit choice over an allowlist, so
  AGENTS.md's write section and example ClusterRole were extended; the
  caller's own RBAC remains the real limit. Strict field validation rejects
  typo'd fields, Secret values are redacted from diffs, the diff is pinned
  to a `resourceVersion` so busy resources don't spuriously conflict, and
  every real write is audited. The Helm chart's ClusterRole also gained the
  Phase 33 scale/pod-delete grants it had missed.
- Create via a form, with a YAML toggle (Roadmap Phase 31). The "+ Create"
  modal opens in Form mode for Deployment (name, namespace, image, replicas,
  port, env vars, CPU/memory requests and limits, optional node from the
  Phase 19 node list), ConfigMap, Secret (values masked) and Namespace; a
  Form/YAML segmented control switches between the form and the generated
  manifest in Monaco. YAML -> Form only succeeds if the form can represent
  the manifest exactly (read it into form state, serialize back, require
  equality) — otherwise switching is refused with "too complex to edit as a
  form" and nothing is lost. Everything after that is Phase 25's unchanged
  dry-run -> diff -> apply. New read-only `GET /api/v1/forms/{resourceType}`
  serves the hand-written field schemas; no new write path.

### Changed

- Replaced the draft `ROADMAP.md` with a detailed, phase-by-phase technical
  plan organized as vertical slices (one working end-to-end feature per
  phase, each with a concrete Definition of done, dependency list, and
  complexity/risk notes) instead of a horizontal frontend/backend split.
- **Architecture pivot (project owner decision):** ROADMAP.md's Phase 9
  (Restart/Delete/Rollback) no longer goes through a GitOps-PR flow —
  it's now direct `client-go` writes (annotation/template patches and a
  plain `Delete()`) against the live cluster, scoped to exactly those
  three Deployment actions. AGENTS.md's core rule was updated to match:
  the UI is still read-only by default, but these three actions are now
  named as the explicit, audited exception. Implemented and verified — see
  the Phase 9 entry under Added above.
