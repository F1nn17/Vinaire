import NextAuth from "next-auth";
import Keycloak from "next-auth/providers/keycloak";

// Roadmap Phase 10: Keycloak-backed login (see /keycloak at the repo root
// for the dev instance this points at). The backend never sees a session
// cookie — every /api/v1 call carries this session's accessToken as a
// bearer token instead (see lib/api/http.ts), so authorization is enforced
// once, server-side, against the same token either way.
//
// AUTH_KEYCLOAK_ISSUER vs. AUTH_KEYCLOAK_AUTHORIZATION_URL: this backend
// process (Next.js's server) and the browser can end up reaching Keycloak
// by two genuinely different paths, and each needs its own URL:
//
//   - `issuer` is what THIS SERVER PROCESS uses — for the OIDC discovery
//     fetch (which then supplies the real token/jwks/userinfo endpoints)
//     and for checking the id_token's `iss` claim. When frontend runs
//     natively on Windows, that's `http://localhost:8180/...` same as
//     everyone else. When it runs in a container instead (see
//     docker-compose.dev.yml) — the fix for a local Proxifier install
//     silently breaking Node's own TCP connections, even to localhost, see
//     AGENTS.md's frontend "Known local issue" — it's instead
//     `http://vinaire-keycloak:8080/...`, Keycloak's container-network
//     service name, since that's a path Node-in-a-container can actually
//     reach. Keycloak's default "request-based" hostname derivation means
//     whichever hostname a request arrives on is also what it reports back
//     in the discovery document and issues in the token's `iss` claim, so
//     picking either consistently for every server-side call keeps them
//     self-consistent.
//   - `authorization` is where the BROWSER gets redirected to log in —
//     always the host-published `http://localhost:8180/...`, regardless of
//     where this server process itself runs, because only a real browser
//     ever requests it, and a Docker-internal service name means nothing
//     to it.
export const { handlers, auth, signIn, signOut } = NextAuth({
  providers: [
    Keycloak({
      clientId: process.env.AUTH_KEYCLOAK_ID,
      clientSecret: process.env.AUTH_KEYCLOAK_SECRET,
      issuer: process.env.AUTH_KEYCLOAK_ISSUER,
      authorization: process.env.AUTH_KEYCLOAK_AUTHORIZATION_URL,
    }),
  ],
  callbacks: {
    // Stash the OIDC access token (and its expiry) on the JWT so it
    // survives across requests — session() below is what actually exposes
    // it to client code.
    async jwt({ token, account }) {
      if (account) {
        token.accessToken = account.access_token;
        token.accessTokenExpires = account.expires_at
          ? account.expires_at * 1000
          : undefined;
      }
      return token;
    },
    async session({ session, token }) {
      session.accessToken = token.accessToken;
      session.accessTokenExpires = token.accessTokenExpires;
      return session;
    },
  },
});
