import useSWR from "swr";
import {
  getDaemonSet,
  getDeployment,
  getJob,
  getNode,
  getNodes,
  getPod,
  getStatefulSet,
  getWorkloads,
} from "@/lib/api/workloads";
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

export function useStatefulSet(namespace: string, name: string) {
  const cluster = useCurrentCluster();
  return useSWR(["statefulset", namespace, name, cluster], () =>
    getStatefulSet(namespace, name),
  );
}

export function useDaemonSet(namespace: string, name: string) {
  const cluster = useCurrentCluster();
  return useSWR(["daemonset", namespace, name, cluster], () =>
    getDaemonSet(namespace, name),
  );
}

export function useJob(namespace: string, name: string) {
  const cluster = useCurrentCluster();
  return useSWR(["job", namespace, name, cluster], () =>
    getJob(namespace, name),
  );
}

export function useNodes() {
  const cluster = useCurrentCluster();
  return useSWR(["nodes", cluster], () => getNodes());
}

export function useNode(name: string) {
  const cluster = useCurrentCluster();
  return useSWR(["node", name, cluster], () => getNode(name));
}
