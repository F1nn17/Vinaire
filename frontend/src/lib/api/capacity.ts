import type { CapacityResponse } from "@/lib/types";
import { apiFetch } from "./http";

// Real endpoint (Roadmap Phase 22).
export function getCapacity(): Promise<CapacityResponse> {
  return apiFetch<CapacityResponse>("/api/v1/capacity");
}
