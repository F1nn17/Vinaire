"use client";

import { useState } from "react";
import { useNamespaces } from "@/lib/hooks/useNamespaces";
import { friendlyErrorMessage } from "@/lib/api/errors";
import type { NamespaceRow } from "@/lib/types";
import { DataTable, type DataTableColumn } from "@/components/ui/DataTable";
import { StatusDot } from "@/components/ui/StatusDot";
import { QuotaBar } from "@/components/ui/QuotaBar";
import { CreateResourceModal } from "@/components/editor/CreateResourceModal";
import { namespaceTemplates } from "@/components/editor/templates";
import { PlusIcon } from "@/components/icons";

const columns: DataTableColumn<NamespaceRow>[] = [
  {
    header: "Name",
    width: "1.6fr",
    render: (r) => (
      <span className="truncate pr-3 font-mono text-[12.5px]">{r.name}</span>
    ),
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
    header: "Pods",
    width: ".6fr",
    render: (r) => (
      <span className="font-mono text-xs text-text-muted">{r.pods ?? "—"}</span>
    ),
  },
  {
    header: "Quota usage",
    width: "2.4fr",
    render: (r) =>
      r.quota.length === 0 ? (
        <span className="text-xs text-text-faint">No quota</span>
      ) : (
        <div className="flex flex-wrap gap-x-4 gap-y-1.5 py-1">
          {r.quota.map((q) => (
            <QuotaBar key={q.resource} row={q} />
          ))}
        </div>
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

export default function NamespacesPage() {
  const { data, isLoading, error, mutate } = useNamespaces();
  const [createOpen, setCreateOpen] = useState(false);

  return (
    <div className="flex flex-col gap-[18px] px-8 py-7">
      <div className="flex items-center justify-between">
        <div className="text-[15px] font-semibold">Namespaces</div>
        <button
          type="button"
          onClick={() => setCreateOpen(true)}
          className="flex items-center gap-1.5 rounded-lg bg-accent px-3.5 py-2 text-[12.5px] font-semibold text-white"
        >
          <PlusIcon />
          Create
        </button>
      </div>

      {error ? (
        <div className="text-sm text-err">
          Failed to load namespaces: {friendlyErrorMessage(error)}
        </div>
      ) : (
        <DataTable
          columns={columns}
          rows={data ?? []}
          rowKey={(r) => r.name}
          rowHref={(r) => r.href}
          isLoading={isLoading || !data}
          emptyLabel="No namespaces found."
        />
      )}

      {createOpen && (
        <CreateResourceModal
          templates={namespaceTemplates}
          onClose={() => setCreateOpen(false)}
          onCreated={() => void mutate()}
        />
      )}
    </div>
  );
}
