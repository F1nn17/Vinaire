# AGENTS.md

Instructions for AI coding agents (Claude Code and similar) working in this
repository.

## The one rule that overrides everything else

**Vinaire's UI is read-only against the Kubernetes API. Full stop.**

The backend must never expose an endpoint that creates, updates, patches, or
deletes a cluster resource, and the frontend must never call the Kubernetes
API directly. This is a deliberate architectural decision, not a missing
feature:

- Any "make a change" flow (scale a deployment, edit a ConfigMap, update an
  Ingress, ...) must be implemented as: generate/update a manifest, open a
  pull request against the GitOps manifests repository, and let ArgoCD (or
  whatever the deployed GitOps controller is) reconcile the cluster. The UI's
  job stops at "PR opened."
- Do not add write verbs to `internal/k8s` (`Create*`, `Update*`, `Patch*`,
  `Delete*`, `Apply*`) against the live cluster clientset. If a task seems to
  require one, stop and flag it instead of implementing it — it likely means
  the task should be reframed as a GitOps PR flow.
- Secrets: list/describe metadata only. Never return decoded secret values
  over the API.

If a request from a user or an issue conflicts with this rule, implement the
GitOps-PR version of the feature and say so, rather than silently adding a
direct write path.

## Repository structure

```text
.
├── frontend/               Next.js (App Router) + TypeScript + Tailwind CSS
│   └── src/app/            Pages, layouts, route handlers
├── backend/                Go module: github.com/shiraku/vinaire/backend
│   ├── main.go             HTTP server (chi), route wiring, graceful shutdown
│   ├── internal/config/    env-driven config (PORT, KUBECONFIG, CORS)
│   ├── internal/k8s/       client-go clientset + read-only listers
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
- Keep `internal/k8s` read-only (see the rule above). New listers go there;
  new routes get wired up in `main.go`.
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
