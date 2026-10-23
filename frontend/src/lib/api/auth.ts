import { API_BASE } from "./config";
import { ApiError } from "./http";

export type AuthConfig = {
  mode: "token" | "oidc" | "disabled";
  // Keycloak sign-in is on (also when it was set up at run time in token mode)
  oidc?: boolean;
  // pasting a token is open
  tokenLogin?: boolean;
  // Vinaire shows its own username and password form for the Keycloak sign-in (Phase 85)
  oidcDirect?: boolean;
};

// Not routed through apiFetch: this one call must succeed even when the
// caller isn't logged in yet (that's the whole point — it's what decides
// which login screen to show), so it must never trigger apiFetch's
// 401-redirects-to-/login behavior.
export async function getAuthConfig(): Promise<AuthConfig> {
  const res = await fetch(`${API_BASE}/api/v1/auth/config`, {
    credentials: "include",
  });
  if (!res.ok) {
    throw new ApiError(res.status, "failed to load auth config");
  }
  return res.json() as Promise<AuthConfig>;
}

// Logs in with a pasted Kubernetes ServiceAccount token (AUTH_MODE=token).
// The backend never echoes the token back — a 200 here just means the
// HttpOnly session cookie is now set.
export async function loginWithToken(token: string): Promise<void> {
  const res = await fetch(`${API_BASE}/api/v1/auth/login`, {
    method: "POST",
    credentials: "include",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ token }),
  });
  if (!res.ok) {
    const body = await res.text().catch(() => "");
    throw new ApiError(res.status, body || "login failed");
  }
}

export async function logout(): Promise<void> {
  await fetch(`${API_BASE}/api/v1/auth/logout`, {
    method: "POST",
    credentials: "include",
  });
}

// The browser navigates here directly (not a fetch) — Keycloak's own login
// page needs a real top-level navigation, not an XHR/fetch response body.
export function oidcLoginURL(): string {
  return `${API_BASE}/api/v1/auth/oidc/login`;
}

// Vinaire's own sign-in form for Keycloak (Roadmap Phase 85): the password is sent to Vinaire, which passes it to
// Keycloak. The answer is a fixed text; `code` tells the form what to do — "needs_provider_page" means the account
// has to finish setting up on Keycloak's own page (a temporary password, a required step), "direct_not_enabled"
// that the grant is off at Keycloak. The password is never kept: it lives in the form's state until this returns.
export async function loginWithPassword(
  username: string,
  password: string,
  code?: string,
): Promise<void> {
  const res = await fetch(`${API_BASE}/api/v1/auth/oidc/password`, {
    method: "POST",
    credentials: "include",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ username, password, ...(code ? { code } : {}) }),
  });
  if (!res.ok) {
    let message = "Sign-in failed.";
    let data: Record<string, unknown> | undefined;
    try {
      const body = (await res.json()) as { error?: string; code?: string };
      if (body.error) message = body.error;
      if (body.code) data = { code: body.code };
    } catch {
      /* not JSON */
    }
    throw new ApiError(res.status, message, data);
  }
}
