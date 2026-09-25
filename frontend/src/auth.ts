import NextAuth from "next-auth";
import Keycloak from "next-auth/providers/keycloak";

// Roadmap Phase 10: Keycloak-backed login (see /keycloak at the repo root
// for the dev instance this points at). The backend never sees a session
// cookie — every /api/v1 call carries this session's accessToken as a
// bearer token instead (see lib/api/http.ts), so authorization is enforced
// once, server-side, against the same token either way.
export const { handlers, auth, signIn, signOut } = NextAuth({
  providers: [
    Keycloak({
      clientId: process.env.AUTH_KEYCLOAK_ID,
      clientSecret: process.env.AUTH_KEYCLOAK_SECRET,
      issuer: process.env.AUTH_KEYCLOAK_ISSUER,
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
