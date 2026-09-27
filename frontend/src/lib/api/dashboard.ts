import type {
  ClusterEvent,
  DashboardData,
  DashboardSummary,
  StatCardData,
} from "@/lib/types";
import { apiFetch } from "./http";

// Real endpoints (Roadmap Phase 18, CPU/Memory wired up as a follow-up).
// PVC-near-capacity stays an explicit "not available yet" stub — Phase 13
// built the real data source, but wiring it into the Dashboard is still
// out of scope (see ROADMAP.md's Phase 18 outcome note).
export async function getDashboard(): Promise<DashboardData> {
  const [summary, events] = await Promise.all([
    apiFetch<DashboardSummary>("/api/v1/dashboard/summary"),
    apiFetch<ClusterEvent[]>("/api/v1/events?limit=20"),
  ]);

  const statCards: StatCardData[] = [
    {
      title: "Healthy pods",
      dotColor: "ok",
      value: summary.healthyPods,
      caption: `across ${summary.namespaces.length} namespace${summary.namespaces.length === 1 ? "" : "s"}`,
    },
    {
      title: "Pods down",
      dotColor: summary.podsDown > 0 ? "err" : "ok",
      value: summary.podsDown,
      caption:
        summary.podsDownSample.length > 0
          ? summary.podsDownSample.join(", ")
          : "All pods healthy",
    },
    {
      title: "PVC near capacity",
      dotColor: "warn",
      value: null,
      caption: "Not available yet",
    },
  ];

  return {
    statCards,
    driftedFromGit: summary.argoCDAvailable
      ? {
          count: summary.driftedCount,
          dotColor: summary.driftedCount > 0 ? "warn" : "ok",
          caption:
            summary.driftedSample.length > 0
              ? summary.driftedSample.join(", ")
              : "All apps in sync",
        }
      : null,
    events,
    cpu: summary.cpu
      ? {
          used: summary.cpu.used,
          total: summary.cpu.limit,
          pct: summary.cpu.pct,
        }
      : null,
    memory: summary.memory
      ? {
          used: summary.memory.used,
          total: summary.memory.limit,
          pct: summary.memory.pct,
        }
      : null,
    namespaces: summary.namespaces,
  };
}
