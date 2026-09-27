"use client";

import { useNodes } from "@/lib/hooks/useWorkloads";
import { useNodesStream } from "@/lib/hooks/useNodesStream";
import { friendlyErrorMessage } from "@/lib/api/errors";
import type { NodeRow } from "@/lib/types";
import { DataTable, type DataTableColumn } from "@/components/ui/DataTable";
import { StatusDot } from "@/components/ui/StatusDot";

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

export default function NodesPage() {
  const { data, isLoading, error } = useNodes();
  useNodesStream();

  return (
    <div className="flex flex-col gap-[18px] px-8 py-7">
      <div className="text-[15px] font-semibold">Nodes</div>

      {error ? (
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
      )}
    </div>
  );
}
