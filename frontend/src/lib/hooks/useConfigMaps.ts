import useSWR from "swr";
import { getConfigMaps } from "@/lib/api/configmaps";

export function useConfigMaps() {
  return useSWR("configmaps", getConfigMaps);
}
