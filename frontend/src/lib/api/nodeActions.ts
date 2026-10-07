import { apiFetch } from "./http";

// Node lifecycle (Roadmap Phase 40): cordon / uncordon, drain, labels, taints and
// removing the Node object. Direct, audited calls that run as the signed-in user,
// so the cluster's RBAC decides (see AGENTS.md's write list).

const json = { "Content-Type": "application/json" };
const base = (name: string) => `/api/v1/nodes/${encodeURIComponent(name)}`;

export async function cordonNode(name: string): Promise<void> {
  await apiFetch(`${base(name)}/cordon`, { method: "POST" });
}

export async function uncordonNode(name: string): Promise<void> {
  await apiFetch(`${base(name)}/uncordon`, { method: "POST" });
}

export type DrainDisposition = "evict" | "skip" | "unmanaged" | "localdata";

export type DrainPod = {
  namespace: string;
  name: string;
  disposition: DrainDisposition;
  reason?: string;
};

export async function getDrainPlan(name: string): Promise<DrainPod[]> {
  const res = await apiFetch<{ pods: DrainPod[] }>(`${base(name)}/drain-plan`);
  return res.pods;
}

export type DrainResult = {
  evicted: number;
  skipped: number;
  pending?: DrainPod[]; // a PodDisruptionBudget held them back
  failed?: { pod: string; reason: string }[];
};

export function drainNode(
  name: string,
  opts: { evictUnmanaged: boolean; deleteLocalData: boolean },
): Promise<DrainResult> {
  return apiFetch(`${base(name)}/drain`, {
    method: "POST",
    headers: json,
    body: JSON.stringify({ confirmName: name, ...opts }),
  });
}

export async function setNodeLabels(
  name: string,
  change: { set: Record<string, string>; remove: string[] },
): Promise<void> {
  await apiFetch(`${base(name)}/labels`, {
    method: "PUT",
    headers: json,
    body: JSON.stringify(change),
  });
}

export type Taint = { key: string; value: string; effect: string };

export async function setNodeTaints(
  name: string,
  taints: Taint[],
): Promise<void> {
  await apiFetch(`${base(name)}/taints`, {
    method: "PUT",
    headers: json,
    body: JSON.stringify({ taints }),
  });
}

export async function deleteNode(name: string): Promise<void> {
  await apiFetch(base(name), {
    method: "DELETE",
    headers: json,
    body: JSON.stringify({ confirmName: name }),
  });
}
