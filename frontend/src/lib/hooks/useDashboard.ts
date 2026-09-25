import useSWR from "swr";
import { getDashboard } from "@/lib/api/dashboard";
import { useCurrentCluster } from "@/lib/clusterStore";

export function useDashboard() {
  const cluster = useCurrentCluster();
  return useSWR(["dashboard", cluster], getDashboard);
}
