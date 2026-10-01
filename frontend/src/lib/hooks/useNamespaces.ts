import useSWR from "swr";
import { useCurrentCluster } from "@/lib/clusterStore";
import { getNamespace, getNamespaces } from "@/lib/api/namespaces";

export function useNamespaces() {
  const cluster = useCurrentCluster();
  return useSWR(["namespaces", cluster], () => getNamespaces());
}

export function useNamespace(name: string) {
  const cluster = useCurrentCluster();
  return useSWR(["namespace", name, cluster], () => getNamespace(name));
}
