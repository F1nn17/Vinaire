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
};

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
};

export type KeycloakSetupResult = {
  steps: { what: string; status: string }[];
  issuer: string;
  redirectUrl: string;
  apiServerFlags: string[];
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
