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

### Changed

- Replaced the draft `ROADMAP.md` with a detailed, phase-by-phase technical
  plan organized as vertical slices (one working end-to-end feature per
  phase, each with a concrete Definition of done, dependency list, and
  complexity/risk notes) instead of a horizontal frontend/backend split.
