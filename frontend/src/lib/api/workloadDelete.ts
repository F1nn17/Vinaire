import { apiFetch } from "./http";

// Deleting a StatefulSet, a DaemonSet or a Job (the owner's decision of 2026-10-09: the
// multi-select in the Workloads lists works on every tab). Plain deletes as the caller; the
// backend removes the pods in the background, as `kubectl delete` does.
const del = (kind: string, namespace: string, name: string) =>
  apiFetch(
    `/api/v1/${kind}/${encodeURIComponent(namespace)}/${encodeURIComponent(name)}`,
    { method: "DELETE" },
  );

export async function deleteStatefulSet(
  namespace: string,
  name: string,
): Promise<void> {
  await del("statefulsets", namespace, name);
}

export async function deleteDaemonSet(
  namespace: string,
  name: string,
): Promise<void> {
  await del("daemonsets", namespace, name);
}

export async function deleteJob(
  namespace: string,
  name: string,
): Promise<void> {
  await del("jobs", namespace, name);
}
