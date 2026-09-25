import useSWR from "swr";
import { getConfigMaps } from "@/lib/api/configmaps";

export function useConfigMaps(namespace = "") {
  return useSWR(["configmaps", namespace], () => getConfigMaps(namespace));
}
