import type { ConfigMapDetail } from "@/lib/types";
import { apiFetch } from "./http";

// Real endpoint (Roadmap Phase 6) — ConfigMap data isn't sensitive, so
// values come back directly here, unlike Secrets.
export function getConfigMaps(namespace = ""): Promise<ConfigMapDetail[]> {
  const query = namespace ? `?namespace=${encodeURIComponent(namespace)}` : "";
  return apiFetch<ConfigMapDetail[]>(`/api/v1/configmaps${query}`);
}
