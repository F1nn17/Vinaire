import type {
  MetricRange,
  MetricResolution,
  MetricResourceType,
  MetricType,
  ResourceMetrics,
} from "@/lib/types";
import { apiFetch } from "./http";

// Real endpoints (Roadmap Phase 32). The backend builds the PromQL; the
// frontend only picks a metric, range and resolution. Nodes are
// cluster-scoped, so they have no namespace segment.
export function getResourceMetrics(
  resourceType: MetricResourceType,
  namespace: string,
  name: string,
  params: {
    type: MetricType;
    range: MetricRange;
    resolution: MetricResolution;
  },
): Promise<ResourceMetrics> {
  const base =
    resourceType === "cluster"
      ? "/api/v1/cluster/metrics"
      : resourceType === "nodes"
        ? `/api/v1/nodes/${encodeURIComponent(name)}/metrics`
        : `/api/v1/${resourceType}/${encodeURIComponent(namespace)}/${encodeURIComponent(name)}/metrics`;
  const qs = new URLSearchParams(params);
  return apiFetch<ResourceMetrics>(`${base}?${qs.toString()}`);
}
