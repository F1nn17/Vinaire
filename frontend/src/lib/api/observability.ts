import type {
  AlertsResult,
  LogsSearchResult,
  MetricsQueryResult,
} from "@/lib/types";
import { apiFetch } from "./http";

// Real endpoints (Roadmap Phase 16) — all three proxy to an
// already-running Prometheus/Loki/Alertmanager stack, if configured, and
// come back as an explicit `{available: false, reason}` otherwise (never a
// thrown error for "just isn't installed").

export function getAlerts(): Promise<AlertsResult> {
  return apiFetch<AlertsResult>("/api/v1/alerts");
}

export function searchLogs(
  namespace: string,
  name: string,
  query: string,
  since: string,
): Promise<LogsSearchResult> {
  const params = new URLSearchParams();
  if (query) params.set("query", query);
  if (since) params.set("since", since);
  const qs = params.toString();
  return apiFetch<LogsSearchResult>(
    `/api/v1/pods/${encodeURIComponent(namespace)}/${encodeURIComponent(name)}/logs/search${qs ? `?${qs}` : ""}`,
  );
}

export function queryMetrics(query: string): Promise<MetricsQueryResult> {
  return apiFetch<MetricsQueryResult>(
    `/api/v1/metrics/query?query=${encodeURIComponent(query)}`,
  );
}

export function queryMetricsRange(
  query: string,
  start: string,
  end: string,
  step: string,
): Promise<MetricsQueryResult> {
  const params = new URLSearchParams({ query, start, end, step });
  return apiFetch<MetricsQueryResult>(
    `/api/v1/metrics/query_range?${params.toString()}`,
  );
}
