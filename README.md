# Vinaire

Vinaire is a custom web UI for managing Kubernetes clusters — a friendlier,
more opinionated alternative to Rancher/Headlamp, targeting **k3s** clusters.

The UI is **read-only by default**, with a small, deliberate, fully-audited
set of exceptions (Deployment restart/delete/rollback — see AGENTS.md's core
rule) rather than a general-purpose write path. Logging in (Roadmap Phase
10) is either a pasted Kubernetes ServiceAccount token or Keycloak/OIDC —
either way, what a given login can actually do is decided by that identity's
real Kubernetes RBAC, not by a separate permission system in this app. See
[AGENTS.md](./AGENTS.md) for the full rationale and conventions for anyone
(human or AI) writing code here.

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

## Docker (single combined image)

For a prod/test deploy — not needed for day-to-day development, which still
uses `npm run dev` + `go run .` above:

```bash
docker build -t vinaire .
docker run -p 8080:8080 --env AUTH_MODE=disabled vinaire
```

One image, one process: the Go backend serves both the API and the
frontend's static export (`next build`'s `output: "export"`) from the same
port — no Node/npm in the final layer. See AGENTS.md's "Rendering model"
and "Authentication (Phase 10)" sections for how the pieces fit together,
and set `AUTH_MODE`/`OIDC_*` env vars for a real deployment instead of
`disabled`.

## Installation (Helm)

For installing onto a real cluster rather than building from source. The Helm chart in
[`/charts/vinaire`](./charts/vinaire) packages the same single combined image as above and
creates **everything Vinaire needs** — the Deployment, Service, its own ServiceAccount and its
RBAC. You don't write a Deployment, a Service or a role by hand.

Pick the scenario that matches you, then read [the notes that apply to all of them](#things-every-install-needs-to-know).

| | Scenario | Image comes from | Sign-in | Reached over |
| --- | --- | --- | --- | --- |
| **1** | [Quick local trial — K3s / k3d / homelab](#scenario-1--quick-local-trial-k3s-k3d-homelab) | built on your machine, imported by hand | pasted token | plain HTTP |
| **2** | [Regular Kubernetes](#scenario-2--regular-kubernetes-registry-image--ingress--token) | a registry | pasted token | Ingress |
| **3** | [Production](#scenario-3--production-registry-https-oidc) | a registry, pinned version | OIDC | HTTPS Ingress |

### Scenario 1 — quick local trial (K3s, k3d, homelab)

**1. Build the image** (on your machine, in this repository):

```bash
docker build -t vinaire:latest .
```

**2. Get the image onto the cluster's node.** `vinaire:latest` isn't in any registry, so the
node has to be given it. Which command depends on where the cluster runs:

```bash
# k3d (the cluster runs in Docker on this machine)
k3d image import vinaire:latest -c <cluster-name>

# K3s on another machine (a homelab server): ship the image there and import it
docker save vinaire:latest -o vinaire.tar
scp vinaire.tar user@server:/tmp/vinaire.tar
ssh user@server 'sudo k3s ctr images import /tmp/vinaire.tar'
ssh user@server 'sudo k3s ctr images list | grep vinaire'     # confirm it's there
```

`k3d image import` only reaches a k3d cluster on *this* Docker — it does nothing for a
K3s server elsewhere. That's why the chart's `image.pullPolicy` defaults to `IfNotPresent`:
with `Always` the node would try to pull `vinaire:latest` from a registry, fail, and sit in
`ImagePullBackOff`. (`Never` also works for an imported image.)

**3. Install.** The chart ships a ready-made values file for exactly this case:

```bash
helm upgrade --install vinaire ./charts/vinaire \
  --namespace vinaire --create-namespace \
  -f ./charts/vinaire/examples/values-local.yaml

kubectl rollout status deployment/vinaire -n vinaire
kubectl get pods -n vinaire
kubectl get svc -n vinaire
```

[`examples/values-local.yaml`](./charts/vinaire/examples/values-local.yaml) uses the imported
image, `authMode: token`, an Ingress for Traefik (which K3s ships) on `vinaire.k3s.home`, and
**`cookie.secure: false`** (see [the cookie note](#why-you-bounce-back-to-the-login-screen-cookiesecure)).
Make the name resolve — add `<server-ip> vinaire.k3s.home` to your hosts file — and open
`http://vinaire.k3s.home`. No Ingress? `kubectl port-forward -n vinaire svc/vinaire 8080:8080`
and open `http://localhost:8080`.

> **On a K3s server, where is `kubectl`?** K3s keeps its kubeconfig at
> `/etc/rancher/k3s/k3s.yaml`, readable by root. As root, `export
> KUBECONFIG=/etc/rancher/k3s/k3s.yaml`, or prefix every command with `sudo k3s kubectl …`.
> The **Helm CLI is not bundled with K3s** — install it separately
> (<https://helm.sh/docs/intro/install/>), or run Helm from your own machine with that
> kubeconfig copied over.

**4. Log in** — see [Two different ServiceAccounts](#two-different-serviceaccounts-dont-mix-them-up).

### Scenario 2 — regular Kubernetes (registry image + Ingress + token)

Push the image to a registry the cluster can pull from, then point the chart at it and publish
it through your Ingress controller (the example uses Traefik; use `nginx` etc. as your cluster
has):

```bash
docker build -t <registry>/<owner>/vinaire:<version> .
docker push <registry>/<owner>/vinaire:<version>

helm upgrade --install vinaire ./charts/vinaire \
  --namespace vinaire --create-namespace \
  --set image.repository=<registry>/<owner>/vinaire \
  --set image.tag=<version> \
  --set ingress.enabled=true \
  --set ingress.className=traefik \
  --set 'ingress.hosts[0].host=vinaire.example.com' \
  --set 'ingress.tls[0].secretName=vinaire-tls' \
  --set 'ingress.tls[0].hosts[0]=vinaire.example.com'
```

Keep `cookie.secure` at its default (`true`) — the Ingress terminates TLS. A private registry
needs `imagePullSecrets`. Sign-in is by token, as in scenario 1.

### Scenario 3 — production (registry, HTTPS, OIDC)

Everything of scenario 2, plus real login through Keycloak (or another OIDC provider), the
OIDC client secret in a Secret you create yourself, and a NetworkPolicy. Start from
[`examples/values-production.yaml`](./charts/vinaire/examples/values-production.yaml), replace
the `<placeholders>`, and:

```bash
kubectl create namespace vinaire
kubectl create secret generic vinaire-oidc -n vinaire --from-literal=clientSecret='<client secret>'

helm upgrade --install vinaire ./charts/vinaire \
  --namespace vinaire \
  -f values-production.yaml
```

The cluster's own API server must **also** trust the OIDC issuer, or sign-in succeeds while every
cluster call returns 401 — Vinaire's *Settings → Справка → Вход по логину и паролю* walks through
it. Read [docs/security.md](./docs/security.md) before exposing this anywhere real.

### Things every install needs to know

#### Two different ServiceAccounts — don't mix them up

1. **Vinaire's own ServiceAccount** — created by the chart (named after the release). It
   only feeds the live-update watchers and is deliberately minimal. Leave it alone, and don't
   replace the Deployment's ServiceAccount with `default`.
2. **Yours** — the account whose *token you paste on the login screen*. Vinaire acts as that
   account, so the cluster's RBAC for it decides what you can do. Create one and bind the roles
   the chart provides (named `<fullname>-…`; for a release called `vinaire` that is `vinaire-…`):

```bash
kubectl create serviceaccount me -n vinaire

# browse + the fixed actions (restart, scale, logs, terminal, port-forward)
kubectl create clusterrolebinding me-vinaire-viewer \
  --clusterrole=vinaire-viewer --serviceaccount=vinaire:me

# optional: install from the Marketplace / apply YAML — in addition to viewer
kubectl create clusterrolebinding me-vinaire-installer \
  --clusterrole=vinaire-installer --serviceaccount=vinaire:me

# optional: change Vinaire's own settings (clusters, GitOps) — NOT cluster admin, see below
kubectl create clusterrolebinding me-vinaire-admin \
  --clusterrole=vinaire-admin --serviceaccount=vinaire:me

kubectl create token me -n vinaire --duration=1h      # paste this into the login screen
```

The three roles do different things, and **`-admin` is not cluster admin**: it only lets you
change Vinaire's own configuration (the registry of clusters, GitOps) and grants nothing
inside Kubernetes, so it is always *in addition to* `-viewer`. `-installer` gives create /
update / delete on the namespaced kinds Helm charts usually contain; a chart that also creates
ClusterRoles or CRDs needs more, which Vinaire tells you *before* it starts installing (add
the rights with `rbac.installer.extraRules`).

Check that the rights are what you think:

```bash
kubectl auth can-i list pods --as=system:serviceaccount:vinaire:me --all-namespaces        # yes
kubectl auth can-i create clusterrolebindings --as=system:serviceaccount:vinaire:me        # no
```

> **`-viewer` includes read access to Secrets.** Vinaire's UI shows only a Secret's metadata
> until you use the explicit, audited *reveal* action — but at the Kubernetes API level the
> account can `get`/`list` them (and Helm keeps each release as a Secret, which is part of why
> the role needs them). If an account must *not* be able to read Secrets, don't bind it to
> `-viewer`: give it a role of your own without them.

#### Sign-in modes (`authMode`)

| `authMode` | What it is | Use it for |
| --- | --- | --- |
| `token` *(default)* | Paste a ServiceAccount token on the login screen | Trials, homelabs, small teams |
| `oidc` | Real login through Keycloak / any OIDC provider | **Production** |
| `disabled` | No login at all; every request runs with Vinaire's own ServiceAccount, which then gets the broad permissions | A disposable local cluster only — anyone who can reach the URL can act. It does **not** mean "use the default ServiceAccount's rights" |

Never recommend `cluster-admin` for everyday use. An unknown value (`authMode: tokens`) is
rejected by the chart's `values.schema.json` with a message naming the allowed ones.

#### Why you bounce back to the login screen (`cookie.secure`)

The session cookie is marked `Secure` by default. A browser keeps a `Secure` cookie only for a
page served over **HTTPS** (Chrome and Firefox also accept it on `http://localhost`), so over
plain HTTP at any other address you enter a valid token and land straight back on the login
screen. The default is right for HTTPS — leave it. For a **local, HTTP-only trial**:

```bash
helm upgrade --install vinaire ./charts/vinaire … --set cookie.secure=false
```

> ⚠️ With `cookie.secure=false` the session cookie also travels unencrypted — anyone on the
> network path can read it. Never do this on a shared or real cluster.

#### Upgrading

`helm upgrade --install` is the same command for first install and every upgrade. Chart 0.2.0
changed the defaults (log in as a ServiceAccount bound to `-viewer`, not as Vinaire's own;
`SameSite=Strict`); 0.3.0 adds the Ingress, `values.schema.json`, the `-installer` role and the
examples, and changes nothing that existed.

See `charts/vinaire/values.yaml` for every option — most notably `authMode`, `ingress`,
`cookie`, `networkPolicy`, and `rbac.extraRules` for read access to the CRDs your own cluster
has (Custom Resources; see Vinaire's Settings → Справка once logged in).

### Troubleshooting

| Symptom | Likely cause and what to do |
| --- | --- |
| `ImagePullBackOff` on the Vinaire pod | The node doesn't have `vinaire:latest`. Import it (see scenario 1) and check `sudo k3s ctr images list \| grep vinaire`; make sure `image.pullPolicy` is `IfNotPresent`/`Never`, and the tag in `kubectl describe pod` is the one you imported. |
| `clusterrole "vinaire-viewer" not found` when binding | Vinaire was deployed without the chart (a hand-written Deployment), so the roles were never created. Install it with `helm upgrade --install` — the chart creates them. |
| After entering a token you're sent back to the login screen | (1) `cookie.secure` is `true` but you're on plain HTTP — use HTTPS, or `--set cookie.secure=false` for a local trial; (2) `authMode` isn't `token`; (3) the token expired (`--duration`). |
| `pods is forbidden` / `403` everywhere | The account whose token you pasted has no rights. Check with `kubectl auth can-i … --as=system:serviceaccount:<ns>:<name>` and make sure the **viewer** role is bound to *your* ServiceAccount, not to Vinaire's own. |
| Marketplace install: "you don't have the permissions this chart needs" | Bind `-installer` (in addition to `-viewer`). Note `-admin` grants no cluster rights. |
| The Ingress answers 404 / doesn't resolve | `ingress.className` must match your controller (`kubectl get ingressclass`); the host must resolve to it (hosts file / DNS). |
| Helm says `values don't meet the specifications of the schema` | A value has the wrong type or isn't an allowed choice (e.g. `authMode: tokens`); the message names the key and the allowed values. |

## Security

Read [docs/security.md](./docs/security.md) before putting Vinaire anywhere real: a short
threat model, the properties that were verified (and how to verify them again), and the risks
that remain. In short:

- **Don't expose Vinaire directly to the internet.** Reach it over a VPN (WireGuard, NetBird,
  Tailscale). If it must be public, only behind HTTPS with a real certificate (cert-manager).
- **Vinaire is a single point with access to every cluster registered in it** — treat it like a
  bastion host, and keep `CLUSTER_STORE_KEY` out of the repository and the image.
- Almost everything runs as the signed-in user, so the cluster's own RBAC decides; the roles in
  the chart (`viewer`, `installer`, `admin`) are created unbound — bind them deliberately.
- The `Security` workflow (govulncheck, gosec, npm audit, gitleaks, Trivy, an OWASP ZAP
  baseline and browser checks of the CSP) runs on every pull request and weekly.

## Repository layout

```text
.
├── frontend/     Next.js app
├── backend/      Go HTTP server + Kubernetes client
├── charts/       Helm chart for installing onto a real cluster
├── package.json  Root orchestrator (`npm run dev` runs both at once)
├── AGENTS.md     Conventions & architecture rules for AI coding agents
├── ROADMAP.md    Staged plan (MVP → GitOps write flow)
└── CHANGELOG.md
```

## License

Licensed under the [Apache License 2.0](./LICENSE). See [NOTICE](./NOTICE) for third-party components (including the bundled fonts, under the SIL Open Font License).
