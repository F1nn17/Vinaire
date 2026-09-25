import { apiFetch } from "./http";

export type HealthzResponse = {
  status: string;
};

export function getHealthz(): Promise<HealthzResponse> {
  return apiFetch<HealthzResponse>("/healthz");
}
