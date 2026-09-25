import useSWR from "swr";
import { getTopology } from "@/lib/api/topology";

export function useTopology(namespace: string) {
  return useSWR(namespace ? ["topology", namespace] : null, () =>
    getTopology(namespace),
  );
}
