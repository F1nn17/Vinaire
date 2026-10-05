"use client";

import Link from "next/link";
import { useState } from "react";
import { friendlyErrorMessage } from "@/lib/api/errors";
import {
  formatValue,
  levelFor,
  pctOf,
  useRequestsLimits,
  type ResourceFigure,
  type ResourceOverview,
  type ResourceRow,
  type Unit,
} from "@/lib/api/requestsLimits";
import { Donut } from "@/components/resources/Donut";
import { FigureBars } from "@/components/resources/FigureBars";
import { ListSkeleton } from "@/components/ui/Skeleton";
import { PillTabs } from "@/components/ui/Tabs";

type Scope = "cluster" | "nodes" | "namespaces";

const scopes: { key: Scope; label: string }[] = [
  { key: "cluster", label: "Cluster" },
  { key: "nodes", label: "Nodes" },
  { key: "namespaces", label: "Namespaces" },
];

export default function RequestsLimitsPage() {
  const [scope, setScope] = useState<Scope>("cluster");
  const { data, error, isLoading } = useRequestsLimits();

  return (
    <div className="flex flex-col gap-[18px] px-8 py-7">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <div className="text-[15px] font-semibold">Requests &amp; Limits</div>
          <div className="mt-1 max-w-3xl text-xs text-text-muted">
            <b>Requests</b> are what the scheduler reserves for pods;{" "}
            <b>limits</b> are the ceiling a container may burst to; <b>usage</b>{" "}
            is what it uses right now. A node can be full by requests while
            barely used — and the other way round.
          </div>
        </div>
        <PillTabs items={scopes} active={scope} onChange={setScope} />
      </div>

      {error ? (
        <div className="text-sm text-err">
          Failed to load: {friendlyErrorMessage(error)}
        </div>
      ) : isLoading || !data ? (
        <ListSkeleton />
      ) : scope === "cluster" ? (
        <ClusterView data={data} />
      ) : scope === "nodes" ? (
        <NodesView data={data} />
      ) : (
        <NamespacesView data={data} />
      )}
    </div>
  );
}

// ---------------------------------------------------------------- cluster

function ResourceCard({
  title,
  figure,
  unit,
  metricsAvailable,
  resourceLabel,
}: {
  title: string;
  figure: ResourceFigure;
  unit: Unit;
  metricsAvailable: boolean;
  resourceLabel: string;
}) {
  const base = figure.allocatable;
  const reqPct = pctOf(figure.requests, base);
  return (
    <div className="rounded-[10px] border border-border bg-surface p-5">
      <div className="mb-4 flex items-baseline justify-between">
        <span className="text-[13px] font-semibold">{title}</span>
        <span className="font-mono text-[11px] text-text-faint">
          {base !== undefined
            ? `${formatValue(base, unit)} allocatable`
            : "capacity unknown"}
        </span>
      </div>
      <div className="flex items-center gap-6">
        <Donut
          pct={reqPct}
          level={levelFor(reqPct)}
          label={`${title} requested`}
          sublabel="requested"
        />
        <div className="min-w-0 flex-1">
          <FigureBars
            figure={figure}
            base={base}
            unit={unit}
            metricsAvailable={metricsAvailable}
            resourceLabel={resourceLabel}
          />
        </div>
      </div>
    </div>
  );
}

function ClusterView({ data }: { data: ResourceOverview }) {
  const c = data.cluster;
  return (
    <div className="grid gap-4 xl:grid-cols-2">
      <ResourceCard
        title="CPU"
        figure={c.cpu}
        unit="cpu"
        metricsAvailable={data.metricsAvailable}
        resourceLabel="CPU"
      />
      <ResourceCard
        title="Memory"
        figure={c.memory}
        unit="bytes"
        metricsAvailable={data.metricsAvailable}
        resourceLabel="memory"
      />
      {c.ephemeralStorage.allocatable !== undefined && (
        <ResourceCard
          title="Ephemeral storage"
          figure={c.ephemeralStorage}
          unit="bytes"
          metricsAvailable={false}
          resourceLabel="ephemeral-storage"
        />
      )}
      <div className="rounded-[10px] border border-border bg-surface p-5">
        <div className="mb-4 text-[13px] font-semibold">Pods</div>
        <div className="flex items-center gap-6">
          <Donut
            pct={pctOf(c.pods, c.podsAllocatable)}
            level={levelFor(pctOf(c.pods, c.podsAllocatable))}
            label="Pods"
            sublabel="of capacity"
          />
          <div className="text-xs text-text-muted">
            <div className="font-mono text-[13px] text-text">
              {c.pods} running or pending
            </div>
            <div className="mt-1">
              {c.podsAllocatable !== undefined
                ? `of ${c.podsAllocatable} the nodes allow`
                : ""}
            </div>
            <div className="mt-1">{c.containers} containers in total</div>
          </div>
        </div>
      </div>
    </div>
  );
}

// ------------------------------------------------------------------ nodes

function MiniBars({
  label,
  figure,
  base,
  unit,
  metricsAvailable,
}: {
  label: string;
  figure: ResourceFigure;
  base: number | undefined;
  unit: Unit;
  metricsAvailable: boolean;
}) {
  const row = (
    name: string,
    value: number | undefined,
    color: string,
  ): React.ReactNode => {
    if (value === undefined) return null;
    const pct = pctOf(value, base);
    return (
      <div className="grid grid-cols-[4.5rem_1fr_7.5rem] items-center gap-2 text-[11px]">
        <span className="text-text-faint">{name}</span>
        <div
          className="h-1.5 overflow-hidden rounded bg-surface-2"
          role="progressbar"
          aria-label={`${label} ${name}`}
          aria-valuemin={0}
          aria-valuemax={100}
          aria-valuenow={Math.min(pct, 100)}
        >
          <div
            className="h-full"
            style={{ width: `${Math.min(pct, 100)}%`, background: color }}
          />
        </div>
        <span className="text-right font-mono text-text-muted">
          {formatValue(value, unit)} · {pct}%
        </span>
      </div>
    );
  };
  const reqColor = {
    ok: "var(--ok)",
    warn: "var(--warn)",
    err: "var(--err)",
  }[levelFor(pctOf(figure.requests, base))];
  return (
    <div className="flex flex-col gap-1.5">
      <div className="text-xs font-semibold">{label}</div>
      {row("requests", figure.requests, reqColor)}
      {row("limits", figure.limits, "var(--accent)")}
      {metricsAvailable && row("usage", figure.usage, "var(--text-muted)")}
    </div>
  );
}

function NodesView({ data }: { data: ResourceOverview }) {
  if (data.nodes.length === 0) {
    return <div className="text-sm text-text-faint">No nodes.</div>;
  }
  return (
    <div className="grid gap-4 lg:grid-cols-2 2xl:grid-cols-3">
      {data.nodes.map((n) => (
        <div
          key={n.name}
          className="flex flex-col gap-4 rounded-[10px] border border-border bg-surface p-5"
        >
          <div className="flex items-center justify-between gap-2">
            <Link
              href={`/nodes/detail?name=${encodeURIComponent(n.name)}`}
              className="truncate font-mono text-[13px] font-semibold hover:text-accent-text"
            >
              {n.name}
            </Link>
            <span className="shrink-0 font-mono text-[11px] text-text-faint">
              {n.pods}
              {n.podsAllocatable !== undefined
                ? `/${n.podsAllocatable}`
                : ""}{" "}
              pods
            </span>
          </div>
          <MiniBars
            label="CPU"
            figure={n.cpu}
            base={n.cpu.allocatable}
            unit="cpu"
            metricsAvailable={data.metricsAvailable}
          />
          <MiniBars
            label="Memory"
            figure={n.memory}
            base={n.memory.allocatable}
            unit="bytes"
            metricsAvailable={data.metricsAvailable}
          />
        </div>
      ))}
    </div>
  );
}

// ------------------------------------------------------------- namespaces

function NamespacesView({ data }: { data: ResourceOverview }) {
  const [filter, setFilter] = useState("");
  const base = data.cluster;
  const rows = data.namespaces
    .filter((n) => n.name.toLowerCase().includes(filter.trim().toLowerCase()))
    .sort(
      (a: ResourceRow, b: ResourceRow) => b.memory.requests - a.memory.requests,
    );

  return (
    <div className="flex flex-col gap-3">
      <div className="flex items-center gap-3">
        <input
          type="text"
          aria-label="Filter namespaces"
          value={filter}
          onChange={(e) => setFilter(e.target.value)}
          placeholder="Filter namespaces"
          className="rounded-lg border border-border bg-surface px-3 py-1.75 font-mono text-xs outline-none placeholder:text-text-faint"
        />
        <span className="text-xs text-text-faint">
          Bars show each namespace&apos;s share of the whole cluster&apos;s
          allocatable CPU and memory. Sorted by memory requested.
        </span>
      </div>
      {rows.length === 0 ? (
        <div className="text-sm text-text-faint">No namespaces match.</div>
      ) : (
        <div className="grid gap-4 lg:grid-cols-2 2xl:grid-cols-3">
          {rows.map((n) => (
            <div
              key={n.name}
              className="flex flex-col gap-4 rounded-[10px] border border-border bg-surface p-5"
            >
              <div className="flex items-center justify-between gap-2">
                <Link
                  href={`/namespaces/detail?name=${encodeURIComponent(n.name)}`}
                  className="truncate font-mono text-[13px] font-semibold hover:text-accent-text"
                >
                  {n.name}
                </Link>
                <span className="shrink-0 font-mono text-[11px] text-text-faint">
                  {n.pods} pods · {n.containers} containers
                </span>
              </div>
              <MiniBars
                label="CPU"
                figure={n.cpu}
                base={base.cpu.allocatable}
                unit="cpu"
                metricsAvailable={data.metricsAvailable}
              />
              <MiniBars
                label="Memory"
                figure={n.memory}
                base={base.memory.allocatable}
                unit="bytes"
                metricsAvailable={data.metricsAvailable}
              />
            </div>
          ))}
        </div>
      )}
    </div>
  );
}
