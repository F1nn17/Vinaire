import "next-auth";
import "next-auth/jwt";

// Extends next-auth's Session/JWT with the OIDC access token this backend
// needs as a bearer token (see src/auth.ts's callbacks and
// lib/api/http.ts).
declare module "next-auth" {
  interface Session {
    accessToken?: string;
    accessTokenExpires?: number;
  }
}

declare module "next-auth/jwt" {
  interface JWT {
    accessToken?: string;
    accessTokenExpires?: number;
  }
}
