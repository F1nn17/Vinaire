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
  // image-installed instances: the image now, and the one before the last upgrade
  image?: string;
  previousImage?: string;
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

// Changing the tag of an image-installed instance (Roadmap Phase 77d), or putting back the previous
// image. As the caller; the body repeats the instance's name.
export async function upgradeInstance(
  namespace: string,
  name: string,
  body: { tag: string; acceptMajor?: boolean; revert?: boolean },
): Promise<{ from: string; to: string }> {
  return apiFetch(
    `/api/v1/instances/${encodeURIComponent(namespace)}/${encodeURIComponent(name)}/upgrade`,
    {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ ...body, confirmName: name }),
    },
  );
}
