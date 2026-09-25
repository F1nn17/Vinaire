import type { GatewayResourcesResult, IngressRow } from "@/lib/types";
import { apiFetch } from "./http";

// Real endpoint (Roadmap Phase 8).
export function getIngresses(namespace = ""): Promise<IngressRow[]> {
  const query = namespace ? `?namespace=${encodeURIComponent(namespace)}` : "";
  return apiFetch<IngressRow[]>(`/api/v1/ingresses${query}`);
}

// Real endpoint (Roadmap Phase 8) — reports `{ available: false, reason }`
// instead of an empty list or an error when Gateway API CRDs aren't
// installed on the target cluster (stock k3s/k3d don't ship them).
export function getGatewayResources(
  namespace = "",
): Promise<GatewayResourcesResult> {
  const query = namespace ? `?namespace=${encodeURIComponent(namespace)}` : "";
  return apiFetch<GatewayResourcesResult>(`/api/v1/gateway-routes${query}`);
}
