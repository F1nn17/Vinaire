import useSWR from "swr";
import { apiFetch } from "./http";
import { useCurrentCluster } from "@/lib/clusterStore";

// Installed applications (Roadmap Phase 77a): one "instance" is everything a Marketplace install or
// "Install from an image" created under one name. GET /api/v1/instances only reads, as the caller; the
// delete in the UI is made of calls that already exist (Helm uninstall, workload and PVC deletes).
export type InstancePart = {
  kind: string;
  name: string;
  detail?: string;
  fate: "removed" | "stays";
  why?: string;
};

export type Instance = {
  namespace: string;
  name: string;
  origin: "image" | "helm";
  app?: string;
  version?: string;
  system: boolean;
  parts: InstancePart[];
  stays: number;
};

export type InstancesResponse = {
  instances: Instance[];
  // kinds the caller may not list; their objects are missing from the instances
  unreadable: string[];
};

export function useInstances() {
  const cluster = useCurrentCluster();
  return useSWR(["instances", cluster], () =>
    apiFetch<InstancesResponse>("/api/v1/instances"),
  );
}
