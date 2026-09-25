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
  never holds anything real). Confidential rather than public+PKCE because
  next-auth/Auth.js does the authorization-code exchange server-side, in
  the Next.js server process, never in the browser — the standard pattern
  for a server-rendered app, not a pure SPA. `directAccessGrantsEnabled`
  stays on for quick `curl`-based token testing below. Redirect/web-origin
  locked to `http://localhost:3100`.
- A `groups` claim (group *names*, not full paths) and an `aud` (audience)
  claim naming `vinaire-frontend` are both added to the access token via
  protocol mappers on that client — the backend's OIDC verifier
  (`internal/authz`) checks the audience, and Keycloak doesn't include it
  by default.
- Two groups with no attributes of their own — the actual
  group → namespace/action policy lives in the **backend's own config**
  (`backend/policy.yaml`, see Phase 10 in ROADMAP.md), not in Keycloak,
  matching the "config-backed allow-list, not a policy engine" design:
  - `auth-viewers` — user `alice` / `alice` (namespace `auth`, read-only)
  - `platform-admins` — user `bob` / `bob` (every namespace, read+write)

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
