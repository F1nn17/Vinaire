import type { Cluster } from "@/lib/types";
import { apiFetch } from "./http";

// Real endpoint (Roadmap Phase 11) — every kubeconfig context the backend
// is configured to talk to.
export function getClusters(): Promise<Cluster[]> {
  return apiFetch<Cluster[]>("/api/v1/clusters");
}
