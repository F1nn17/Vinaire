import useSWR from "swr";
import { apiFetch } from "./http";

// Sign-in through Keycloak, set up from the UI (Roadmap Phase 47). Matches the
// backend's api.KeycloakStatus / KeycloakSetupResult.

export type KeycloakStatus = {
  available: boolean;
  reason?: string;
  active: boolean;
  configured: boolean;
  issuer?: string;
  realm?: string;
  clientId?: string;
  redirectUrl?: string;
  tokenLogin: boolean;
  setUpBy?: string;
  pendingError?: string;
  operatorManaged?: boolean;
  // the operator allowed plain http to a Keycloak on a private network (home server without TLS)
  allowHttpPrivateNetwork?: boolean;
  // how a person who signed in reaches the cluster (Phase 80): "serviceaccount" (an account of their own
  // in userNamespace) or "apiserver" (the ID token; the API server must trust the issuer, https only)
  identityMode?: IdentityMode;
  userNamespace?: string;
  brokerAvailable?: boolean;
  brokerReason?: string;
  // the prefix of the chart's roles (<prefix>-viewer, -admin…); empty outside the chart
  rolePrefix?: string;
};

export type IdentityMode = "serviceaccount" | "apiserver";

export type KeycloakSetupRequest = {
  keycloakUrl: string;
  adminRealm: string;
  adminUser: string;
  adminPassword: string;
  realm: string;
  clientId: string;
  user: string;
  userEmail: string;
  userPassword: string;
  userChangePassword: boolean;
  allowTokenLogin: boolean;
  // empty: ServiceAccounts for an http address, the API server for an https one
  identityMode?: IdentityMode;
  // Keycloak's sign-in page in Vinaire's look (the "vinaire" theme, if it is installed on that Keycloak)
  loginTheme?: boolean;
};

export type KeycloakSetupResult = {
  steps: { what: string; status: string }[];
  issuer: string;
  redirectUrl: string;
  // only for the API-server mode
  apiServerFlags?: string[];
  identityMode: IdentityMode;
  userNamespace?: string;
};

const json = { "Content-Type": "application/json" };

export function useKeycloakStatus() {
  return useSWR("keycloak-status", () =>
    apiFetch<KeycloakStatus>("/api/v1/auth/keycloak"),
  );
}

export function setupKeycloak(
  req: KeycloakSetupRequest,
): Promise<KeycloakSetupResult> {
  return apiFetch("/api/v1/auth/keycloak/setup", {
    method: "POST",
    headers: json,
    body: JSON.stringify(req),
  });
}

export function setKeycloakTokenLogin(allow: boolean): Promise<void> {
  return apiFetch("/api/v1/auth/keycloak/token-login", {
    method: "PUT",
    headers: json,
    body: JSON.stringify({ allow }),
  });
}

export function disableKeycloak(): Promise<void> {
  return apiFetch("/api/v1/auth/keycloak", { method: "DELETE" });
}

// The signed-in sessions (Roadmap Phase 48): who and until when — never a cookie or token.
export type SessionRow = {
  ref: string;
  owner: string;
  username?: string;
  expiresAt: string;
  current: boolean;
};

export function useSessions() {
  return useSWR(
    "auth-sessions",
    () =>
      apiFetch<{ sessions: SessionRow[]; shared: boolean }>(
        "/api/v1/auth/sessions",
      ),
    { refreshInterval: 15000 },
  );
}

export function revokeSession(ref: string): Promise<void> {
  return apiFetch(`/api/v1/auth/sessions/${encodeURIComponent(ref)}`, {
    method: "DELETE",
  });
}

// How this session is known to the cluster (GET /api/v1/auth/me). kubernetesUser is set for a person who
// signed in through Keycloak by ServiceAccount: it is what an administrator binds a role to.
export type WhoAmI = {
  username: string;
  kubernetesUser: string;
  mode: string;
  // the release of this Vinaire ("dev" when it was not set)
  version?: string;
};

export function useWhoAmI() {
  return useSWR("auth-me", () => apiFetch<WhoAmI>("/api/v1/auth/me"), {
    revalidateOnFocus: false,
  });
}

// Which sign-in theme the realm's page really serves (Roadmap Phase 99): a read of a public page, no password.
export type ThemeCheck = {
  checked: boolean;
  active: boolean;
  themes?: string[];
  error?: string;
  realm?: string;
};

export function useKeycloakThemeCheck(enabled: boolean) {
  return useSWR(
    enabled ? "keycloak-theme-check" : null,
    () => apiFetch<ThemeCheck>("/api/v1/auth/keycloak/theme-check"),
    { revalidateOnFocus: false },
  );
}

export function getKeycloakThemeCheck(): Promise<ThemeCheck> {
  return apiFetch<ThemeCheck>("/api/v1/auth/keycloak/theme-check");
}

// Switch the saved realm to the Vinaire theme with the administrator's login (used once, never kept).
export function applyKeycloakLoginTheme(req: {
  adminUser: string;
  adminPassword: string;
}): Promise<{ status: string; realm: string }> {
  return apiFetch("/api/v1/auth/keycloak/login-theme", {
    method: "POST",
    headers: json,
    body: JSON.stringify(req),
  });
}
