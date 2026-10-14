"use client";

import {
  CartesianGrid,
  Line,
  LineChart,
  ResponsiveContainer,
  Tooltip,
  XAxis,
  YAxis,
} from "recharts";
import { usePanelData } from "@/lib/hooks/useDashboards";
import { friendlyErrorMessage } from "@/lib/api/errors";
import { formatPanelValue } from "@/lib/dashboards/format";
import type { Panel } from "@/lib/dashboards/model";
import type { Series } from "@/lib/dashboards/prom";

const colors = [
  "var(--accent)",
  "var(--ok)",
  "var(--warn)",
  "var(--err)",
  "var(--accent-text)",
  "var(--text-muted)",
];

// recharts wants one row per timestamp
function toRows(series: Series[]): Record<string, number>[] {
  const byTs = new Map<number, Record<string, number>>();
  series.forEach((s, i) => {
    for (const p of s.points) {
      const row = byTs.get(p.t) ?? { t: p.t };
      row[`s${i}`] = p.v;
      byTs.set(p.t, row);
    }
  });
  return [...byTs.values()].sort((a, b) => a.t - b.t);
}

function tick(ts: number, rangeSec: number): string {
  const d = new Date(ts);
  const time = d.toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" });
  return rangeSec > 24 * 3600
    ? `${d.toLocaleDateString([], { month: "short", day: "numeric" })} ${time}`
    : time;
}

export function PanelView({
  dashboardId,
  index,
  panel,
  declared,
  chosen,
  rangeSec,
  paused,
}: {
  dashboardId: string;
  index: number;
  panel: Panel;
  declared: string[];
  chosen: Record<string, string>;
  rangeSec: number;
  paused: boolean;
}) {
  const { data, error, isLoading } = usePanelData(
    dashboardId,
    index,
    panel,
    declared,
    chosen,
    rangeSec,
    paused,
  );
  const series = (data?.series ?? []).filter((s) => s.points.length > 0);
  const stat = panel.kind === "stat";

  return (
    <div
      className={`flex flex-col rounded-[10px] border border-border bg-surface p-4 ${
        stat ? "" : "min-h-[260px] md:col-span-2"
      }`}
      data-testid="dash-panel"
    >
      <div className="flex items-baseline justify-between gap-2">
        <h3 className="text-[13px] font-medium">{panel.title}</h3>
        {panel.help && (
          <span className="text-[11px] text-text-faint">{panel.help}</span>
        )}
      </div>
      {error ? (
        <Note tone="err">{friendlyErrorMessage(error)}</Note>
      ) : isLoading && !data ? (
        <Note>Loading…</Note>
      ) : data?.error ? (
        <Note tone="err">{data.error}</Note>
      ) : series.length === 0 ? (
        <Note>No data</Note>
      ) : stat ? (
        <div className="mt-3 font-mono text-3xl">
          {formatPanelValue(series[0].points.at(-1)?.v ?? NaN, panel.unit)}
        </div>
      ) : (
        <Chart series={series} unit={panel.unit} rangeSec={rangeSec} />
      )}
    </div>
  );
}

function Note({
  children,
  tone,
}: {
  children: React.ReactNode;
  tone?: "err";
}) {
  return (
    <div
      className={`flex flex-1 items-center justify-center px-3 py-6 text-center text-xs ${
        tone === "err" ? "text-err" : "text-text-faint"
      }`}
    >
      {children}
    </div>
  );
}

function Chart({
  series,
  unit,
  rangeSec,
}: {
  series: Series[];
  unit: Panel["unit"];
  rangeSec: number;
}) {
  const rows = toRows(series);
  return (
    <div className="mt-2 h-[210px] w-full">
      <ResponsiveContainer width="100%" height="100%">
        <LineChart data={rows} margin={{ top: 6, right: 12, bottom: 0, left: 4 }}>
          <CartesianGrid stroke="var(--border)" strokeDasharray="3 3" />
          <XAxis
            dataKey="t"
            type="number"
            domain={["dataMin", "dataMax"]}
            tickFormatter={(t: number) => tick(t, rangeSec)}
            tick={{ fill: "var(--text-muted)", fontSize: 10 }}
            stroke="var(--border)"
          />
          <YAxis
            width={64}
            tickFormatter={(v: number) => formatPanelValue(v, unit)}
            tick={{ fill: "var(--text-muted)", fontSize: 10 }}
            stroke="var(--border)"
          />
          <Tooltip
            content={({ active, payload, label }) => {
              if (!active || !payload?.length) return null;
              return (
                <div className="rounded-lg border border-border bg-surface px-3 py-2 text-xs shadow-lg">
                  <div className="font-mono text-text-muted">
                    {new Date(Number(label)).toLocaleString()}
                  </div>
                  {payload.map((p) => (
                    <div key={String(p.dataKey)} className="mt-1 font-mono">
                      <span style={{ color: p.color }}>● </span>
                      {String(p.name)}: {formatPanelValue(Number(p.value), unit)}
                    </div>
                  ))}
                </div>
              );
            }}
          />
          {series.map((s, i) => (
            <Line
              key={i}
              type="monotone"
              dataKey={`s${i}`}
              name={s.name}
              stroke={colors[i % colors.length]}
              strokeWidth={1.6}
              dot={rows.length <= 2 ? { r: 3 } : false}
              isAnimationActive={false}
              connectNulls
            />
          ))}
        </LineChart>
      </ResponsiveContainer>
      <div className="mt-1 flex flex-wrap gap-x-3 gap-y-0.5 text-[10.5px] text-text-muted">
        {series.slice(0, 10).map((s, i) => (
          <span key={i} className="font-mono">
            <span style={{ color: colors[i % colors.length] }}>● </span>
            {s.name}
          </span>
        ))}
        {series.length > 10 && <span>+{series.length - 10}</span>}
      </div>
    </div>
  );
}
