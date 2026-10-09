"use client";

import { useRememberedState } from "@/lib/hooks/useRememberedState";
import Link from "next/link";
import { useState } from "react";
import { useNodes, useCapacity } from "@/lib/hooks/useWorkloads";
import { useNodesStream } from "@/lib/hooks/useNodesStream";
import { friendlyErrorMessage } from "@/lib/api/errors";
import type { CapacityUsage, NodeCapacityRow, NodeRow } from "@/lib/types";
import { DataTable, type DataTableColumn } from "@/components/ui/DataTable";
import { StatusDot } from "@/components/ui/StatusDot";
import { PillTabs } from "@/components/ui/Tabs";
import { AddNodeModal } from "@/components/nodes/AddNodeModal";
import { useClusters } from "@/lib/hooks/useClusters";
import { useCurrentCluster } from "@/lib/clusterStore";
import { PlusIcon } from "@/components/icons";
import type { ClusterType } from "@/lib/types";

type Tab = "nodes" | "capacity";

const tabs: { key: Tab; label: string }[] = [
  { key: "nodes", label: "Nodes" },
  { key: "capacity", label: "Capacity" },
];

const colorVar: Record<string, string> = {
  ok: "var(--ok)",
  warn: "var(--warn)",
  err: "var(--err)",
};

function UsageBar({
  usage,
  color,
}: {
  usage: { used: string; limit: string; pct: number } | null;
  color: string;
}) {
  if (!usage) {
    return <span className="text-xs text-text-faint">—</span>;
  }
  return (
    <div className="flex flex-col gap-1">
      <span className="font-mono text-[11px] text-text-muted">
        {usage.used} / {usage.limit}
      </span>
      <div className="h-1.5 w-24 overflow-hidden rounded bg-surface-2">
        <div
          className="h-full"
          style={{ width: `${usage.pct}%`, background: color }}
        />
      </div>
    </div>
  );
}

// Same visual language as UsageBar, but for a CapacityUsage — the bar's
// color comes from the row itself (ok/warn/err, set server-side from how
// close to the ceiling it is), not a fixed per-column color, since the
// whole point of this tab is flagging a specific node red/yellow.
function CapacityBar({ usage }: { usage: CapacityUsage }) {
  return (
    <div className="flex flex-col gap-1">
      <span className="font-mono text-[11px] text-text-muted">
        {usage.used} / {usage.total}
      </span>
      <div className="h-1.5 w-24 overflow-hidden rounded bg-surface-2">
        <div
          className="h-full"
          style={{
            width: `${usage.pct}%`,
            background: colorVar[usage.color] ?? colorVar.ok,
          }}
        />
      </div>
    </div>
  );
}

const columns: DataTableColumn<NodeRow>[] = [
  {
    header: "Name",
    width: "2fr",
    render: (r) => (
      <span className="truncate pr-3 font-mono text-[12.5px]">{r.name}</span>
    ),
  },
  {
    header: "Status",
    width: "1.3fr",
    render: (r) => (
      <span className="flex items-center gap-1.5 text-xs">
        <StatusDot color={r.statusColor} />
        {r.status}
      </span>
    ),
  },
  {
    header: "Roles",
    width: "1fr",
    render: (r) => (
      <span className="font-mono text-xs text-text-muted">{r.roles}</span>
    ),
  },
  {
    header: "CPU",
    width: "1.2fr",
    render: (r) => <UsageBar usage={r.cpu} color="var(--accent)" />,
  },
  {
    header: "Memory",
    width: "1.2fr",
    render: (r) => <UsageBar usage={r.memory} color="var(--warn)" />,
  },
  {
    header: "Age",
    width: ".7fr",
    render: (r) => (
      <span className="font-mono text-xs text-text-muted">{r.age}</span>
    ),
  },
];

const capacityColumns: DataTableColumn<NodeCapacityRow>[] = [
  {
    header: "Name",
    width: "1.6fr",
    render: (r) => (
      <span className="truncate pr-3 font-mono text-[12.5px]">{r.name}</span>
    ),
  },
  {
    header: "CPU requested",
    width: "1.2fr",
    render: (r) => <CapacityBar usage={r.cpu} />,
  },
  {
    header: "Memory requested",
    width: "1.2fr",
    render: (r) => <CapacityBar usage={r.memory} />,
  },
  {
    header: "Pods",
    width: "1.2fr",
    render: (r) => <CapacityBar usage={r.pods} />,
  },
];

function CapacitySummaryCard({
  title,
  usage,
}: {
  title: string;
  usage: CapacityUsage;
}) {
  return (
    <div className="rounded-[10px] border border-border bg-surface p-[18px]">
      <div className="mb-2.5 flex items-center justify-between">
        <span className="text-[13px] font-semibold">{title}</span>
        <span className="font-mono text-xs text-text-muted">
          {usage.used} / {usage.total}
        </span>
      </div>
      <div className="h-2 overflow-hidden rounded bg-surface-2">
        <div
          className="h-full"
          style={{
            width: `${usage.pct}%`,
            background: colorVar[usage.color] ?? colorVar.ok,
          }}
        />
      </div>
    </div>
  );
}

function CapacityTab() {
  const { data, isLoading, error } = useCapacity();

  if (error) {
    return (
      <div className="text-sm text-err">
        Failed to load capacity: {friendlyErrorMessage(error)}
      </div>
    );
  }

  return (
    <div className="flex flex-col gap-[18px]">
      <div className="text-xs text-text-muted">
        Based on what&rsquo;s <em>requested</em> by scheduled pods against each
        node&rsquo;s allocatable capacity — not live usage (see the Nodes
        tab&rsquo;s CPU/Memory columns for that instead).
      </div>

      {isLoading || !data ? (
        <div className="text-sm text-text-muted">Loading…</div>
      ) : (
        <>
          <div className="grid grid-cols-3 gap-4">
            <CapacitySummaryCard title="Cluster CPU" usage={data.cluster.cpu} />
            <CapacitySummaryCard
              title="Cluster memory"
              usage={data.cluster.memory}
            />
            <CapacitySummaryCard
              title="Cluster pods"
              usage={data.cluster.pods}
            />
          </div>

          <DataTable
            columns={capacityColumns}
            rows={data.nodes}
            rowKey={(r) => r.name}
            rowHref={(r) => `/nodes/detail?name=${encodeURIComponent(r.name)}`}
            emptyLabel="No nodes found."
          />
        </>
      )}
    </div>
  );
}

// Why Add node (SSH + installer) isn't offered, and what to do instead, by the
// detected cluster type (Roadmap Phase 34).
const addNodeAdviceByType: Partial<Record<ClusterType, string>> = {
  k3d: "k3d nodes are containers, not machines you can log into — add one with `k3d node create <name> --cluster <cluster>`.",
  kind: "kind nodes are containers, not machines you can log into — recreate the cluster with more nodes in its config.",
  minikube:
    "minikube nodes are VMs or containers it manages itself — use `minikube node add`.",
  managed:
    "A managed cluster (EKS, GKE, AKS…) grows through its provider — change the node pool size there.",
};

export default function NodesPage() {
  const [tab, setTab] = useRememberedState<Tab>("nodes-tab", "nodes");
  const { data, isLoading, error, mutate } = useNodes();
  useNodesStream();
  const [addOpen, setAddOpen] = useState(false);
  // Adding a machine over SSH only makes sense for some kinds of cluster
  // (Roadmap Phase 34's detected type) — k3d/kind "nodes" are containers.
  const { data: clusters } = useClusters();
  const selected = useCurrentCluster();
  const activeCluster =
    clusters?.find((c) => c.name === selected) ??
    clusters?.find((c) => c.default);
  const canAddNode = activeCluster?.canAddNode ?? true;
  const addNodeAdvice = activeCluster
    ? (addNodeAdviceByType[activeCluster.type] ??
      "Nodes can't be added over SSH to this kind of cluster.")
    : "";

  return (
    <div className="flex flex-col gap-[18px] px-8 py-7">
      <div className="flex items-center justify-between">
        <div className="text-[15px] font-semibold">Nodes</div>
        <div className="flex items-center gap-3">
          <PillTabs items={tabs} active={tab} onChange={setTab} />
          <button
            type="button"
            onClick={() => setAddOpen(true)}
            disabled={!canAddNode}
            title={canAddNode ? undefined : addNodeAdvice}
            className="flex items-center gap-1.5 rounded-lg bg-accent px-3.5 py-2 text-[12.5px] font-semibold text-white disabled:cursor-not-allowed disabled:opacity-40"
          >
            <PlusIcon />
            Add node
          </button>
        </div>
      </div>

      {!canAddNode && (
        <div className="rounded-lg border border-border bg-surface p-3 text-xs text-text-muted">
          <span className="font-semibold text-text">
            Add node is off for this cluster.
          </span>{" "}
          {addNodeAdvice}{" "}
          <Link href="/clusters" className="text-accent-text underline">
            Wrong type? Change it on the Clusters page.
          </Link>
        </div>
      )}

      {tab === "nodes" ? (
        error ? (
          <div className="text-sm text-err">
            Failed to load nodes: {friendlyErrorMessage(error)}
          </div>
        ) : (
          <DataTable
            columns={columns}
            rows={data ?? []}
            rowKey={(r) => r.name}
            rowHref={(r) => `/nodes/detail?name=${encodeURIComponent(r.name)}`}
            isLoading={isLoading || !data}
            emptyLabel="No nodes found."
          />
        )
      ) : (
        <CapacityTab />
      )}

      {addOpen && (
        <AddNodeModal
          onClose={() => setAddOpen(false)}
          onFinished={() => void mutate()}
        />
      )}
    </div>
  );
}
