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

## Home server without TLS (decided 2026-10-21)

- Keycloak sign-in may run over plain http, but only as an operator opt-in (`keycloak.allowHttpPrivateNetwork`) and only on a private network (RFC 1918, 100.64/10, IPv6 ULA) — not a checkbox in the UI, not for public addresses.

## Keycloak groups are not turned into rights (decided 2026-10-24, Roadmap Phase 87)

**Decision.** Vinaire does not map Keycloak groups to Kubernetes roles ("everyone in `devs` gets role X"). This is a
deliberate "no", not a missing feature, and it stays a no unless the owner decides to accept the cost below.

**Why.**
- In the ServiceAccount mode (Phase 80, the only way to use a Keycloak on http) the cluster sees a ServiceAccount token, and
  that token carries no groups. The only way to honour a group would be for Vinaire to **create RoleBindings itself** for each
  member, from what Keycloak says about them.
- That makes Vinaire the one who *grants* rights, on its own authority. Everywhere else it hands out only roles that already
  exist, **as the caller**, so Kubernetes' escalation check applies and nobody gets more than the person clicking already has
  (Phase 44). A background mapping has no caller, so it would need the right to bind roles it does not itself hold
  (`bind`/`escalate`) — a standing power to give anyone any role.
- So a compromised Vinaire, or a wrong group in Keycloak, would become cluster administration: whoever can edit group
  membership in Keycloak could grant themselves rights in the cluster without Kubernetes ever asking who approved it. Today a
  person's rights are decided in one place, the cluster's RBAC, and Keycloak only proves who they are. We keep it that way.
- It would also be a second source of truth: bindings made by Vinaire from groups would drift from what Keycloak says after
  every membership change, and cleaning up (someone leaves a group) needs the same standing power to delete.

**What to do instead.**
- **Keycloak on https:** let the Kubernetes API server trust it (`--oidc-*` flags, `identityMode: apiserver`) and put the
  groups claim in the token. Kubernetes then sees `Group` subjects natively and a RoleBinding to a group works — approved by
  whoever creates the binding, as the caller, with nothing extra for Vinaire.
- **Keycloak on http (home server):** bind roles to people one by one on the Access control page (each is audited and checked
  by Kubernetes' escalation rule), or create one RoleBinding with several ServiceAccount subjects yourself.

**When to revisit.** If the owner accepts a Vinaire that grants rights on its own authority — then it needs its own design
(an explicit allow-list of group → role pairs in the operator's configuration, never in a request; roles limited to the chart's
own; every grant audited; removal on leaving the group) and a new entry in AGENTS.md's list of what may write.

## Working with agents

- Work through phases one after another in a session until the context fills, then continue in a new one. Each phase ends with a commit, a CHANGELOG.md entry and a short summary (done / checked / left).
- ROADMAP.md is large: find a phase with `grep -n "^## Phase" ROADMAP.md` and read only that section.
- Push feature branches (never `main`); the owner merges the PR, then refresh local `main` and branch the next task from it.
- A phase marked "discuss first" (e.g. Phase 77) needs the owner's decision before any code.
