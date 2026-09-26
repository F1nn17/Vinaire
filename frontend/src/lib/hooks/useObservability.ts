import useSWR from "swr";
import { getAlerts, searchLogs } from "@/lib/api/observability";
import { useCurrentCluster } from "@/lib/clusterStore";

// Polls Alertmanager every 30s for the Topbar bell — frequent enough to
// feel live without hammering an external system on every render.
const alertsRefreshIntervalMs = 30_000;

export function useAlerts() {
  const cluster = useCurrentCluster();
  return useSWR(["alerts", cluster], () => getAlerts(), {
    refreshInterval: alertsRefreshIntervalMs,
  });
}

// `enabled` lets the Logs tab's history search only fire on demand (a
// button click), not on every keystroke.
export function useLogsSearch(
  namespace: string,
  name: string,
  query: string,
  since: string,
  enabled: boolean,
) {
  const cluster = useCurrentCluster();
  return useSWR(
    enabled ? ["logs-search", namespace, name, query, since, cluster] : null,
    () => searchLogs(namespace, name, query, since),
  );
}
