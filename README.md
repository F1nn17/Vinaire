# Vinaire

Vinaire is a custom web UI for managing Kubernetes clusters — a friendlier,
more opinionated alternative to Rancher/Headlamp, targeting **k3s** clusters.

The UI is **read-only by default**. It is not a way to poke at your cluster
by hand; it's a way to *see* it clearly. Changes to cluster state are made
through GitOps (ArgoCD) via pull requests, not through direct writes from the
browser. See [AGENTS.md](./AGENTS.md) for the full rationale and the rule
this implies for anyone (human or AI) writing code here.

Design concept: an interactive mockup covering the main screens (Dashboard,
Topology, Workloads, ConfigMaps, Secrets, Network) exists outside this repo.
Ask the project owner for a walkthrough of the UX decisions if you need
context before implementing a screen.

## Stack

- **Frontend** — [`/frontend`](./frontend): Next.js (App Router), React,
  TypeScript, Tailwind CSS. Dark theme by default, accent color `#8B5FBF`.
  Fonts: JetBrains Mono (headings/mono) + IBM Plex Sans (body), loaded via
  `next/font`.
- **Backend** — [`/backend`](./backend): Go, [client-go](https://github.com/kubernetes/client-go)
  for talking to the Kubernetes API, a small HTTP server (chi) serving
  REST/JSON to the frontend.

## Getting started

### Prerequisites

- Node.js 20+ and npm
- Go 1.27+
- A reachable Kubernetes cluster (a local [k3d](https://k3d.io/) cluster is
  the intended dev target) and a working `~/.kube/config`

### Run both at once (recommended)

From the repo root:

```bash
npm install   # one-time, installs the root orchestrator (concurrently)
npm run dev
```

This starts the Go backend (`:8080`) and the Next.js frontend (`:3100`)
together in one terminal, color-tagged `BACKEND` / `FRONTEND`, and stops both
on Ctrl+C. In VS Code, `dev` also shows up as a one-click ▶ entry in the NPM
Scripts sidebar (no extra setup needed) — that's the "one button" version.

> The frontend runs on port `3100`, not Next.js's default `3000` — pick a
> free port for your machine and adjust `-p` in `frontend/package.json` if
> `3100` collides with something local too.
>
> **Note (Windows):** this requires `go` to be on your `PATH`. If
> `npm run dev` fails with `'go' is not recognized`, add
> `C:\Program Files\Go\bin` (or wherever Go is installed) to your user PATH
> and open a new terminal.

### Backend only

```bash
cd backend
go run .
```

The server listens on `:8080`. Check `GET /healthz` once it's running. If no
cluster is reachable it still boots (it just logs a warning) so you can work
on the frontend without a live cluster.

### Frontend only

```bash
cd frontend
npm install
npm run dev
```

Open [http://localhost:3100](http://localhost:3100).

> **Known issue:** on this Windows dev setup, Next.js 16's default Turbopack
> compiler panics while processing `globals.css` (`evaluate_webpack_loader` /
> "node process exited before we could connect to it"). `dev` and `build` are
> pinned to `--webpack` in `package.json` as a workaround. If you're on a
> platform where Turbopack works fine, feel free to drop the flag.

## Repository layout

```text
.
├── frontend/     Next.js app
├── backend/      Go HTTP server + Kubernetes client
├── package.json  Root orchestrator (`npm run dev` runs both at once)
├── AGENTS.md     Conventions & architecture rules for AI coding agents
├── ROADMAP.md    Staged plan (MVP → GitOps write flow)
└── CHANGELOG.md
```

## License

[MIT](./LICENSE)
