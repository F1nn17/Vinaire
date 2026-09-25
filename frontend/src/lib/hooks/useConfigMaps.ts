import useSWR from "swr";
import { getConfigMaps } from "@/lib/api/configmaps";
import { useCurrentCluster } from "@/lib/clusterStore";

export function useConfigMaps(namespace = "") {
  const cluster = useCurrentCluster();
  return useSWR(["configmaps", namespace, cluster], () =>
    getConfigMaps(namespace),
  );
}
