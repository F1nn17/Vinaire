import { API_BASE } from "./config";
import { ApiError } from "./http";

export type AuthConfig = {
  mode: "token" | "oidc" | "disabled";
  // Keycloak sign-in is on (also when it was set up at run time in token mode)
  oidc?: boolean;
  // pasting a token is open
  tokenLogin?: boolean;
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
  const res = await fetch(`${API_BASE}/api/v1/auth/logout`, {
    method: "POST",
    credentials: "include",
  });
  // shared sessions: the server says no until it has really ended the session (Roadmap Phase 109)
  if (!res.ok) throw new ApiError(res.status, "sign-out failed");
}

// The browser navigates here directly (not a fetch) — Keycloak's own login
// page needs a real top-level navigation, not an XHR/fetch response body.
export function oidcLoginURL(): string {
  return `${API_BASE}/api/v1/auth/oidc/login`;
}
