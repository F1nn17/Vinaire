import { apiFetch } from "./http";

// Roadmap Phase 33 — Scale via the Scale subresource, StatefulSet's
// counterpart to deploymentActions.ts's scaleDeployment.
export async function scaleStatefulSet(
  namespace: string,
  name: string,
  replicas: number,
): Promise<void> {
  await apiFetch(
    `/api/v1/statefulsets/${encodeURIComponent(namespace)}/${encodeURIComponent(name)}/scale`,
    {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ replicas }),
    },
  );
}
