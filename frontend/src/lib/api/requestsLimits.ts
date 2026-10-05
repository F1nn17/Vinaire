import useSWR from "swr";
import { useCurrentCluster } from "@/lib/clusterStore";
import { apiFetch } from "./http";

// Matches the backend's api.ResourceOverview. Quantities are raw numbers —
// millicores for CPU, bytes for memory and storage, plain counts for pods — so
// the charts and the unit formatting live here, not on the server.

export type ResourceFigure = {
  // what can be scheduled (cluster and nodes only; a namespace has no capacity)
  allocatable?: number;
  // reserved by the scheduler
  requests: number;
  // the ceiling a container may burst to
  limits: number;
  // what is used right now (needs metrics-server)
  usage?: number;
  // containers that set none, so requests / limits are a lower bound
  containersWithoutRequests?: number;
  containersWithoutLimits?: number;
};

export type ResourceRow = {
  name: string;
  cpu: ResourceFigure; // millicores
  memory: ResourceFigure; // bytes
  ephemeralStorage: ResourceFigure; // bytes
  pods: number;
  podsAllocatable?: number;
  containers: number;
};

export type ResourceOverview = {
  cluster: ResourceRow;
  nodes: ResourceRow[];
  namespaces: ResourceRow[];
  metricsAvailable: boolean;
};

export function getRequestsLimits(): Promise<ResourceOverview> {
  return apiFetch("/api/v1/requests-limits");
}

export function useRequestsLimits() {
  const cluster = useCurrentCluster();
  return useSWR(["requests-limits", cluster], () => getRequestsLimits(), {
    refreshInterval: 20000,
  });
}

// ---- formatting ---------------------------------------------------------------

export function formatCpu(milli: number): string {
  if (milli >= 1000) {
    const cores = milli / 1000;
    return `${cores >= 10 ? cores.toFixed(0) : cores.toFixed(2).replace(/\.?0+$/, "")} cores`;
  }
  return `${Math.round(milli)}m`;
}

export function formatBytes(n: number): string {
  const units = ["B", "KiB", "MiB", "GiB", "TiB"];
  let v = Math.abs(n);
  let i = 0;
  while (v >= 1024 && i < units.length - 1) {
    v /= 1024;
    i++;
  }
  return `${v >= 100 || i === 0 ? v.toFixed(0) : v.toFixed(1)} ${units[i]}`;
}

export type Unit = "cpu" | "bytes" | "count";

export function formatValue(v: number, unit: Unit): string {
  if (unit === "cpu") return formatCpu(v);
  if (unit === "bytes") return formatBytes(v);
  return String(Math.round(v));
}

// requests as a share of a total, as a whole percent (0 when the total is unknown)
export function pctOf(
  part: number | undefined,
  total: number | undefined,
): number {
  if (!part || !total || total <= 0) return 0;
  return Math.round((part / total) * 100);
}

// Same thresholds as the Nodes capacity view: warn from 80%, err from 95%.
export function levelFor(pct: number): "ok" | "warn" | "err" {
  if (pct >= 95) return "err";
  if (pct >= 80) return "warn";
  return "ok";
}
