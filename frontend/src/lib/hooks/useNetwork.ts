import useSWR from "swr";
import { getGatewayResources, getIngresses } from "@/lib/api/network";
import { useCurrentCluster } from "@/lib/clusterStore";

export function useIngresses(namespace = "") {
  const cluster = useCurrentCluster();
  return useSWR(["ingresses", namespace, cluster], () =>
    getIngresses(namespace),
  );
}

export function useGatewayResources(namespace = "") {
  const cluster = useCurrentCluster();
  return useSWR(["gateway-resources", namespace, cluster], () =>
    getGatewayResources(namespace),
  );
}
