"use client";

import {
  formatValue,
  levelFor,
  pctOf,
  type ResourceFigure,
  type Unit,
} from "@/lib/api/requestsLimits";

const fill = {
  ok: "var(--ok)",
  warn: "var(--warn)",
  err: "var(--err)",
};

// One labelled progress bar. `base` is what 100% means; a value above it (limits
// are routinely larger than what the node has) fills the bar and says so in
// words — "150% of allocatable" — instead of silently clipping.
function Bar({
  label,
  value,
  base,
  unit,
  color,
  note,
}: {
  label: string;
  value: number | undefined;
  base: number | undefined;
  unit: Unit;
  color: string;
  note?: string;
}) {
  const known = value !== undefined;
  const pct = base ? pctOf(value, base) : null;
  return (
    <div>
      <div className="mb-1 flex items-baseline justify-between gap-2 text-xs">
        <span className="text-text-muted">{label}</span>
        <span className="font-mono text-[11.5px]">
          {known ? formatValue(value, unit) : "—"}
          {known && pct !== null && (
            <span className="ml-1.5 text-text-faint">{pct}%</span>
          )}
        </span>
      </div>
      <div
        className="h-2 overflow-hidden rounded bg-surface-2"
        role="progressbar"
        aria-label={label}
        aria-valuemin={0}
        aria-valuemax={100}
        aria-valuenow={Math.min(pct ?? 0, 100)}
      >
        <div
          className="h-full rounded"
          style={{
            width: `${Math.min(pct ?? 0, 100)}%`,
            background: color,
          }}
        />
      </div>
      {note && <div className="mt-1 text-[11px] text-warn">{note}</div>}
    </div>
  );
}

// Requests, limits and (if known) live usage of one resource, each against the
// same base — the node's / cluster's allocatable, or, for a namespace, the whole
// cluster's allocatable (so the bar reads "this namespace's share of the cluster").
export function FigureBars({
  figure,
  base,
  unit,
  metricsAvailable,
  resourceLabel,
}: {
  figure: ResourceFigure;
  base: number | undefined;
  unit: Unit;
  metricsAvailable: boolean;
  resourceLabel: string;
}) {
  const reqLevel = levelFor(pctOf(figure.requests, base));
  const lowerBoundReq = figure.containersWithoutRequests
    ? `${figure.containersWithoutRequests} container${figure.containersWithoutRequests === 1 ? " sets" : "s set"} no ${resourceLabel} request — the real reservation may be higher.`
    : undefined;
  const lowerBoundLim = figure.containersWithoutLimits
    ? `${figure.containersWithoutLimits} container${figure.containersWithoutLimits === 1 ? " has" : "s have"} no ${resourceLabel} limit — they can use more than shown.`
    : undefined;
  return (
    <div className="flex flex-col gap-3">
      <Bar
        label="Requests (reserved)"
        value={figure.requests}
        base={base}
        unit={unit}
        color={fill[reqLevel]}
        note={lowerBoundReq}
      />
      <Bar
        label="Limits (ceiling)"
        value={figure.limits}
        base={base}
        unit={unit}
        color="var(--accent)"
        note={lowerBoundLim}
      />
      {metricsAvailable && figure.usage !== undefined ? (
        <Bar
          label="Usage (right now)"
          value={figure.usage}
          base={base}
          unit={unit}
          color="var(--text-muted)"
        />
      ) : (
        <div className="text-[11px] text-text-faint">
          Live usage isn&apos;t available — it needs metrics-server.
        </div>
      )}
    </div>
  );
}
