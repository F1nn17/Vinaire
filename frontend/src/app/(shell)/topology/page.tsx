"use client";

import { useState } from "react";
import { useTopology } from "@/lib/hooks/useTopology";
import type { TopologyNode, TopologyNodeId } from "@/lib/types";
import { StatusDot } from "@/components/ui/StatusDot";
import {
  ChevronDownIcon,
  ConfigMapIcon,
  GrapeLogo,
  NetworkIcon,
  LayersIcon,
  PodIcon,
  SecretIcon,
  ServiceIcon,
} from "@/components/icons";

const REF_W = 800;
const REF_H = 560;

const layout: Record<
  TopologyNodeId,
  { x: number; y: number; icon: React.ReactNode }
> = {
  ingress: { x: 325, y: 40, icon: <NetworkIcon /> },
  svc: { x: 325, y: 130, icon: <ServiceIcon /> },
  deploy: { x: 325, y: 220, icon: <LayersIcon /> },
  secret: { x: 70, y: 220, icon: <SecretIcon /> },
  cm: { x: 580, y: 220, icon: <ConfigMapIcon /> },
  pod1: { x: 200, y: 340, icon: <PodIcon /> },
  pod2: { x: 450, y: 340, icon: <PodIcon /> },
};
const NODE_W = 150;
const NODE_H = 60;

function pct(value: number, ref: number) {
  return `${(value / ref) * 100}%`;
}

function center(id: TopologyNodeId) {
  const n = layout[id];
  return { x: n.x + NODE_W / 2, y: n.y + NODE_H / 2 };
}

export default function TopologyPage() {
  const { data, isLoading } = useTopology();
  const [selected, setSelected] = useState<TopologyNodeId>("pod1");

  if (isLoading || !data) {
    return <div className="p-8 text-sm text-text-muted">Loading…</div>;
  }

  const nodes = data.nodes;
  const selectedNode: TopologyNode = nodes[selected];
  // deploy fans out to two pods below it
  const deployCenter = center("deploy");
  const pod1Center = center("pod1");
  const pod2Center = center("pod2");

  return (
    <div className="flex flex-col gap-[18px] px-8 py-7">
      <div className="flex items-center justify-between">
        <div className="text-[15px] font-semibold">Service graph</div>
        <button
          type="button"
          className="flex items-center gap-2 rounded-lg border border-border bg-surface px-3 py-[7px]"
        >
          <span className="font-mono text-xs">namespace: {data.namespace}</span>
          <ChevronDownIcon />
        </button>
      </div>

      <div className="flex min-h-[520px] gap-6">
        <div className="relative min-h-[480px] flex-1 overflow-hidden rounded-xl border border-border bg-surface">
          <div className="pointer-events-none absolute -bottom-6 -right-8 opacity-[0.07] text-accent">
            <GrapeLogo size={260} />
          </div>

          <svg
            aria-hidden="true"
            className="pointer-events-none absolute left-0 top-0 h-full w-full"
            viewBox={`0 0 ${REF_W} ${REF_H}`}
            preserveAspectRatio="none"
          >
            <line
              x1={center("ingress").x}
              y1={layout.ingress.y + NODE_H}
              x2={center("ingress").x}
              y2={layout.svc.y}
              stroke="var(--border)"
              strokeWidth={1.5}
            />
            <line
              x1={center("svc").x}
              y1={layout.svc.y + NODE_H}
              x2={center("svc").x}
              y2={layout.deploy.y}
              stroke="var(--border)"
              strokeWidth={1.5}
            />
            <line
              x1={layout.deploy.x}
              y1={deployCenter.y}
              x2={layout.secret.x + NODE_W}
              y2={deployCenter.y}
              stroke="var(--border)"
              strokeWidth={1.5}
            />
            <line
              x1={layout.deploy.x + NODE_W}
              y1={deployCenter.y}
              x2={layout.cm.x}
              y2={deployCenter.y}
              stroke="var(--border)"
              strokeWidth={1.5}
            />
            <line
              x1={deployCenter.x}
              y1={layout.deploy.y + NODE_H}
              x2={pod1Center.x}
              y2={layout.pod1.y}
              stroke="var(--border)"
              strokeWidth={1.5}
            />
            <line
              x1={deployCenter.x}
              y1={layout.deploy.y + NODE_H}
              x2={pod2Center.x}
              y2={layout.pod2.y}
              stroke="var(--border)"
              strokeWidth={1.5}
            />
          </svg>

          {(Object.keys(layout) as TopologyNodeId[]).map((id) => {
            const node = nodes[id];
            const pos = layout[id];
            const isActive = id === selected;
            return (
              <button
                key={id}
                type="button"
                onClick={() => setSelected(id)}
                className="absolute z-[1] flex items-start gap-2 rounded-[10px] border-[1.5px] p-[9px_11px] text-left"
                style={{
                  left: pct(pos.x, REF_W),
                  top: pct(pos.y, REF_H),
                  width: pct(NODE_W, REF_W),
                  borderColor: isActive ? "var(--accent)" : "var(--border)",
                  background: isActive ? "var(--surface-2)" : "var(--surface)",
                }}
              >
                <span className="mt-px shrink-0 text-text-muted">
                  {pos.icon}
                </span>
                <span className="min-w-0 flex-1">
                  <span className="block text-xs font-semibold">
                    {node.kind}
                  </span>
                  <span className="mono block truncate font-mono text-[10.5px] text-text-muted">
                    {node.name}
                  </span>
                </span>
                {node.warn && (
                  <span className="mt-0.5 shrink-0">
                    <StatusDot color="warn" size={7} />
                  </span>
                )}
              </button>
            );
          })}
        </div>

        <div className="flex w-80 shrink-0 flex-col gap-4 overflow-auto rounded-xl border border-border bg-surface p-5">
          <div>
            <div className="text-[11px] uppercase tracking-wide text-text-muted">
              {selectedNode.kind}
            </div>
            <div className="mt-1 break-all font-mono text-[15px] font-semibold">
              {selectedNode.name}
            </div>
            <div className="mt-2.5 flex items-center gap-1.5">
              <StatusDot color={selectedNode.statusColor} size={7} />
              <span className="text-xs text-text-muted">
                {selectedNode.statusText}
              </span>
              <span className="ml-auto font-mono text-[11px] text-text-faint">
                ns/{selectedNode.namespace}
              </span>
            </div>
          </div>
          <div className="h-px bg-border" />
          <div className="flex flex-col gap-2.5">
            {selectedNode.extra.map(([k, v]) => (
              <div key={k} className="flex items-start justify-between gap-2.5">
                <span className="shrink-0 text-xs text-text-muted">{k}</span>
                <span className="text-right font-mono text-xs">{v}</span>
              </div>
            ))}
          </div>
        </div>
      </div>
    </div>
  );
}
