import type { CRDInstanceDetail, CRDInstanceRow, CRDRow } from "@/lib/types";
import { apiFetch } from "./http";

// Real endpoint (Roadmap Phase 15) — discovers whatever CRDs are actually
// installed on the connected cluster.
export function getCRDs(): Promise<CRDRow[]> {
  return apiFetch<CRDRow[]>("/api/v1/crds");
}

export function getCRDInstances(
  crdName: string,
  namespace = "",
): Promise<CRDInstanceRow[]> {
  const query = namespace ? `?namespace=${encodeURIComponent(namespace)}` : "";
  return apiFetch<CRDInstanceRow[]>(
    `/api/v1/crds/${encodeURIComponent(crdName)}/instances${query}`,
  );
}

export function getCRDInstance(
  crdName: string,
  name: string,
  namespace = "",
): Promise<CRDInstanceDetail> {
  const query = namespace ? `?namespace=${encodeURIComponent(namespace)}` : "";
  return apiFetch<CRDInstanceDetail>(
    `/api/v1/crds/${encodeURIComponent(crdName)}/instances/${encodeURIComponent(name)}${query}`,
  );
}
