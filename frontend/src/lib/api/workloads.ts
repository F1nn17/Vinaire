import type {
  DaemonSetDetail,
  DeploymentDetail,
  JobDetail,
  NodeDetail,
  NodeRow,
  PodDetail,
  StatefulSetDetail,
  WorkloadKind,
  WorkloadRow,
} from "@/lib/types";
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

// Real endpoint.
export async function getDeployment(
  namespace: string,
  name: string,
): Promise<DeploymentDetail | undefined> {
  try {
    return await apiFetch<DeploymentDetail>(
      `/api/v1/deployments/${encodeURIComponent(namespace)}/${encodeURIComponent(name)}`,
    );
  } catch (err) {
    if (err instanceof ApiError && err.status === 404) {
      return undefined;
    }
    throw err;
  }
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

// Real endpoint (Roadmap Phase 12).
export async function getStatefulSet(
  namespace: string,
  name: string,
): Promise<StatefulSetDetail | undefined> {
  try {
    return await apiFetch<StatefulSetDetail>(
      `/api/v1/statefulsets/${encodeURIComponent(namespace)}/${encodeURIComponent(name)}`,
    );
  } catch (err) {
    if (err instanceof ApiError && err.status === 404) {
      return undefined;
    }
    throw err;
  }
}

// Real endpoint (Roadmap Phase 12).
export async function getDaemonSet(
  namespace: string,
  name: string,
): Promise<DaemonSetDetail | undefined> {
  try {
    return await apiFetch<DaemonSetDetail>(
      `/api/v1/daemonsets/${encodeURIComponent(namespace)}/${encodeURIComponent(name)}`,
    );
  } catch (err) {
    if (err instanceof ApiError && err.status === 404) {
      return undefined;
    }
    throw err;
  }
}

// Real endpoint (Roadmap Phase 19).
export function getNodes(): Promise<NodeRow[]> {
  return apiFetch<NodeRow[]>("/api/v1/nodes");
}

// Real endpoint (Roadmap Phase 19).
export async function getNode(name: string): Promise<NodeDetail | undefined> {
  try {
    return await apiFetch<NodeDetail>(
      `/api/v1/nodes/${encodeURIComponent(name)}`,
    );
  } catch (err) {
    if (err instanceof ApiError && err.status === 404) {
      return undefined;
    }
    throw err;
  }
}

// Real endpoint (Roadmap Phase 12).
export async function getJob(
  namespace: string,
  name: string,
): Promise<JobDetail | undefined> {
  try {
    return await apiFetch<JobDetail>(
      `/api/v1/jobs/${encodeURIComponent(namespace)}/${encodeURIComponent(name)}`,
    );
  } catch (err) {
    if (err instanceof ApiError && err.status === 404) {
      return undefined;
    }
    throw err;
  }
}
