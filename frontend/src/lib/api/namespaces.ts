import type { NamespaceDetail, NamespaceRow } from "@/lib/types";
import { apiFetch } from "./http";

// Real endpoints (Roadmap Phase 27). Creating a namespace and editing its
// ResourceQuota/LimitRange go through Phase 25's generic YAML apply
// (lib/api/resources.ts); only the irreversible delete has its own endpoint.

export function getNamespaces(): Promise<NamespaceRow[]> {
  return apiFetch<NamespaceRow[]>("/api/v1/namespaces");
}

export function getNamespace(name: string): Promise<NamespaceDetail> {
  return apiFetch<NamespaceDetail>(
    `/api/v1/namespaces/${encodeURIComponent(name)}`,
  );
}

// `confirmName` must equal `name` — the backend re-checks it, so the
// double-confirmation isn't only a UI nicety.
export async function deleteNamespace(name: string): Promise<void> {
  await apiFetch(`/api/v1/namespaces/${encodeURIComponent(name)}`, {
    method: "DELETE",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ confirmName: name }),
  });
}
