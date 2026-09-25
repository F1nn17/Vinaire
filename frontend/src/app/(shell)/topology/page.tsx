"use client";

import { useState, type ReactNode } from "react";
import { useTopology } from "@/lib/hooks/useTopology";
import type { TopologyNode, TopologyNodeKind } from "@/lib/types";
import { StatusDot } from "@/components/ui/StatusDot";
import {
  ConfigMapIcon,
  GrapeLogo,
  LayersIcon,
  NetworkIcon,
  PodIcon,
  SecretIcon,
  ServiceIcon,
} from "@/components/icons";

// Layered-column layout: each kind gets a row (ConfigMap and Secret share
// one), rows with no nodes in the current graph are skipped rather than
// leaving a blank gap. Nodes within a row spread out evenly. This is the
// "manual layout" the roadmap suggested trying before reaching for a graph
// layout library — reconsider if graphs turn out more tangled than this
// handles well.
const KIND_RANK: Record<TopologyNodeKind, number> = {
  ingress: 0,
  service: 1,
  deployment: 2,
  configmap: 3,
  secret: 3,
  pod: 4,
};

const KIND_LABEL: Record<TopologyNodeKind, string> = {
  ingress: "Ingress",
  service: "Service",
  deployment: "Deployment",
  configmap: "ConfigMap",
  secret: "Secret",
  pod: "Pod",
};

const KIND_ICON: Record<TopologyNodeKind, ReactNode> = {
  ingress: <NetworkIcon />,
  service: <ServiceIcon />,
  deployment: <LayersIcon />,
  configmap: <ConfigMapIcon />,
  secret: <SecretIcon />,
  pod: <PodIcon />,
};

type Point = { x: number; y: number };

function layoutNodes(nodes: TopologyNode[]): Map<string, Point> {
  const ranksPresent = [...new Set(nodes.map((n) => KIND_RANK[n.kind]))].sort(
    (a, b) => a - b,
  );
  const rowOf = new Map(ranksPresent.map((rank, i) => [rank, i]));
  const rowCount = ranksPresent.length || 1;

  const byRow = new Map<number, TopologyNode[]>();
  for (const n of nodes) {
    const row = rowOf.get(KIND_RANK[n.kind])!;
    if (!byRow.has(row)) byRow.set(row, []);
    byRow.get(row)!.push(n);
  }

  const points = new Map<string, Point>();
  for (const [row, rowNodes] of byRow) {
    rowNodes.forEach((n, i) => {
      points.set(n.id, {
        x: ((i + 0.5) / rowNodes.length) * 100,
        y: ((row + 0.5) / rowCount) * 100,
      });
    });
  }
  return points;
}

export default function TopologyPage() {
  const [namespace, setNamespace] = useState("default");
  const { data, isLoading, error } = useTopology(namespace);
  const [selectedId, setSelectedId] = useState<string | null>(null);

  const nodes = data?.nodes ?? [];
  const edges = data?.edges ?? [];
  const points = layoutNodes(nodes);
  const selected = nodes.find((n) => n.id === selectedId) ?? nodes[0];

  return (
    <div className="flex flex-col gap-[18px] px-8 py-7">
      <div className="flex items-center justify-between">
        <div className="text-[15px] font-semibold">Service graph</div>
        <label className="flex items-center gap-2">
          <span className="sr-only">Namespace</span>
          <input
            type="text"
            value={namespace}
            onChange={(e) => setNamespace(e.target.value)}
            placeholder="namespace"
            className="rounded-lg border border-border bg-surface px-3 py-1.75 font-mono text-xs outline-none placeholder:text-text-faint"
          />
        </label>
      </div>

      {error ? (
        <div className="text-sm text-err">
          Failed to load topology: {error.message}
        </div>
      ) : isLoading || !data ? (
        <div className="text-sm text-text-muted">Loading…</div>
      ) : nodes.length === 0 ? (
        <div className="text-sm text-text-muted">
          No resources found in namespace &ldquo;{namespace}&rdquo;.
        </div>
      ) : (
        <div className="flex min-h-[520px] gap-6">
          <div className="relative min-h-[480px] flex-1 overflow-hidden rounded-xl border border-border bg-surface">
            <div className="pointer-events-none absolute -bottom-6 -right-8 text-accent opacity-[0.07]">
              <GrapeLogo size={260} />
            </div>

            <svg
              aria-hidden="true"
              className="pointer-events-none absolute left-0 top-0 h-full w-full"
              viewBox="0 0 100 100"
              preserveAspectRatio="none"
            >
              {edges.map((e, i) => {
                const from = points.get(e.from);
                const to = points.get(e.to);
                if (!from || !to) return null;
                return (
                  <line
                    key={i}
                    x1={from.x}
                    y1={from.y}
                    x2={to.x}
                    y2={to.y}
                    stroke="var(--border)"
                    strokeWidth={1}
                    vectorEffect="non-scaling-stroke"
                  />
                );
              })}
            </svg>

            {nodes.map((node) => {
              const pos = points.get(node.id);
              if (!pos) return null;
              const isActive = node.id === selected?.id;
              return (
                <button
                  key={node.id}
                  type="button"
                  onClick={() => setSelectedId(node.id)}
                  className="absolute z-[1] w-[150px] -translate-x-1/2 -translate-y-1/2 rounded-[10px] border-[1.5px] p-[9px_11px] text-left"
                  style={{
                    left: `${pos.x}%`,
                    top: `${pos.y}%`,
                    borderColor: isActive ? "var(--accent)" : "var(--border)",
                    background: isActive
                      ? "var(--surface-2)"
                      : "var(--surface)",
                  }}
                >
                  <span className="flex items-start gap-2">
                    <span className="mt-px shrink-0 text-text-muted">
                      {KIND_ICON[node.kind]}
                    </span>
                    <span className="min-w-0 flex-1">
                      <span className="block text-xs font-semibold">
                        {KIND_LABEL[node.kind]}
                      </span>
                      <span className="block truncate font-mono text-[10.5px] text-text-muted">
                        {node.name}
                      </span>
                    </span>
                    {node.statusColor !== "ok" && (
                      <span className="mt-0.5 shrink-0">
                        <StatusDot color={node.statusColor} size={7} />
                      </span>
                    )}
                  </span>
                </button>
              );
            })}
          </div>

          {selected && (
            <div className="flex w-80 shrink-0 flex-col gap-4 overflow-auto rounded-xl border border-border bg-surface p-5">
              <div>
                <div className="text-[11px] uppercase tracking-wide text-text-muted">
                  {KIND_LABEL[selected.kind]}
                </div>
                <div className="mt-1 break-all font-mono text-[15px] font-semibold">
                  {selected.name}
                </div>
                <div className="mt-2.5 flex items-center gap-1.5">
                  <StatusDot color={selected.statusColor} size={7} />
                  <span className="text-xs text-text-muted">
                    {selected.statusText}
                  </span>
                  <span className="ml-auto font-mono text-[11px] text-text-faint">
                    ns/{selected.namespace}
                  </span>
                </div>
              </div>
              <div className="h-px bg-border" />
              <div className="flex flex-col gap-2.5">
                {selected.extra.map(([k, v]) => (
                  <div
                    key={k}
                    className="flex items-start justify-between gap-2.5"
                  >
                    <span className="shrink-0 text-xs text-text-muted">
                      {k}
                    </span>
                    <span className="text-right font-mono text-xs">{v}</span>
                  </div>
                ))}
              </div>
            </div>
          )}
        </div>
      )}
    </div>
  );
}
