import type {
  ClusterEvent,
  DashboardData,
  DashboardSummary,
  StatCardData,
} from "@/lib/types";
import { apiFetch } from "./http";

// Real endpoints (Roadmap Phase 18). PVC-near-capacity and CPU/Memory stay
// explicit "not available yet" stubs here — Phase 13/16 built the real
// data sources, but wiring them into the Dashboard is out of this phase's
// scope (see ROADMAP.md's Phase 18 outcome note).
export async function getDashboard(): Promise<DashboardData> {
  const [summary, events] = await Promise.all([
    apiFetch<DashboardSummary>("/api/v1/dashboard/summary"),
    apiFetch<ClusterEvent[]>("/api/v1/events?limit=20"),
  ]);

  const statCards: StatCardData[] = [
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
      title: "Healthy pods",
      dotColor: "ok",
      value: summary.healthyPods,
      caption: `across ${summary.namespaces.length} namespace${summary.namespaces.length === 1 ? "" : "s"}`,
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
    cpu: null,
    memory: null,
    namespaces: summary.namespaces,
  };
}
