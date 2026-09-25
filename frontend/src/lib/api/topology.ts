import type { TopologyData } from "@/lib/types";
import { apiFetch } from "./http";

// Real endpoint (Roadmap Phase 7) — the backend computes the whole graph
// (nodes + edges) server-side; this just fetches it for one namespace.
export function getTopology(namespace: string): Promise<TopologyData> {
  return apiFetch<TopologyData>(
    `/api/v1/topology?namespace=${encodeURIComponent(namespace)}`,
  );
}
