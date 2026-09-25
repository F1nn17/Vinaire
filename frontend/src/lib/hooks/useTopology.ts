import useSWR from "swr";
import { getTopology } from "@/lib/api/topology";
import { useCurrentCluster } from "@/lib/clusterStore";

export function useTopology(namespace: string) {
  const cluster = useCurrentCluster();
  return useSWR(namespace ? ["topology", namespace, cluster] : null, () =>
    getTopology(namespace),
  );
}
