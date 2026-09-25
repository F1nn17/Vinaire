import { deploymentDetailsMock } from "@/lib/mock/deploymentDetails";
import { podDetailsMock } from "@/lib/mock/podDetails";
import type {
  DeploymentDetail,
  PodDetail,
  WorkloadKind,
  WorkloadRow,
} from "@/lib/types";
import { simulatedDelay } from "./config";
import { apiFetch } from "./http";

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

// TODO(vinaire, Phase 3): replace with
// `fetch(`${API_BASE}/api/v1/pods/${namespace}/${name}`)`. Signature already
// takes `namespace` (Phase 1 routing fix) even though the lookup is still
// name-only mock data for now.
export async function getPod(
  namespace: string,
  name: string,
): Promise<PodDetail | undefined> {
  void namespace;
  await simulatedDelay();
  return podDetailsMock[name];
}
