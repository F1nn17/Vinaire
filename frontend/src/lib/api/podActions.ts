import { apiFetch } from "./http";

// Roadmap Phase 33 — deletes a Pod outright. If it's controller-owned the
// controller recreates it (effectively a restart); a bare pod is gone for
// good. See PodDetail.hasController for telling the two cases apart
// before calling this.
export async function deletePod(
  namespace: string,
  name: string,
): Promise<void> {
  await apiFetch(
    `/api/v1/pods/${encodeURIComponent(namespace)}/${encodeURIComponent(name)}`,
    { method: "DELETE" },
  );
}
