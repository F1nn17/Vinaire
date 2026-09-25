import useSWR from "swr";
import { getSecrets } from "@/lib/api/secrets";
import { useCurrentCluster } from "@/lib/clusterStore";

export function useSecrets(namespace = "") {
  const cluster = useCurrentCluster();
  return useSWR(["secrets", namespace, cluster], () => getSecrets(namespace));
}
