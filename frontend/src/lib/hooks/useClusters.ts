import useSWR from "swr";
import { getClusters } from "@/lib/api/clusters";

export function useClusters() {
  return useSWR("clusters", getClusters);
}
