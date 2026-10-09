import type { PVCDetail, PVCRow, PVRow, StorageClassRow } from "@/lib/types";
import { apiFetch, ApiError } from "./http";

// Real endpoint (Roadmap Phase 13).
export function getPVCs(namespace = ""): Promise<PVCRow[]> {
  const query = namespace ? `?namespace=${encodeURIComponent(namespace)}` : "";
  return apiFetch<PVCRow[]>(`/api/v1/pvcs${query}`);
}

// Real endpoint (Roadmap Phase 13). PersistentVolumes are cluster-scoped —
// no namespace parameter.
export function getPVs(): Promise<PVRow[]> {
  return apiFetch<PVRow[]>("/api/v1/pvs");
}

// Real endpoint (Roadmap Phase 13). StorageClasses are cluster-scoped.
export function getStorageClasses(): Promise<StorageClassRow[]> {
  return apiFetch<StorageClassRow[]>("/api/v1/storageclasses");
}

// Deleting one PersistentVolumeClaim (owner's decision of 2026-10-09). `confirmName` must equal
// `name` — the backend re-checks it, and refuses while a pod still mounts the claim (409).
export async function deletePVC(
  namespace: string,
  name: string,
): Promise<void> {
  await apiFetch(
    `/api/v1/pvcs/${encodeURIComponent(namespace)}/${encodeURIComponent(name)}`,
    {
      method: "DELETE",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ confirmName: name }),
    },
  );
}

// Real endpoint (Roadmap Phase 13).
export async function getPVC(
  namespace: string,
  name: string,
): Promise<PVCDetail | undefined> {
  try {
    return await apiFetch<PVCDetail>(
      `/api/v1/pvcs/${encodeURIComponent(namespace)}/${encodeURIComponent(name)}`,
    );
  } catch (err) {
    if (err instanceof ApiError && err.status === 404) {
      return undefined;
    }
    throw err;
  }
}
