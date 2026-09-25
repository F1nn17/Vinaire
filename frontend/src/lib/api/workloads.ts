import { deploymentDetailsMock } from "@/lib/mock/deploymentDetails";
import type {
  DeploymentDetail,
  PodDetail,
  WorkloadKind,
  WorkloadRow,
} from "@/lib/types";
import { simulatedDelay } from "./config";
import { apiFetch, ApiError } from "./http";

// Real endpoint (Roadmap Phase 1) — backend derives status/color/age and
// returns rows shaped exactly like WorkloadRow, so no mapping happens here.
export function getWorkloads(
  kind: WorkloadKind,
  namespace = "",
): Promise<WorkloadRow[]> {
  const query = namespace ? `?namespace=${encodeURIComponent(namespace)}` : "";
  return apiFetch<WorkloadRow[]>(`/api/v1/${kind}${query}`);
}

// TODO(vinaire, Phase 3): replace with
// `fetch(`${API_BASE}/api/v1/deployments/${namespace}/${name}`)`. Signature
// already takes `namespace` (Phase 1 routing fix) even though the lookup is
// still name-only mock data for now.
export async function getDeployment(
  namespace: string,
  name: string,
): Promise<DeploymentDetail | undefined> {
  void namespace;
  await simulatedDelay();
  return deploymentDetailsMock[name];
}

// Real endpoint (Roadmap Phase 3) — Overview and Events come from the real
// pod; Logs/Terminal/YAML tabs still show mock placeholders until their own
// phases (the backend already returns real YAML too, since it's free once
// the pod object is in hand).
export async function getPod(
  namespace: string,
  name: string,
): Promise<PodDetail | undefined> {
  try {
    return await apiFetch<PodDetail>(
      `/api/v1/pods/${encodeURIComponent(namespace)}/${encodeURIComponent(name)}`,
    );
  } catch (err) {
    if (err instanceof ApiError && err.status === 404) {
      return undefined;
    }
    throw err;
  }
}
