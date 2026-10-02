import useSWR from "swr";
import { useCurrentCluster } from "@/lib/clusterStore";
import { getArgoApplications, getGitOps } from "@/lib/api/gitops";

// The overview carries the mode and every source with its live status, and a
// source syncs in the background, so it is polled — quicker while something is
// in flight.
export function useGitOps() {
  const cluster = useCurrentCluster();
  return useSWR(["gitops", cluster], () => getGitOps(), {
    refreshInterval: (data) =>
      data?.sources.some(
        (s) => s.status.phase === "Syncing" || s.status.phase === "Pending",
      )
        ? 2000
        : 8000,
  });
}

export function useArgoApplications(enabled: boolean) {
  const cluster = useCurrentCluster();
  return useSWR(
    enabled ? ["gitops-argo", cluster] : null,
    () => getArgoApplications(),
    { refreshInterval: 6000 },
  );
}
