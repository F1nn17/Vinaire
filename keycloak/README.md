# Vinaire dev Keycloak

A throwaway, fully isolated Keycloak instance for developing and testing
Vinaire's Phase 10 authorization — the same idea as the dev-only k3d
cluster (`k3d-vinaire-dev`), just for identity instead of the cluster
itself. It doesn't touch anything else on the machine: separate container,
separate port, in-memory dev-mode storage (nothing persists once you tear
it down).

## Start

```bash
cd keycloak
docker compose up -d
```

Wait for `docker compose logs -f keycloak` to print "Keycloak ... started"
(realm import happens on that first boot). Then:

- Admin console: http://localhost:8180 (`admin` / `admin`)
- Realm: `vinaire-dev`
- OIDC discovery document:
  http://localhost:8180/realms/vinaire-dev/.well-known/openid-configuration

Port 8180, not the more obvious 8081: on this dev machine something else
(Nginx Proxy Manager) already listens on `[::1]:8081`, and Node's `fetch`
prefers that IPv6 loopback binding over Docker's IPv4 publish on the same
port number — next-auth's server-side OIDC discovery call silently hit the
wrong service instead of Keycloak. `curl` didn't show the problem (it
resolved to IPv4 first), which made it look like a Keycloak issue at first.
If you free up 8081 differently, moving this back is just the port mapping
in docker-compose.yml plus every `8180` reference below.

## What's pre-configured

- Realm `vinaire-dev`, `sslRequired: none` (plain HTTP is fine for local dev).
- Client `vinaire-frontend`: **confidential** client (`vinaire-frontend-dev-secret`
  — a throwaway dev-only secret, fine to commit since this Keycloak instance
  never holds anything real). Confidential because the whole Authorization
  Code + PKCE exchange happens **server-side in the Go backend**
  (`internal/authn`, `AUTH_MODE=oidc` — see ROADMAP.md's Phase 10), never in
  the browser or in any frontend JS — the redirect URI Keycloak sends the
  browser back to is the backend's own
  `http://localhost:8080/api/v1/auth/oidc/callback`, not the frontend.
  `directAccessGrantsEnabled` stays on for quick `curl`-based token testing
  below.
- A `groups` claim (group *names*, not full paths) is added to tokens via a
  protocol mapper — a scaffold for mapping Keycloak groups to Kubernetes
  RBAC (`ClusterRoleBinding`, see AGENTS.md's example), not consumed by
  Vinaire's own code directly: authorization is whatever the target
  cluster's real RBAC decides once configured to trust this realm
  (`--oidc-issuer-url` etc. on the API server), same as `token` mode's
  pasted ServiceAccount tokens.
- Two groups, used only as an example for that RBAC mapping — not wired to
  anything in this repo by default:
  - `auth-viewers` — user `alice` / `alice`
  - `platform-admins` — user `bob` / `bob`

## The Vinaire sign-in theme

`theme/vinaire` is the login theme the setup wizard offers (Roadmap Phase 85b): Keycloak's own sign-in page in Vinaire's
colours, in English and Russian. The compose file mounts it, so a realm can use it right away (Realm settings → Themes →
Login theme → `vinaire`); the wizard sets it for a realm it creates. It is the **source** of what Settings → Sign-in hands
out (`frontend/src/lib/keycloakThemeFiles.ts` is generated from it with `npm run gen:keycloak-theme`, and
`npm run check:keycloak-theme` fails when they differ).

## Quick token check (no browser)

```bash
curl -s http://localhost:8180/realms/vinaire-dev/protocol/openid-connect/token \
  -d grant_type=password -d client_id=vinaire-frontend \
  -d client_secret=vinaire-frontend-dev-secret \
  -d username=alice -d password=alice | jq .
```

The decoded access token's `groups` claim should be `["auth-viewers"]` and
its `aud` claim should include `"vinaire-frontend"`.

## Stop / reset

```bash
docker compose down -v
```

`-v` drops the container's in-memory state — the realm re-imports fresh
from `realm-export.json` on the next `up`. Editing that file (new users,
groups, redirect URIs) just needs a `down -v` + `up` to take effect.
