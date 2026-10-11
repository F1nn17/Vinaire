"use client";

import { NamespaceSelect } from "@/components/ui/NamespaceSelect";
import { useRememberedState } from "@/lib/hooks/useRememberedState";
import { useState } from "react";
import { usePVCs, usePVs, useStorageClasses } from "@/lib/hooks/useStorage";
import { friendlyErrorMessage } from "@/lib/api/errors";
import type { PVCRow, PVRow, StorageClassRow } from "@/lib/types";
import { DataTable, type DataTableColumn } from "@/components/ui/DataTable";
import { PillTabs } from "@/components/ui/Tabs";
import { StatusDot } from "@/components/ui/StatusDot";

type StorageTab = "pvcs" | "pvs" | "storageclasses";

const tabs: { key: StorageTab; label: string }[] = [
  { key: "pvcs", label: "PVCs" },
  { key: "pvs", label: "PersistentVolumes" },
  { key: "storageclasses", label: "StorageClasses" },
];

const pvcColumns: DataTableColumn<PVCRow>[] = [
  {
    header: "Name",
    width: "2fr",
    render: (r) => (
      <span className="truncate pr-3 font-mono text-[12.5px]">{r.name}</span>
    ),
  },
  {
    header: "Namespace",
    width: "1fr",
    render: (r) => (
      <span className="font-mono text-xs text-text-muted">{r.namespace}</span>
    ),
  },
  {
    header: "Status",
    width: "1fr",
    render: (r) => (
      <span className="flex items-center gap-1.5 text-xs">
        <StatusDot color={r.statusColor} />
        {r.status}
      </span>
    ),
  },
  {
    header: "Capacity",
    width: "1fr",
    render: (r) => (
      <span className="font-mono text-xs text-text-muted">{r.capacity}</span>
    ),
  },
  {
    header: "Access modes",
    width: "1.2fr",
    render: (r) => (
      <span className="font-mono text-xs text-text-muted">{r.accessModes}</span>
    ),
  },
  {
    header: "Storage class",
    width: "1.2fr",
    render: (r) => (
      <span className="font-mono text-xs text-text-muted">
        {r.storageClass}
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

const pvColumns: DataTableColumn<PVRow>[] = [
  {
    header: "Name",
    width: "1.8fr",
    render: (r) => (
      <span className="truncate pr-3 font-mono text-[12.5px]">{r.name}</span>
    ),
  },
  {
    header: "Status",
    width: ".9fr",
    render: (r) => (
      <span className="flex items-center gap-1.5 text-xs">
        <StatusDot color={r.statusColor} />
        {r.status}
      </span>
    ),
  },
  {
    header: "Capacity",
    width: ".9fr",
    render: (r) => (
      <span className="font-mono text-xs text-text-muted">{r.capacity}</span>
    ),
  },
  {
    header: "Reclaim policy",
    width: "1fr",
    render: (r) => (
      <span className="font-mono text-xs text-text-muted">
        {r.reclaimPolicy}
      </span>
    ),
  },
  {
    header: "Storage class",
    width: "1.1fr",
    render: (r) => (
      <span className="font-mono text-xs text-text-muted">
        {r.storageClass}
      </span>
    ),
  },
  {
    header: "Claim",
    width: "1.3fr",
    render: (r) => (
      <span className="font-mono text-xs text-text-muted">{r.claimRef}</span>
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

const storageClassColumns: DataTableColumn<StorageClassRow>[] = [
  {
    header: "Name",
    width: "1.6fr",
    render: (r) => (
      <span className="flex items-center gap-2 truncate pr-3 font-mono text-[12.5px]">
        {r.name}
        {r.isDefault && (
          <span className="rounded-full border border-border bg-surface-2 px-1.5 py-[1px] text-[10px] text-text-muted">
            default
          </span>
        )}
      </span>
    ),
  },
  {
    header: "Provisioner",
    width: "1.6fr",
    render: (r) => (
      <span className="font-mono text-xs text-text-muted">{r.provisioner}</span>
    ),
  },
  {
    header: "Reclaim policy",
    width: "1fr",
    render: (r) => (
      <span className="font-mono text-xs text-text-muted">
        {r.reclaimPolicy}
      </span>
    ),
  },
  {
    header: "Volume binding",
    width: "1.1fr",
    render: (r) => (
      <span className="font-mono text-xs text-text-muted">
        {r.volumeBindingMode}
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

export default function StoragePage() {
  const [tab, setTab] = useRememberedState<StorageTab>("storage-tab", "pvcs");
  const [namespace, setNamespace] = useState("");

  const pvcs = usePVCs(namespace, tab === "pvcs");
  const pvs = usePVs(tab === "pvs");
  const storageClasses = useStorageClasses(tab === "storageclasses");

  const active = tab === "pvcs" ? pvcs : tab === "pvs" ? pvs : storageClasses;

  return (
    <div className="flex flex-col gap-[18px] px-8 py-7">
      <div className="text-[15px] font-semibold">Storage</div>

      <div className="flex items-center justify-between gap-3">
        <PillTabs items={tabs} active={tab} onChange={setTab} />

        {tab === "pvcs" && (
          <label className="flex items-center gap-2">
            <span className="sr-only">Filter by namespace</span>
            <NamespaceSelect value={namespace} onChange={setNamespace} />
          </label>
        )}
      </div>

      {active.error ? (
        <div className="text-sm text-err">
          Failed to load {tab}: {friendlyErrorMessage(active.error)}
        </div>
      ) : tab === "pvcs" ? (
        <DataTable
          columns={pvcColumns}
          rows={pvcs.data ?? []}
          rowKey={(r) => `${r.namespace}/${r.name}`}
          rowHref={(r) => r.href}
          isLoading={pvcs.isLoading || !pvcs.data}
          emptyLabel={`No PVCs found${namespace ? ` in namespace "${namespace}"` : ""}.`}
        />
      ) : tab === "pvs" ? (
        <DataTable
          columns={pvColumns}
          rows={pvs.data ?? []}
          rowKey={(r) => r.name}
          isLoading={pvs.isLoading || !pvs.data}
          emptyLabel="No PersistentVolumes found."
        />
      ) : (
        <DataTable
          columns={storageClassColumns}
          rows={storageClasses.data ?? []}
          rowKey={(r) => r.name}
          isLoading={storageClasses.isLoading || !storageClasses.data}
          emptyLabel="No StorageClasses found."
        />
      )}
    </div>
  );
}
