import { apiFetch } from "./http";

// Roadmap Phase 74: the well-known applications that run in the cluster (PostgreSQL, Redis/Valkey,
// MinIO, Keycloak), found among the pods the signed-in user may list — a read, as the caller.
export type AppInstance = {
  namespace: string;
  name: string;
  kind: string;
  image: string;
  pods: number;
  ready: number;
};

export type DetectedApp = {
  id: string;
  title: string;
  instances: AppInstance[];
};

export function getDetectedApps(): Promise<{ apps: DetectedApp[] }> {
  return apiFetch("/api/v1/apps/detected");
}
