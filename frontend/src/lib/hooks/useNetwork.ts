import useSWR from "swr";
import { getGatewayResources, getIngresses } from "@/lib/api/network";

export function useIngresses() {
  return useSWR("ingresses", getIngresses);
}

export function useGatewayResources() {
  return useSWR("gateway-resources", getGatewayResources);
}
