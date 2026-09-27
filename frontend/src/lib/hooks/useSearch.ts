import useSWR from "swr";
import { search } from "@/lib/api/search";
import { useCurrentCluster } from "@/lib/clusterStore";

export function useSearch(query: string) {
  const cluster = useCurrentCluster();
  const trimmed = query.trim();
  return useSWR(trimmed ? ["search", trimmed, cluster] : null, () =>
    search(trimmed),
  );
}
