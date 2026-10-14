import useSWR from "swr";
import { useCurrentCluster } from "@/lib/clusterStore";
import { loadPanel, queryNow, type PanelData } from "@/lib/dashboards/prom";
import { listCustomDashboards } from "@/lib/dashboards/store";
import {
  metricRe,
  labelRe,
  type Dashboard,
  type DashboardVariable,
  type Panel,
} from "@/lib/dashboards/model";

// How often an open dashboard re-reads its panels. Every poll asks for the whole window ending at
// "now", so the chart slides.
const POLL_MS = 10_000;

export function usePanelData(
  dashboardId: string,
  index: number,
  panel: Panel,
  declared: string[],
  chosen: Record<string, string>,
  rangeSec: number,
  paused: boolean,
) {
  const cluster = useCurrentCluster();
  return useSWR<PanelData>(
    [
      "dash-panel",
      cluster,
      dashboardId,
      index,
      JSON.stringify(panel.queries),
      JSON.stringify(chosen),
      rangeSec,
    ],
    () => loadPanel(panel, declared, chosen, rangeSec),
    {
      refreshInterval: paused ? 0 : POLL_MS,
      revalidateOnFocus: !paused,
      keepPreviousData: true,
    },
  );
}

// the values a variable can take (the label's values on its metric), sorted
export function useVariableOptions(v: DashboardVariable) {
  const cluster = useCurrentCluster();
  const ok = metricRe.test(v.metric) && labelRe.test(v.by);
  return useSWR<string[]>(
    ok ? ["dash-var", cluster, v.metric, v.by] : null,
    async () => {
      const rows = await queryNow(`count by (${v.by}) (${v.metric})`);
      return rows
        .map((r) => r.metric[v.by])
        .filter(Boolean)
        .sort();
    },
    { revalidateOnFocus: false },
  );
}

// whether the metric a dashboard needs exists at all
export function useRequirement(requires: Dashboard["requires"]) {
  const cluster = useCurrentCluster();
  const metric = requires?.metric;
  return useSWR<boolean>(
    metric && metricRe.test(metric) ? ["dash-req", cluster, metric] : null,
    async () => (await queryNow(`count(${metric})`)).some((r) => r.v > 0),
    { revalidateOnFocus: false, shouldRetryOnError: false },
  );
}

export function useCustomDashboards() {
  const cluster = useCurrentCluster();
  return useSWR(["dash-custom", cluster], listCustomDashboards, {
    refreshInterval: 30_000,
  });
}
