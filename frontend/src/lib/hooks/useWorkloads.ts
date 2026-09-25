import useSWR from "swr";
import { getDeployment, getPod, getWorkloads } from "@/lib/api/workloads";
import { useCurrentCluster } from "@/lib/clusterStore";
import type { WorkloadKind } from "@/lib/types";

// The selected cluster (Roadmap Phase 11) is part of every key below so
// switching it in the Topbar refetches instead of showing stale data from
// the previous cluster — apiFetch itself reads the same selection to set
// the X-Cluster header (see lib/api/http.ts).
export function useWorkloads(kind: WorkloadKind, namespace = "") {
  const cluster = useCurrentCluster();
  return useSWR(["workloads", kind, namespace, cluster], () =>
    getWorkloads(kind, namespace),
  );
}

export function useDeployment(namespace: string, name: string) {
  const cluster = useCurrentCluster();
  return useSWR(["deployment", namespace, name, cluster], () =>
    getDeployment(namespace, name),
  );
}

export function usePod(namespace: string, name: string) {
  const cluster = useCurrentCluster();
  return useSWR(["pod", namespace, name, cluster], () =>
    getPod(namespace, name),
  );
}
