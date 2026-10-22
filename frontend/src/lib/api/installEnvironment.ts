import useSWR from "swr";
import { useCurrentCluster } from "@/lib/clusterStore";
import { apiFetch } from "./http";

// Roadmap Phase 75b: what the cluster offers an install, read as the signed-in user (a part they may
// not read is reported as not known — "unknown" is never "missing").
export type InstallEnvironment = {
  storageClasses: string[];
  storageClassesKnown: boolean;
  defaultStorageClass: boolean;
  ingressClasses: string[];
  loadBalancer: boolean;
  loadBalancerKnown: boolean;
};

export function useInstallEnvironment() {
  const cluster = useCurrentCluster();
  return useSWR(
    ["install-environment", cluster],
    () => apiFetch<InstallEnvironment>("/api/v1/install/environment"),
    { revalidateOnFocus: false, shouldRetryOnError: false },
  );
}
