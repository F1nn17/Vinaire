import useSWR from "swr";
import { useCurrentCluster } from "@/lib/clusterStore";
import { apiFetch } from "./http";

// The logs stack (Roadmap Phase 45): what's in the cluster, and the one-click
// install plan. Matches the backend's api.LoggingStatus / LoggingPlan.

export type LoggingState = "none" | "loki-only" | "ready" | "configured";

export type LoggingStatus = {
  state: LoggingState;
  loki?: { namespace: string; service: string; port: number };
  collectors?: string[];
  // Loki answers and holds pod logs
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

export type LoggingPlan = {
  steps: PlanStep[];
  notes: string[];
  warnings: string[];
};

export type LoggingOptions = {
  namespace: string;
  retentionDays: number;
  storageSize: string; // "10Gi", or "none" for no volume
  storageClass: string;
};

// While an install runs — or while a collector has started but Loki holds no pod
// logs yet — look again every few seconds instead of leaving a stale "no logs yet".
export function useLoggingStatus(installing = false) {
  const cluster = useCurrentCluster();
  return useSWR(
    ["logging-status", cluster],
    () => apiFetch<LoggingStatus>("/api/v1/logging/status"),
    {
      refreshInterval: (d) =>
        installing || (d?.state === "ready" && !d.flowing) ? 4000 : 0,
      revalidateOnFocus: false,
    },
  );
}

export function useLoggingPlan(o: LoggingOptions, enabled: boolean) {
  const cluster = useCurrentCluster();
  return useSWR(
    enabled ? ["logging-plan", cluster, o] : null,
    () => {
      const q = new URLSearchParams({
        namespace: o.namespace,
        retentionDays: String(o.retentionDays),
        storageSize: o.storageSize,
      });
      if (o.storageClass) q.set("storageClass", o.storageClass);
      return apiFetch<LoggingPlan>(`/api/v1/logging/plan?${q.toString()}`);
    },
    {
      revalidateOnFocus: false,
      shouldRetryOnError: false,
      keepPreviousData: true,
    },
  );
}
