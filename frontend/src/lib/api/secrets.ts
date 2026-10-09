import type { SecretDetail } from "@/lib/types";
import { apiFetch } from "./http";

// Real endpoint (Roadmap Phase 6) — the backend only ever returns metadata
// and key *names* here, never values (see AGENTS.md's secrets rule).
export function getSecrets(namespace = ""): Promise<SecretDetail[]> {
  const query = namespace ? `?namespace=${encodeURIComponent(namespace)}` : "";
  return apiFetch<SecretDetail[]>(`/api/v1/secrets${query}`);
}

// Deleting one Secret (owner's decision of 2026-10-09). `confirmName` must equal `name`; the
// backend re-checks it and refuses a Helm release record (409).
export async function deleteSecret(
  namespace: string,
  name: string,
): Promise<void> {
  await apiFetch(
    `/api/v1/secrets/${encodeURIComponent(namespace)}/${encodeURIComponent(name)}`,
    {
      method: "DELETE",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ confirmName: name }),
    },
  );
}

// Explicit, audited reveal of one key's decoded value. Every call is logged
// server-side; the value returned here should only ever live in local
// component state, never in the SWR cache, so it's re-masked on navigation.
export function revealSecretKey(
  namespace: string,
  name: string,
  key: string,
): Promise<string> {
  return apiFetch<{ value: string }>(
    `/api/v1/secrets/${encodeURIComponent(namespace)}/${encodeURIComponent(name)}/reveal`,
    {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ key }),
    },
  ).then((res) => res.value);
}
