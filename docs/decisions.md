# Key decisions

Short record of decisions that shape the project. What is already specified in
[AGENTS.md](../AGENTS.md) (write list, auth modes, rendering model, security checklist) is not repeated here.
The phase list and its order live only in [ROADMAP.md](../ROADMAP.md).

## Product

- Vinaire is a web UI for Kubernetes — an alternative to Rancher and Headlamp, with the emphasis on a convenient UI.
  A solo project, one monorepo: `/frontend`, `/backend`.
- Stack: Next.js (App Router) + React + TypeScript + Tailwind; Go + client-go.

## Design

- Dark theme, accent royal purple `#8B5FBF`, fonts JetBrains Mono + IBM Plex Sans (self-hosted, see AGENTS.md).
- Sidebar groups: Cluster / Workloads / Configuration / Network.
- The 8-screen concept: https://claude.ai/artifact/6XXGiELv94PGyju7g2F6M6 (if it does not open, follow the
  existing `/frontend`).

## Delivery

- One Docker image (multi-stage, non-root): Go serves the static export and the API on one port.
- In development `npm run dev` and `go run .` run separately.
- Keycloak is optional and always a separate pod; it is never embedded in the image.

## Data

- Live updates come from informers plus a stream (WebSocket/SSE).
- Metrics go through a proxy to Prometheus. The Metrics tab (Phase 32) is a step chart of CPU / Memory / Network /
  Filesystem with a period, resolution and Pause, like Headlamp's; it is added **next to** Overview, never replaces it.
  Installing the metrics stack in one button is Phase 37 (Helm SDK).
- Informer caches: data is filtered by the user's rights (SelfSubjectAccessReview) before it is returned, and Secret
  values are never held in a cache.

## GitOps

- Separate from the Phase 9 direct writes. Modes: `argocd` | `native` | `off` (Phase 29).

## Environments

- Production: k3s on a VPS; the target also includes a full Kubernetes (kubeadm).
- Local: k3d (`k3d-vinaire-dev`) or Docker Desktop Kubernetes.
- The home server sits behind NAT, on a private self-hosted NetBird network.

## Lifecycle of applications (Phase 77, decided 2026-10-21)

- All four directions at once: the "instance" and safe delete, backups, restore, version upgrade — built as sub-phases 77a–77d.
- Vinaire may create VolumeSnapshots, Jobs and CronJobs — **as the caller**, with a separate unbound chart role (like `-node-admin`).
- Where backups go is configurable and optional: a PVC in the cluster or external S3.
- Restore goes into a **new** instance, never over a running one.

## Working with agents

- Work through phases one after another in a session until the context fills, then continue in a new one. Each phase ends with a commit, a CHANGELOG.md entry and a short summary (done / checked / left).
- ROADMAP.md is large: find a phase with `grep -n "^## Phase" ROADMAP.md` and read only that section.
- Do not push; pushing is the owner's job.
- A phase marked "discuss first" (e.g. Phase 77) needs the owner's decision before any code.
