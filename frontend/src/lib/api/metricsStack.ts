import useSWR from "swr";
import { useCurrentCluster } from "@/lib/clusterStore";
import { apiFetch } from "./http";

// The metrics stack (Roadmap Phase 37): what's in the cluster, and the one-click
// install plan. Matches the backend's api.MetricsStatus / MetricsPlan.

export type MetricsState =
  | "none"
  | "metrics-server-only"
  | "partial"
  | "prometheus-ready"
  | "configured";

export type MetricsStatus = {
  state: MetricsState;
  metricsServer: boolean;
  prometheus?: { namespace: string; service: string; port: number };
  kubeStateMetrics: boolean;
  nodeExporter: boolean;
  operatorFound: boolean;
  alertmanager?: { namespace: string; service: string; port: number };
  // set when Vinaire installed this Prometheus
  profile?: "standard" | "full";
  release?: string;
  missing?: string[];
  // Prometheus answers and holds container metrics
  flowing: boolean;
  detail?: string;
  managedByVinaire: boolean;
  configuredUrl: boolean;
};

export type PlanStep = {
  title: string;
  repo: string;
  chart: string;
  version: string;
  namespace: string;
  release: string;
  values: string;
  createNamespace: boolean;
  wait: boolean;
};

export type MetricsPlan = {
  steps: PlanStep[];
  notes: string[];
  warnings: string[];
};

export type MetricsProfile = "lite" | "standard" | "full";

export type MetricsOptions = {
  profile: MetricsProfile;
  nodeSelector: string; // "key=value", empty = anywhere
  alertmanager: boolean;
  namespace: string;
  retentionDays: number;
  storageSize: string; // "10Gi", or "none" for no volume
  storageClass: string;
};

// While an install runs — or while Prometheus has started but holds no container
// metrics yet — look again every few seconds instead of leaving a stale "no metrics yet".
export function useMetricsStatus(installing = false) {
  const cluster = useCurrentCluster();
  return useSWR(
    ["metrics-status", cluster],
    () => apiFetch<MetricsStatus>("/api/v1/metrics/status"),
    {
      refreshInterval: (d) =>
        installing ||
        ((d?.state === "prometheus-ready" || d?.state === "partial") &&
          !d.flowing)
          ? 4000
          : 0,
      revalidateOnFocus: false,
    },
  );
}

export function useMetricsPlan(o: MetricsOptions, enabled: boolean) {
  const cluster = useCurrentCluster();
  return useSWR(
    enabled ? ["metrics-plan", cluster, o] : null,
    () => {
      const q = new URLSearchParams({
        profile: o.profile,
        namespace: o.namespace,
        retentionDays: String(o.retentionDays),
        storageSize: o.storageSize,
      });
      if (o.storageClass) q.set("storageClass", o.storageClass);
      if (o.nodeSelector) q.set("nodeSelector", o.nodeSelector);
      if (o.alertmanager) q.set("alertmanager", "true");
      return apiFetch<MetricsPlan>(`/api/v1/metrics/plan?${q.toString()}`);
    },
    {
      revalidateOnFocus: false,
      shouldRetryOnError: false,
      keepPreviousData: true,
    },
  );
}
