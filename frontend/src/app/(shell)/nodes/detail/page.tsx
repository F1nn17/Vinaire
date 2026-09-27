"use client";

import Link from "next/link";
import { useSearchParams } from "next/navigation";
import { Suspense, useState } from "react";
import { useNode } from "@/lib/hooks/useWorkloads";
import { friendlyErrorMessage } from "@/lib/api/errors";
import type { NodeScheduledPodRow } from "@/lib/types";
import { StatusDot } from "@/components/ui/StatusDot";
import { UnderlineTabs } from "@/components/ui/Tabs";
import { CodeBlock } from "@/components/ui/CodeBlock";
import { DataTable, type DataTableColumn } from "@/components/ui/DataTable";

type Tab = "overview" | "pods" | "yaml";

const tabs: { key: Tab; label: string }[] = [
  { key: "overview", label: "Overview" },
  { key: "pods", label: "Pods" },
  { key: "yaml", label: "YAML" },
];

const podColumns: DataTableColumn<NodeScheduledPodRow>[] = [
  {
    header: "Name",
    width: "2fr",
    render: (r) => <span className="font-mono text-[12.5px]">{r.name}</span>,
  },
  {
    header: "Namespace",
    width: "1.2fr",
    render: (r) => (
      <span className="font-mono text-xs text-text-muted">{r.namespace}</span>
    ),
  },
  {
    header: "Ready",
    width: ".8fr",
    render: (r) => <span className="font-mono text-xs">{r.ready}</span>,
  },
  {
    header: "Status",
    width: "1.1fr",
    render: (r) => (
      <span className="flex items-center gap-1.5 text-xs">
        <StatusDot color={r.statusColor} />
        {r.status}
      </span>
    ),
  },
  {
    header: "Age",
    width: ".7fr",
    render: (r) => (
      <span className="font-mono text-xs text-text-muted">{r.age}</span>
    ),
  },
];

// A static export can't pre-render a page per node name (unknowable at
// build time) — this route is a single static page, `/nodes/detail`, that
// reads which node to show from `?name=` instead of a `[name]` route
// segment. See AGENTS.md's "Rendering model".
export default function NodeDetailPageRoute() {
  return (
    <Suspense
      fallback={<div className="p-8 text-sm text-text-muted">Loading…</div>}
    >
      <NodeDetailPage />
    </Suspense>
  );
}

function NodeDetailPage() {
  const searchParams = useSearchParams();
  const name = searchParams.get("name") ?? "";
  const { data: node, isLoading, error } = useNode(name);
  const [tab, setTab] = useState<Tab>("overview");

  if (error) {
    return (
      <div className="p-8 text-sm text-err">
        Failed to load node: {friendlyErrorMessage(error)}
      </div>
    );
  }

  if (isLoading) {
    return <div className="p-8 text-sm text-text-muted">Loading…</div>;
  }

  if (!node) {
    return (
      <div className="p-8 text-sm text-text-muted">
        Node &ldquo;{name}&rdquo; not found.
      </div>
    );
  }

  return (
    <div className="flex flex-col gap-[18px] px-8 py-7">
      <div>
        <div className="font-mono text-xs text-text-faint">
          <Link href="/nodes">Nodes</Link>
        </div>
        <div className="mt-1.5 flex items-center gap-2.5">
          <span className="font-mono text-lg font-semibold">{node.name}</span>
          <span className="flex items-center gap-1.5 rounded-full border border-border bg-surface px-2.5 py-[3px] text-xs text-text-muted">
            <StatusDot color={node.statusColor} />
            {node.status}
          </span>
        </div>
      </div>

      <UnderlineTabs items={tabs} active={tab} onChange={setTab} />

      {tab === "overview" && (
        <div className="flex max-w-3xl flex-col gap-4">
          <div className="rounded-[10px] border border-border bg-surface p-5">
            <div className="mb-3.5 text-[13px] font-semibold">Metadata</div>
            <div className="flex flex-col gap-2.5">
              <Row label="Roles" value={node.roles} />
              <Row label="Kubelet version" value={node.kubeletVersion} />
              <Row label="OS" value={node.os} />
              <Row label="Age" value={node.age} />
            </div>
          </div>

          <div className="rounded-[10px] border border-border bg-surface p-5">
            <div className="mb-3.5 text-[13px] font-semibold">
              Capacity / Allocatable
            </div>
            <div className="grid grid-cols-3 gap-4 text-xs">
              <div />
              <div className="text-text-muted">Capacity</div>
              <div className="text-text-muted">Allocatable</div>
              <div className="text-text-muted">CPU</div>
              <div className="font-mono">{node.capacity.cpu}</div>
              <div className="font-mono">{node.allocatable.cpu}</div>
              <div className="text-text-muted">Memory</div>
              <div className="font-mono">{node.capacity.memory}</div>
              <div className="font-mono">{node.allocatable.memory}</div>
              <div className="text-text-muted">Pods</div>
              <div className="font-mono">{node.capacity.pods}</div>
              <div className="font-mono">{node.allocatable.pods}</div>
            </div>
            {node.cpu && node.memory && (
              <div className="mt-4 flex flex-col gap-3 border-t border-border pt-4">
                <UsageRow
                  label="CPU used"
                  usage={node.cpu}
                  color="var(--accent)"
                />
                <UsageRow
                  label="Memory used"
                  usage={node.memory}
                  color="var(--warn)"
                />
              </div>
            )}
          </div>

          <div className="rounded-[10px] border border-border bg-surface p-5">
            <div className="mb-3.5 text-[13px] font-semibold">Conditions</div>
            <div className="flex flex-col gap-2">
              {node.conditions.map((c) => (
                <div
                  key={c.type}
                  className="flex items-start justify-between gap-4 text-xs"
                >
                  <span className="font-mono">{c.type}</span>
                  <span className="text-right text-text-muted">
                    {c.status}
                    {c.reason && ` · ${c.reason}`}
                  </span>
                </div>
              ))}
            </div>
          </div>

          <div className="rounded-[10px] border border-border bg-surface p-5">
            <div className="mb-3.5 text-[13px] font-semibold">Taints</div>
            {node.taints.length === 0 ? (
              <div className="text-xs text-text-faint">No taints.</div>
            ) : (
              <div className="flex flex-wrap gap-1.5">
                {node.taints.map((t, i) => (
                  <span
                    key={i}
                    className="rounded-md border border-border bg-surface-2 px-2 py-1 font-mono text-[11px]"
                  >
                    {t.key}
                    {t.value && `=${t.value}`}:{t.effect}
                  </span>
                ))}
              </div>
            )}
          </div>

          <div className="rounded-[10px] border border-border bg-surface p-5">
            <div className="mb-2 text-xs text-text-muted">Labels</div>
            <div className="flex flex-wrap gap-1.5">
              {Object.entries(node.labels).map(([k, v]) => (
                <span
                  key={k}
                  className="rounded-md border border-border bg-surface-2 px-2 py-1 font-mono text-[11px]"
                >
                  {k}={v}
                </span>
              ))}
            </div>
          </div>
        </div>
      )}

      {tab === "pods" && (
        <DataTable
          columns={podColumns}
          rows={node.pods}
          rowKey={(r) => `${r.namespace}/${r.name}`}
          rowHref={(r) =>
            `/workloads/pods/detail?namespace=${encodeURIComponent(r.namespace)}&name=${encodeURIComponent(r.name)}`
          }
          emptyLabel="No pods scheduled on this node."
        />
      )}

      {tab === "yaml" && <CodeBlock code={node.yaml} />}
    </div>
  );
}

function Row({ label, value }: { label: string; value: string }) {
  return (
    <div className="flex justify-between">
      <span className="text-xs text-text-muted">{label}</span>
      <span className="font-mono text-xs">{value}</span>
    </div>
  );
}

function UsageRow({
  label,
  usage,
  color,
}: {
  label: string;
  usage: { used: string; limit: string; pct: number };
  color: string;
}) {
  return (
    <div>
      <div className="mb-1.5 flex items-center justify-between">
        <span className="text-xs text-text-muted">{label}</span>
        <span className="font-mono text-xs text-text-muted">
          {usage.used} / {usage.limit}
        </span>
      </div>
      <div className="h-2 overflow-hidden rounded bg-surface-2">
        <div
          className="h-full"
          style={{ width: `${usage.pct}%`, background: color }}
        />
      </div>
    </div>
  );
}
