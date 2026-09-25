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

Every other resource kind (Pods, ConfigMaps, Secrets, Ingress, ...) and
every other verb stays read-only.

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
- Secrets stay list/describe metadata only. Decoded values are returned
  **only** through the explicit, audited
  `POST /api/v1/secrets/{namespace}/{name}/reveal` endpoint (Phase 6) —
  never from a list/get endpoint. This part of the original rule is
  unchanged.

If a request conflicts with this — asks for a write action on a resource
kind or verb not already covered above — stop and flag it rather than
silently extending write access.

## Repository structure

```text
.
├── frontend/               Next.js (App Router) + TypeScript + Tailwind CSS
│   └── src/app/            Pages, layouts, route handlers
├── backend/                Go module: github.com/shiraku/vinaire/backend
│   ├── main.go             HTTP server (chi), route wiring, graceful shutdown
│   ├── internal/config/    env-driven config (PORT, KUBECONFIG, CORS)
│   ├── internal/k8s/       client-go clientset + listers, read-only except
│   │                       Phase 9's Deployment restart/delete/rollback
│   ├── internal/api/       HTTP handlers: response shaping, status/color logic
│   └── internal/stream/    informers + WebSocket fan-out for live updates
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
