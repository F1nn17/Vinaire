import { apiFetch } from "./http";

// Real write actions (Roadmap Phase 9) — direct client-go calls on the
// backend, not a GitOps PR. See AGENTS.md's core rule: this is the
// deliberate, narrow, audited exception to the read-only default.

export async function restartDeployment(
  namespace: string,
  name: string,
): Promise<void> {
  await apiFetch(
    `/api/v1/deployments/${encodeURIComponent(namespace)}/${encodeURIComponent(name)}/restart`,
    { method: "POST" },
  );
}

export async function deleteDeployment(
  namespace: string,
  name: string,
): Promise<void> {
  await apiFetch(
    `/api/v1/deployments/${encodeURIComponent(namespace)}/${encodeURIComponent(name)}`,
    { method: "DELETE" },
  );
}

export async function rollbackDeployment(
  namespace: string,
  name: string,
  revision: number,
): Promise<void> {
  await apiFetch(
    `/api/v1/deployments/${encodeURIComponent(namespace)}/${encodeURIComponent(name)}/rollback`,
    {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ revision }),
    },
  );
}
