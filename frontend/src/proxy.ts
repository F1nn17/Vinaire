import { NextResponse } from "next/server";
import { auth } from "@/auth";

// Roadmap Phase 10 DoD: opening any Vinaire URL while logged out redirects
// to Keycloak. Everything except NextAuth's own routes and static assets
// goes through this — see `config.matcher` below. Named proxy.ts, not
// middleware.ts — Next.js 16 renamed the file convention (same API).
export default auth((req) => {
  if (!req.auth) {
    // NextAuth v5's /api/auth/signin/:provider only accepts a CSRF-checked
    // POST (the sign-in page's own form submits one) — a bare GET redirect
    // to it 500s with UnknownAction. Land on the generic sign-in page
    // instead; with exactly one provider configured it's a single "Sign in
    // with Keycloak" click, not a picklist.
    const signInUrl = new URL("/api/auth/signin", req.nextUrl.origin);
    signInUrl.searchParams.set("callbackUrl", req.nextUrl.href);
    return NextResponse.redirect(signInUrl);
  }
});

export const config = {
  matcher: ["/((?!api/auth|_next/static|_next/image|favicon.ico).*)"],
};
