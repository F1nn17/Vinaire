"use client";

import Link from "next/link";
import { useState } from "react";
import {
  CartesianGrid,
  Legend,
  Line,
  LineChart,
  ResponsiveContainer,
  Tooltip,
  XAxis,
  YAxis,
} from "recharts";
import { PillTabs } from "@/components/ui/Tabs";
import { useResourceMetrics } from "@/lib/hooks/useResourceMetrics";
import { friendlyErrorMessage } from "@/lib/api/errors";
import type {
  MetricRange,
  MetricResolution,
  MetricResourceType,
  MetricType,
  ResourceMetrics,
} from "@/lib/types";

const typeTabs: { key: MetricType; label: string }[] = [
  { key: "cpu", label: "CPU" },
  { key: "memory", label: "Memory" },
  { key: "network", label: "Network" },
  { key: "filesystem", label: "Filesystem" },
];

const ranges: { key: MetricRange; label: string }[] = [
  { key: "1h", label: "Last 1 hour" },
  { key: "6h", label: "Last 6 hours" },
  { key: "24h", label: "Last 24 hours" },
  { key: "7d", label: "Last 7 days" },
];

const resolutions: { key: MetricResolution; label: string }[] = [
  { key: "low", label: "Low res." },
  { key: "medium", label: "Medium res." },
  { key: "high", label: "High res." },
];

const seriesColors = [
  "var(--accent)",
  "var(--warn)",
  "var(--ok)",
  "var(--err)",
];

function humanBytes(v: number): string {
  const units = ["B", "KiB", "MiB", "GiB", "TiB"];
  let n = Math.abs(v);
  let i = 0;
  while (n >= 1024 && i < units.length - 1) {
    n /= 1024;
    i++;
  }
  const sign = v < 0 ? "-" : "";
  return `${sign}${n >= 100 || i === 0 ? n.toFixed(0) : n.toFixed(1)} ${units[i]}`;
}

function formatValue(v: number, unit: ResourceMetrics["unit"]): string {
  switch (unit) {
    case "cores":
      if (Math.abs(v) >= 0.1) return v.toFixed(3);
      return `${(v * 1000).toFixed(Math.abs(v) < 0.001 ? 2 : 1)}m`;
    case "bytes":
      return humanBytes(v);
    case "bytes/s":
      return `${humanBytes(v)}/s`;
  }
}

function formatTick(ts: number, rangeSeconds: number): string {
  const d = new Date(ts * 1000);
  const time = d.toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" });
  return rangeSeconds > 24 * 3600
    ? `${d.toLocaleDateString([], { month: "short", day: "numeric" })} ${time}`
    : time;
}

// Merge series into one row per timestamp, the shape recharts wants.
function toRows(data: ResourceMetrics): Record<string, number>[] {
  const byTs = new Map<number, Record<string, number>>();
  for (const s of data.series) {
    for (const [t, v] of s.points) {
      const row = byTs.get(t) ?? { t };
      row[s.name] = v;
      byTs.set(t, row);
    }
  }
  return [...byTs.values()].sort((a, b) => a.t - b.t);
}

const selectClass =
  "rounded-lg border border-border bg-surface px-2.5 py-1.5 text-xs outline-none";

// The Metrics tab (Roadmap Phase 32): a detailed history chart, separate from
// Overview's current-value cards. Polls while open unless paused; renders an
// explicit state instead of an empty chart when Prometheus is unavailable or
// has no samples.
export function MetricsPanel({
  resourceType,
  namespace,
  name,
}: {
  resourceType: MetricResourceType;
  namespace: string;
  name: string;
}) {
  const [type, setType] = useState<MetricType>("cpu");
  const [range, setRange] = useState<MetricRange>("1h");
  const [resolution, setResolution] = useState<MetricResolution>("medium");
  const [paused, setPaused] = useState(false);

  const { data, error, isLoading } = useResourceMetrics(
    resourceType,
    namespace,
    name,
    type,
    range,
    resolution,
    paused,
  );

  return (
    <div className="flex flex-col gap-4">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <PillTabs items={typeTabs} active={type} onChange={setType} />
        <div className="flex items-center gap-2">
          <button
            type="button"
            onClick={() => setPaused((p) => !p)}
            aria-pressed={paused}
            className="rounded-lg border border-border bg-surface px-3 py-1.5 text-xs"
          >
            {paused ? "Resume" : "Pause"}
          </button>
          <select
            aria-label="Time range"
            value={range}
            onChange={(e) => setRange(e.target.value as MetricRange)}
            className={selectClass}
          >
            {ranges.map((r) => (
              <option key={r.key} value={r.key}>
                {r.label}
              </option>
            ))}
          </select>
          <select
            aria-label="Resolution"
            value={resolution}
            onChange={(e) => setResolution(e.target.value as MetricResolution)}
            className={selectClass}
          >
            {resolutions.map((r) => (
              <option key={r.key} value={r.key}>
                {r.label}
              </option>
            ))}
          </select>
        </div>
      </div>

      <div className="rounded-[10px] border border-border bg-surface p-5">
        {error ? (
          <Message tone="err">
            Failed to load metrics: {friendlyErrorMessage(error)}
          </Message>
        ) : isLoading || !data ? (
          <Message>Loading…</Message>
        ) : !data.available ? (
          <Message>
            Metrics unavailable
            {data.reason ? ` — ${data.reason}` : ""}.{" "}
            <Link href="/metrics-stack" className="text-accent-text underline">
              Set up the metrics stack →
            </Link>
          </Message>
        ) : data.noData ? (
          <Message>
            No {type} data for this {resourceType.replace(/s$/, "")} —{" "}
            {data.reason}.
          </Message>
        ) : (
          <>
            <Chart data={data} />
            <div className="mt-3 flex justify-between text-[11px] text-text-faint">
              <span>
                Step {data.stepSeconds}s ·{" "}
                {paused ? "paused" : "live, every 10s"}
              </span>
              <span>Unit: {data.unit}</span>
            </div>
          </>
        )}
      </div>
    </div>
  );
}

function Message({
  children,
  tone,
}: {
  children: React.ReactNode;
  tone?: "err";
}) {
  return (
    <div
      className={`flex h-[320px] items-center justify-center px-6 text-center text-sm ${
        tone === "err" ? "text-err" : "text-text-muted"
      }`}
    >
      {children}
    </div>
  );
}

function Chart({ data }: { data: ResourceMetrics }) {
  const rows = toRows(data);
  const rangeSeconds = data.end - data.start;

  return (
    <div className="h-[320px] w-full">
      <ResponsiveContainer width="100%" height="100%">
        <LineChart
          data={rows}
          margin={{ top: 8, right: 16, bottom: 0, left: 8 }}
        >
          <CartesianGrid stroke="var(--border)" strokeDasharray="3 3" />
          <XAxis
            dataKey="t"
            type="number"
            domain={[data.start, data.end]}
            tickFormatter={(t: number) => formatTick(t, rangeSeconds)}
            tick={{ fill: "var(--text-muted)", fontSize: 11 }}
            stroke="var(--border)"
          />
          <YAxis
            width={72}
            tickFormatter={(v: number) => formatValue(v, data.unit)}
            tick={{ fill: "var(--text-muted)", fontSize: 11 }}
            stroke="var(--border)"
          />
          <Tooltip
            content={({ active, payload, label }) => {
              if (!active || !payload?.length) return null;
              return (
                <div className="rounded-lg border border-border bg-surface px-3 py-2 text-xs shadow-lg">
                  <div className="font-mono text-text-muted">
                    Date: {new Date(Number(label) * 1000).toLocaleString()}
                  </div>
                  {payload.map((p) => (
                    <div key={String(p.dataKey)} className="mt-1 font-mono">
                      <span style={{ color: p.color }}>● </span>
                      {String(p.name)} ({data.unit}):{" "}
                      {formatValue(Number(p.value), data.unit)}
                    </div>
                  ))}
                </div>
              );
            }}
          />
          {data.series.length > 1 && <Legend />}
          {data.series.map((s, i) => (
            <Line
              key={s.name}
              type="stepAfter"
              dataKey={s.name}
              name={s.name}
              stroke={seriesColors[i % seriesColors.length]}
              strokeWidth={1.75}
              dot={rows.length <= 2 ? { r: 3 } : false}
              isAnimationActive={false}
              connectNulls
            />
          ))}
        </LineChart>
      </ResponsiveContainer>
    </div>
  );
}
