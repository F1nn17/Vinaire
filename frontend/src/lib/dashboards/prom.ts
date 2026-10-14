import { apiFetch } from "@/lib/api/http";
import { type Panel, substitute } from "./model";

// Reads from Prometheus go through the same proxy the Metrics tab uses (/api/v1/metrics/*), which
// asks the cluster's Prometheus as the signed-in user — their own RBAC decides, and nothing here
// is written anywhere.

type PromBody = {
  status?: string;
  error?: string;
  data?: {
    resultType?: string;
    result?: {
      metric?: Record<string, string>;
      value?: [number, string];
      values?: [number, string][];
    }[];
  };
};

type Proxied = { available: boolean; reason?: string; result?: PromBody };

export type Series = {
  name: string;
  points: { t: number; v: number }[];
};

async function ask(
  path: string,
  params: Record<string, string>,
): Promise<PromBody> {
  const res = await apiFetch<Proxied>(
    `${path}?${new URLSearchParams(params).toString()}`,
  );
  if (!res.available)
    throw new Error(res.reason || "Prometheus is not available");
  if (res.result?.status && res.result.status !== "success") {
    throw new Error(res.result.error || "The query failed");
  }
  return res.result ?? {};
}

// A series' name: the legend with {label} filled in, else the labels' values, else "value".
export function seriesName(
  legend: string | undefined,
  metric: Record<string, string>,
): string {
  if (legend) {
    const named = legend
      .replace(/\{(\w+)\}/g, (_, k: string) => metric[k] ?? "")
      .trim();
    if (named) return named;
  }
  const values = Object.entries(metric)
    .filter(([k]) => k !== "__name__")
    .map(([, v]) => v);
  if (values.length) return values.join(" ");
  // a legend that names a label this series doesn't have ("{pod}" on a series without a pod) must not
  // show its raw placeholder
  return legend && !/\{\w+\}/.test(legend) ? legend : "(no label)";
}

export async function queryRange(
  expr: string,
  legend: string | undefined,
  rangeSec: number,
): Promise<Series[]> {
  const end = Math.floor(Date.now() / 1000);
  const step = Math.max(15, Math.ceil(rangeSec / 120));
  const body = await ask("/api/v1/metrics/query_range", {
    query: expr,
    start: String(end - rangeSec),
    end: String(end),
    step: String(step),
  });
  return (body.data?.result ?? []).slice(0, 40).map((r) => ({
    name: seriesName(legend, r.metric ?? {}),
    points: (r.values ?? [])
      .map(([t, v]) => ({ t: t * 1000, v: Number(v) }))
      .filter((p) => Number.isFinite(p.v)),
  }));
}

export async function queryNow(
  expr: string,
): Promise<{ metric: Record<string, string>; v: number }[]> {
  const body = await ask("/api/v1/metrics/query", { query: expr });
  return (body.data?.result ?? [])
    .map((r) => ({ metric: r.metric ?? {}, v: Number(r.value?.[1]) }))
    .filter((r) => Number.isFinite(r.v));
}

export type PanelData = { series: Series[]; error?: string };

// All of a panel's queries, drawn together; one that fails doesn't take the others down.
export async function loadPanel(
  panel: Panel,
  declared: string[],
  chosen: Record<string, string>,
  rangeSec: number,
): Promise<PanelData> {
  const parts = await Promise.allSettled(
    panel.queries.map((q) =>
      queryRange(substitute(q.expr, declared, chosen), q.legend, rangeSec),
    ),
  );
  const series: Series[] = [];
  let error: string | undefined;
  for (const p of parts) {
    if (p.status === "fulfilled") series.push(...p.value);
    else
      error = p.reason instanceof Error ? p.reason.message : String(p.reason);
  }
  return { series, error: series.length === 0 ? error : undefined };
}
