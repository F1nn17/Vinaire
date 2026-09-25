import useSWR from "swr";
import { getDeployment, getPod, getWorkloads } from "@/lib/api/workloads";
import type { WorkloadKind } from "@/lib/types";

export function useWorkloads(kind: WorkloadKind, namespace = "") {
  return useSWR(["workloads", kind, namespace], () =>
    getWorkloads(kind, namespace),
  );
}

export function useDeployment(namespace: string, name: string) {
  return useSWR(["deployment", namespace, name], () =>
    getDeployment(namespace, name),
  );
}

export function usePod(namespace: string, name: string) {
  return useSWR(["pod", namespace, name], () => getPod(namespace, name));
}
