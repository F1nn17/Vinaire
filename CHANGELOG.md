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
- **Roadmap Phase 10 (Authorization):** new `internal/authz` package —
  OIDC bearer-token verification (`github.com/coreos/go-oidc/v3`) plus a
  flat, config-backed group→namespace/action allow-list
  (`backend/policy.yaml`), gating every `/api/v1/*` handler. A specific
  namespace outside the caller's groups gets `403`; an all-namespaces list
  request instead comes back filtered to what the caller can see, rather
  than erroring outright. Every existing handler from Phases 1–9 was
  updated to authorize before touching the cluster, and Phase 9's audit
  log / Phase 6's secret-reveal audit log now record the caller's real
  username instead of just a source IP. Frontend: `next-auth`/Auth.js with
  a Keycloak provider, `src/proxy.ts` protecting every route (redirects to
  sign-in when logged out), a real Topbar user menu with sign-out, and a
  new throwaway, fully isolated dev Keycloak instance under `/keycloak`
  (docker-compose, pre-seeded realm/groups/`alice`+`bob` users) for local
  login testing. Verified end-to-end over curl with real Keycloak-issued
  tokens against the real `k3d-vinaire-dev` cluster: `alice`
  (`auth-viewers`, read-only) sees only the `auth` namespace and gets `403`
  on any other namespace or any write action; `bob` (`platform-admins`,
  read+write) sees everything and can restart a real Deployment. A live
  browser login couldn't be verified on this dev machine — a local,
  unrelated app (Proxifier) breaks Node's outbound connections including
  to `localhost`, unrelated to Vinaire's code (see AGENTS.md's frontend
  "Known local issue" and ROADMAP.md's Phase 10 outcome note for the full
  diagnosis and what to redo once it's fixed).
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
  over curl against two real clusters (`k3d-vinaire-dev` and a second
  `k3d-vinaire-dev-2` created for this phase): the same user's
  all-namespaces pod list correctly differs between clusters with no
  cross-cluster data leakage. Live-browser confirmation of the dropdown
  itself is blocked by the same local Proxifier issue as Phase 10.

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
