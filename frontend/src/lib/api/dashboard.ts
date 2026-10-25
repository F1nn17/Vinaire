import type {
  ClusterEvent,
  DashboardData,
  DashboardSummary,
  StatCardData,
} from "@/lib/types";
import { apiFetch } from "./http";
import { t } from "@/lib/i18n/translate";

// Real endpoints (Roadmap Phase 18, CPU/Memory wired up as a follow-up). The PVC card was a
// "not available yet" stub until Roadmap Phase 90: the backend now reads the kubelet's volume
// numbers and says whether some claims are near their size, none is, or nobody can tell.
function pvcCard(pvc: DashboardSummary["pvc"]): StatCardData {
  const title = "PVC near capacity";
  if (!pvc)
    return { title, dotColor: "warn", value: null, caption: "Not available" };
  if (pvc.state === "nodata") {
    return {
      title,
      dotColor: "warn",
      value: null,
      caption: t("{n} claims, usage unknown — {reason}", {
        n: pvc.total,
        reason: t(pvc.reason ?? "no data"),
      }),
    };
  }
  if (pvc.state === "near") {
    return {
      title,
      dotColor: "err",
      value: pvc.count,
      caption: pvc.sample.join(", "),
    };
  }
  return {
    title,
    dotColor: "ok",
    value: 0,
    caption:
      pvc.total === 0
        ? "No claims"
        : t("{n} of {m} measured, all below {p}%", {
            n: pvc.measured,
            m: pvc.total,
            p: pvc.threshold,
          }),
  };
}

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
    pvcCard(summary.pvc),
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
    disk: summary.disk
      ? {
          used: summary.disk.used,
          total: summary.disk.limit,
          pct: summary.disk.pct,
        }
      : null,
    namespaces: summary.namespaces,
  };
}
