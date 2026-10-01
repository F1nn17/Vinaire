import useSWR from "swr";
import { useCurrentCluster } from "@/lib/clusterStore";
import {
  getCatalogRepos,
  getChart,
  getReleaseHistory,
  getReleases,
  searchCatalog,
} from "@/lib/api/catalog";

// The catalog itself doesn't depend on the selected cluster (it's the same list
// of repositories everywhere), but releases do.
export function useCatalogRepos() {
  return useSWR("catalog-repos", () => getCatalogRepos(), {
    revalidateOnFocus: false,
  });
}

export function useCatalogSearch(search: string, repo: string) {
  return useSWR(
    ["catalog-search", search, repo],
    () => searchCatalog(search, repo),
    { revalidateOnFocus: false, keepPreviousData: true },
  );
}

export function useChart(repo: string, chart: string, version?: string) {
  return useSWR(
    repo && chart ? ["catalog-chart", repo, chart, version ?? ""] : null,
    () => getChart(repo, chart, version),
    { revalidateOnFocus: false, keepPreviousData: true },
  );
}

export function useReleases(namespace?: string) {
  const cluster = useCurrentCluster();
  return useSWR(["helm-releases", namespace ?? "", cluster], () =>
    getReleases(namespace),
  );
}

export function useReleaseHistory(namespace: string, name: string) {
  const cluster = useCurrentCluster();
  return useSWR(["helm-history", namespace, name, cluster], () =>
    getReleaseHistory(namespace, name),
  );
}
