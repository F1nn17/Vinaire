import useSWR from "swr";
import { getTopology } from "@/lib/api/topology";

export function useTopology() {
  return useSWR("topology", getTopology);
}
