import { apiFetch } from "./http";

// Roadmap Phase 25's generic YAML create/update — a deliberate, audited
// extension to AGENTS.md's write rule. The backend always dry-runs before
// the real write; the caller's own Kubernetes RBAC decides what's allowed.

export type ApplyMode = "create" | "update";

export type DryRunResult = {
  kind: string;
  apiVersion: string;
  namespace: string;
  name: string;
  before: string;
  after: string;
  // Live version the diff was computed against; required for an update.
  resourceVersion?: string;
};

const json = { "Content-Type": "application/json" };

export function dryRunResource(
  yaml: string,
  mode: ApplyMode,
): Promise<DryRunResult> {
  return apiFetch<DryRunResult>("/api/v1/resources/dry-run", {
    method: "POST",
    headers: json,
    body: JSON.stringify({ yaml, mode }),
  });
}

export async function createResource(yaml: string): Promise<void> {
  await apiFetch("/api/v1/resources", {
    method: "POST",
    headers: json,
    body: JSON.stringify({ yaml }),
  });
}

export async function updateResource(
  yaml: string,
  resourceVersion: string,
): Promise<void> {
  await apiFetch("/api/v1/resources", {
    method: "PUT",
    headers: json,
    body: JSON.stringify({ yaml, resourceVersion }),
  });
}
