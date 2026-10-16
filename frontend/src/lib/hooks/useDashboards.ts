import useSWR from "swr";
import { useCurrentCluster } from "@/lib/clusterStore";
import { loadPanel, queryNow, type PanelData } from "@/lib/dashboards/prom";
import { listCustomDashboards } from "@/lib/dashboards/store";
import { getDetectedApps } from "@/lib/api/apps";
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

// the applications found in the cluster (refreshed now and then: an install appears within a minute)
export function useDetectedApps() {
  const cluster = useCurrentCluster();
  return useSWR(["detected-apps", cluster], getDetectedApps, {
    refreshInterval: 60_000,
    shouldRetryOnError: false,
  });
}

// For every dashboard that names a required metric: does Prometheus have it? (id -> yes/no; a
// dashboard whose answer isn't in yet is simply missing from the map.)
export function useAvailability(dashboards: Dashboard[]) {
  const cluster = useCurrentCluster();
  const wanted = dashboards.filter(
    (d) => d.requires && metricRe.test(d.requires.metric),
  );
  const key = wanted.map((d) => `${d.id}=${d.requires?.metric}`).join(",");
  return useSWR<Record<string, boolean>>(
    wanted.length ? ["dash-availability", cluster, key] : null,
    async () => {
      const out: Record<string, boolean> = {};
      await Promise.all(
        wanted.map(async (d) => {
          try {
            out[d.id] = (await queryNow(`count(${d.requires?.metric})`)).some(
              (r) => r.v > 0,
            );
          } catch {
            out[d.id] = false;
          }
        }),
      );
      return out;
    },
    { refreshInterval: 60_000, revalidateOnFocus: false },
  );
}
