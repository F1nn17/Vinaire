"use client";

import { useMemo, useState, type ReactNode } from "react";
import { useTopology } from "@/lib/hooks/useTopology";
import { useNamespaces } from "@/lib/hooks/useNamespaces";
import { useRememberedState } from "@/lib/hooks/useRememberedState";
import { friendlyErrorMessage } from "@/lib/api/errors";
import { t } from "@/lib/i18n/translate";
import type { TopologyNode, TopologyNodeKind } from "@/lib/types";
import { StatusDot } from "@/components/ui/StatusDot";
import {
  ConfigMapIcon,
  LayersIcon,
  NetworkIcon,
  PodIcon,
  SecretIcon,
  ServiceIcon,
} from "@/components/icons";

// Layered layout: each kind gets a row (the workload kinds share one, ConfigMap and Secret share
// one), rows with no nodes are skipped rather than leaving a blank gap. Every namespace is its own
// panel, so a graph of the whole cluster stays readable: relationships never cross namespaces.
const KIND_RANK: Record<TopologyNodeKind, number> = {
  ingress: 0,
  service: 1,
  deployment: 2,
  statefulset: 2,
  daemonset: 2,
  job: 2,
  configmap: 3,
  secret: 3,
  pod: 4,
};

const KIND_LABEL: Record<TopologyNodeKind, string> = {
  ingress: "Ingress",
  service: "Service",
  deployment: "Deployment",
  statefulset: "StatefulSet",
  daemonset: "DaemonSet",
  job: "Job",
  configmap: "ConfigMap",
  secret: "Secret",
  pod: "Pod",
};

const KIND_ICON: Record<TopologyNodeKind, ReactNode> = {
  ingress: <NetworkIcon />,
  service: <ServiceIcon />,
  deployment: <LayersIcon />,
  statefulset: <LayersIcon />,
  daemonset: <LayersIcon />,
  job: <LayersIcon />,
  configmap: <ConfigMapIcon />,
  secret: <SecretIcon />,
  pod: <PodIcon />,
};

const NODE_W = 150;
const NODE_GAP = 22;
const ROW_H = 104;

type Point = { x: number; y: number };

// Positions in pixels inside a panel whose width is the widest row.
function layoutNodes(nodes: TopologyNode[]): {
  points: Map<string, Point>;
  width: number;
  height: number;
} {
  const ranks = [...new Set(nodes.map((n) => KIND_RANK[n.kind]))].sort(
    (a, b) => a - b,
  );
  const rowOf = new Map(ranks.map((rank, i) => [rank, i]));
  const byRow = new Map<number, TopologyNode[]>();
  for (const n of nodes) {
    const row = rowOf.get(KIND_RANK[n.kind])!;
    if (!byRow.has(row)) byRow.set(row, []);
    byRow.get(row)!.push(n);
  }
  const widest = Math.max(1, ...[...byRow.values()].map((r) => r.length));
  const width = Math.max(560, widest * (NODE_W + NODE_GAP) + NODE_GAP);
  const height = Math.max(1, ranks.length) * ROW_H + 20;
  const points = new Map<string, Point>();
  for (const [row, rowNodes] of byRow) {
    rowNodes.forEach((n, i) => {
      points.set(n.id, {
        x: ((i + 0.5) / rowNodes.length) * width,
        y: row * ROW_H + ROW_H / 2 + 10,
      });
    });
  }
  return { points, width, height };
}

const isSystem = (ns: string) => ns.startsWith("kube-");

export default function TopologyPage() {
  const [namespace, setNamespace] = useRememberedState("topology-ns", "all");
  const [showUnused, setShowUnused] = useRememberedState(
    "topology-unused",
    false,
  );
  const namespaces = useNamespaces();
  const { data, isLoading, error } = useTopology(namespace);
  const [selectedId, setSelectedId] = useState<string | null>(null);

  const edges = data?.edges ?? [];
  const nodes = useMemo(() => {
    const all = data?.nodes ?? [];
    if (showUnused) return all;
    // a ConfigMap or Secret nothing refers to is clutter in a graph (every namespace has its
    // kube-root-ca.crt); the toggle brings them back
    const touched = new Set(edges.flatMap((e) => [e.from, e.to]));
    return all.filter(
      (n) =>
        (n.kind !== "configmap" && n.kind !== "secret") || touched.has(n.id),
    );
  }, [data, edges, showUnused]);

  const groups = useMemo(() => {
    const m = new Map<string, TopologyNode[]>();
    for (const n of nodes) {
      if (!m.has(n.namespace)) m.set(n.namespace, []);
      m.get(n.namespace)!.push(n);
    }
    return [...m.entries()].sort(
      ([a], [b]) =>
        Number(isSystem(a)) - Number(isSystem(b)) || a.localeCompare(b),
    );
  }, [nodes]);

  const selected = nodes.find((n) => n.id === selectedId) ?? nodes[0];

  return (
    <div className="flex flex-col gap-[18px] px-8 py-7">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div className="text-[15px] font-semibold">Service graph</div>
        <div className="flex items-center gap-4">
          <label className="flex items-center gap-2 text-xs text-text-muted">
            <input
              type="checkbox"
              checked={showUnused}
              onChange={(e) => setShowUnused(e.target.checked)}
            />
            {t("Show ConfigMaps and Secrets nothing uses")}
          </label>
          <label className="flex items-center gap-2">
            <span className="sr-only">Namespace</span>
            <select
              value={namespace}
              onChange={(e) => {
                setNamespace(e.target.value);
                setSelectedId(null);
              }}
              className="rounded-lg border border-border bg-surface px-3 py-1.75 font-mono text-xs outline-none"
            >
              <option value="all">{t("All namespaces")}</option>
              {(namespaces.data ?? []).map((n) => (
                <option key={n.name} value={n.name}>
                  {n.name}
                </option>
              ))}
            </select>
          </label>
        </div>
      </div>

      {error ? (
        <div className="text-sm text-err">
          Failed to load topology: {friendlyErrorMessage(error)}
        </div>
      ) : isLoading || !data ? (
        <div className="text-sm text-text-muted">Loading…</div>
      ) : nodes.length === 0 ? (
        <div className="text-sm text-text-muted">
          {namespace === "all"
            ? t("No resources found.")
            : t('No resources found in namespace "{ns}".', { ns: namespace })}
        </div>
      ) : (
        <div className="flex items-start gap-6">
          <div className="flex min-w-0 flex-1 flex-col gap-4">
            {groups.map(([ns, groupNodes]) => (
              <NamespacePanel
                key={ns}
                namespace={ns}
                nodes={groupNodes}
                edges={edges}
                selectedId={selected?.id}
                onSelect={setSelectedId}
                // the cluster's own namespaces start folded when there are others to look at
                defaultOpen={groups.length === 1 || !isSystem(ns)}
              />
            ))}
          </div>

          {selected && (
            <div className="sticky top-4 flex w-80 shrink-0 flex-col gap-4 overflow-auto rounded-xl border border-border bg-surface p-5">
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

function NamespacePanel({
  namespace,
  nodes,
  edges,
  selectedId,
  onSelect,
  defaultOpen,
}: {
  namespace: string;
  nodes: TopologyNode[];
  edges: { from: string; to: string }[];
  selectedId: string | undefined;
  onSelect: (id: string) => void;
  defaultOpen: boolean;
}) {
  const { points, width, height } = useMemo(() => layoutNodes(nodes), [nodes]);
  const ids = useMemo(() => new Set(nodes.map((n) => n.id)), [nodes]);
  const own = edges.filter((e) => ids.has(e.from) && ids.has(e.to));
  const problems = nodes.filter((n) => n.statusColor === "err").length;

  return (
    <details
      open={defaultOpen}
      className="rounded-xl border border-border bg-surface"
    >
      <summary className="flex cursor-pointer items-center gap-3 px-4 py-2.5 text-xs">
        <span className="font-mono text-[13px] font-semibold">{namespace}</span>
        <span className="text-text-muted">
          {t("{n} objects", { n: nodes.length })}
        </span>
        {problems > 0 && (
          <span className="flex items-center gap-1 text-err">
            <StatusDot color="err" size={7} />
            {problems}
          </span>
        )}
      </summary>
      <div className="overflow-x-auto border-t border-border">
        <div className="relative" style={{ width, height }}>
          <svg
            aria-hidden="true"
            className="pointer-events-none absolute left-0 top-0"
            width={width}
            height={height}
          >
            {own.map((e, i) => {
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
                />
              );
            })}
          </svg>

          {nodes.map((node) => {
            const pos = points.get(node.id);
            if (!pos) return null;
            const isActive = node.id === selectedId;
            return (
              <button
                key={node.id}
                type="button"
                onClick={() => onSelect(node.id)}
                className="absolute z-[1] -translate-x-1/2 -translate-y-1/2 rounded-[10px] border-[1.5px] p-[9px_11px] text-left"
                style={{
                  width: NODE_W,
                  left: pos.x,
                  top: pos.y,
                  borderColor: isActive ? "var(--accent)" : "var(--border)",
                  background: isActive ? "var(--surface-2)" : "var(--surface)",
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
      </div>
    </details>
  );
}
