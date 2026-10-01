import useSWR from "swr";
import { useCurrentCluster } from "@/lib/clusterStore";
import { getResourceMetrics } from "@/lib/api/metrics";
import type {
  MetricRange,
  MetricResolution,
  MetricResourceType,
  MetricType,
} from "@/lib/types";

// How often the Metrics tab polls while it's open and not paused. Each poll
// re-fetches the whole selected window ending at "now", so the oldest points
// fall off the left edge — a sliding window.
const POLL_MS = 10_000;

// `paused` really stops the polling (refreshInterval 0, no revalidation on
// focus/reconnect) rather than just hiding the chart; the last data stays on
// screen. Changing the metric/range/resolution still fetches once.
export function useResourceMetrics(
  resourceType: MetricResourceType,
  namespace: string,
  name: string,
  type: MetricType,
  range: MetricRange,
  resolution: MetricResolution,
  paused: boolean,
) {
  const cluster = useCurrentCluster();
  return useSWR(
    [
      "metrics",
      resourceType,
      namespace,
      name,
      type,
      range,
      resolution,
      cluster,
    ],
    () =>
      getResourceMetrics(resourceType, namespace, name, {
        type,
        range,
        resolution,
      }),
    {
      refreshInterval: paused ? 0 : POLL_MS,
      revalidateOnFocus: !paused,
      revalidateOnReconnect: !paused,
      keepPreviousData: true,
    },
  );
}
