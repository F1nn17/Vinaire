import useSWR from "swr";
import { getGatewayResources, getIngresses } from "@/lib/api/network";

export function useIngresses(namespace = "") {
  return useSWR(["ingresses", namespace], () => getIngresses(namespace));
}

export function useGatewayResources(namespace = "") {
  return useSWR(["gateway-resources", namespace], () =>
    getGatewayResources(namespace),
  );
}
