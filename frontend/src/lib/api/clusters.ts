import type { Cluster } from "@/lib/types";
import { apiFetch } from "./http";

// Real endpoint (Roadmap Phase 11) — every kubeconfig context the backend
// is configured to talk to, plus any cluster added dynamically through the
// cluster-management UI (Settings page).
export function getClusters(): Promise<Cluster[]> {
  return apiFetch<Cluster[]>("/api/v1/clusters");
}

export type AddClusterRequest = {
  name: string;
  server: string;
  token: string;
  caData?: string;
};

// Validates the submitted credentials against a real API call server-side
// before persisting anything — a bad token/server fails this call directly,
// not silently later on every page that tries to use the new cluster.
export function addCluster(req: AddClusterRequest): Promise<Cluster> {
  return apiFetch<Cluster>("/api/v1/clusters", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(req),
  });
}

export function removeCluster(name: string): Promise<void> {
  return apiFetch<void>(`/api/v1/clusters/${encodeURIComponent(name)}`, {
    method: "DELETE",
  });
}
