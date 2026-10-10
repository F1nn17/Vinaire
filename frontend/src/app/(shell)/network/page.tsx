"use client";

import { useRememberedState } from "@/lib/hooks/useRememberedState";
import { useState } from "react";
import { t } from "@/lib/i18n/translate";
import { useGatewayResources, useIngresses } from "@/lib/hooks/useNetwork";
import { useResourceTable, type TableRow } from "@/lib/api/allResources";
import { BulkBar } from "@/components/workloads/BulkActions";
import { BULK_MAX } from "@/lib/api/bulk";
import { friendlyErrorMessage } from "@/lib/api/errors";
import type { GatewayRow, IngressRow } from "@/lib/types";
import { DataTable, type DataTableColumn } from "@/components/ui/DataTable";
import { PillTabs } from "@/components/ui/Tabs";
import { StatusDot } from "@/components/ui/StatusDot";
import { TableSkeleton } from "@/components/ui/Skeleton";
import { CreateResourceModal } from "@/components/editor/CreateResourceModal";
import { networkTemplates } from "@/components/editor/templates";
import { PlusIcon } from "@/components/icons";

type Tab = "services" | "ingress" | "gateway";

const tabs: { key: Tab; label: string }[] = [
  { key: "services", label: "Services" },
  { key: "ingress", label: "Ingress" },
  { key: "gateway", label: "Gateway API" },
];

const ingressColumns: DataTableColumn<IngressRow>[] = [
  {
    header: "Name",
    width: "1.6fr",
    render: (r) => <span className="font-mono text-[12.5px]">{r.name}</span>,
  },
  {
    header: "Namespace",
    width: ".8fr",
    render: (r) => (
      <span className="font-mono text-xs text-text-muted">{r.namespace}</span>
    ),
  },
  {
    header: "Host",
    width: "1.6fr",
    render: (r) => <span className="font-mono text-xs">{r.hosts}</span>,
  },
  {
    header: "Backend",
    width: "1.6fr",
    render: (r) => (
      <span className="font-mono text-xs text-text-muted">{r.backend}</span>
    ),
  },
  {
    header: "TLS",
    width: "1.2fr",
    render: (r) => (
      <span className="flex items-center gap-1.5 text-xs">
        <StatusDot color={r.tlsColor} />
        {r.tls}
      </span>
    ),
  },
];

const gatewayColumns: DataTableColumn<GatewayRow>[] = [
  {
    header: "Name",
    width: "1.8fr",
    render: (r) => <span className="font-mono text-[12.5px]">{r.name}</span>,
  },
  {
    header: "Namespace",
    width: "1fr",
    render: (r) => (
      <span className="font-mono text-xs text-text-muted">{r.namespace}</span>
    ),
  },
  {
    header: "Kind",
    width: "1fr",
    render: (r) => (
      <span className="font-mono text-xs text-text-muted">{r.kind}</span>
    ),
  },
  {
    header: "Status",
    width: "1.2fr",
    render: (r) => (
      <span className="flex items-center gap-1.5 text-xs">
        <StatusDot color={r.statusColor} />
        {r.status}
      </span>
    ),
  },
];

function serviceColumns(
  columns: { name: string }[],
): DataTableColumn<TableRow>[] {
  const c = (name: string, header: string, width: string, mono = true) => ({
    header,
    width,
    render: (r: TableRow) => (
      <span className={`${mono ? "font-mono " : ""}truncate text-xs`}>
        {cell(r, columns, name) || "—"}
      </span>
    ),
  });
  return [
    {
      header: "Name",
      width: "1.6fr",
      render: (r) => (
        <span className="truncate pr-3 font-mono text-[12.5px]">{r.name}</span>
      ),
    },
    {
      header: "Namespace",
      width: ".8fr",
      render: (r) => (
        <span className="font-mono text-xs text-text-muted">{r.namespace}</span>
      ),
    },
    c("type", "Type", ".8fr"),
    c("cluster-ip", "Cluster IP", "1fr"),
    c("external-ip", "External IP", "1fr"),
    c("port(s)", "Ports", "1.6fr"),
    c("age", "Age", ".6fr"),
  ];
}

// A Service row is the cluster's own table (`kubectl get svc`): the columns are found by name.
function cell(
  row: TableRow,
  columns: { name: string }[],
  name: string,
): string {
  const i = columns.findIndex((c) => c.name.toLowerCase() === name);
  return i >= 0 ? (row.cells[i] ?? "") : "";
}

const openLink = (kind: string, row: { namespace?: string; name: string }) =>
  `/resources/detail?r=${encodeURIComponent(kind)}&ns=${encodeURIComponent(row.namespace ?? "")}&name=${encodeURIComponent(row.name)}`;

export default function NetworkPage() {
  const [tab, setTab] = useRememberedState<Tab>("network-tab", "ingress");
  const [namespace, setNamespace] = useState("");
  const {
    data: ingressRows,
    isLoading: ingressLoading,
    error: ingressError,
    mutate: mutateIngresses,
  } = useIngresses(namespace);
  const [createOpen, setCreateOpen] = useState(false);
  const svcTable = useResourceTable("core/v1/services", namespace);
  // ticked rows, per tab (the selection belongs to one list)
  const [picked, setPicked] = useState<{ tab: Tab; keys: Set<string> }>({
    tab: "services",
    keys: new Set(),
  });
  const selected = picked.tab === tab ? picked.keys : new Set<string>();
  const svcRows = svcTable.data?.rows ?? [];
  const rowsOfTab: { namespace?: string; name: string }[] =
    tab === "services" ? svcRows : tab === "ingress" ? (ingressRows ?? []) : [];
  const selectedItems = rowsOfTab
    .filter((r) => selected.has(`${r.namespace}/${r.name}`))
    .map((r) => ({ namespace: r.namespace ?? "", name: r.name }));
  const {
    data: gatewayResult,
    isLoading: gatewayLoading,
    error: gatewayError,
  } = useGatewayResources(namespace);

  return (
    <div className="flex flex-col gap-[18px] px-8 py-7">
      <div className="flex items-center justify-between">
        <div className="text-[15px] font-semibold">Services &amp; Ingress</div>
        <button
          type="button"
          onClick={() => setCreateOpen(true)}
          className="flex items-center gap-1.5 rounded-lg bg-accent px-3.5 py-2 text-[12.5px] font-semibold text-white"
        >
          <PlusIcon />
          Create
        </button>
      </div>

      <div className="flex items-center justify-between gap-3">
        <PillTabs items={tabs} active={tab} onChange={setTab} />

        <label className="flex items-center gap-2">
          <span className="sr-only">Filter by namespace</span>
          <input
            type="text"
            value={namespace}
            onChange={(e) => setNamespace(e.target.value)}
            placeholder="All namespaces"
            className="rounded-lg border border-border bg-surface px-3 py-1.75 font-mono text-xs outline-none placeholder:text-text-faint"
          />
        </label>
      </div>

      {(tab === "services" || tab === "ingress") && (
        <>
          <BulkBar
            kind={tab === "services" ? "services" : "ingresses"}
            items={selectedItems}
            onClear={() => setPicked({ tab, keys: new Set() })}
            onFinished={(done) => {
              const left = new Set(selected);
              for (const k of done) left.delete(k);
              setPicked({ tab, keys: left });
              void mutateIngresses();
              void svcTable.mutate();
            }}
          />
          <div className="text-xs text-text-muted">
            {t(
              "Tick rows to delete several at once. Click a row to open it; its YAML tab edits it.",
            )}
          </div>
        </>
      )}

      {tab === "services" &&
        (svcTable.error ? (
          <div className="text-sm text-err">
            Failed to load Services: {friendlyErrorMessage(svcTable.error)}
          </div>
        ) : (
          <DataTable
            columns={serviceColumns(svcTable.data?.columns ?? [])}
            rows={svcRows}
            rowKey={(r) => `${r.namespace}/${r.name}`}
            rowHref={(r) => openLink("core/v1/services", r)}
            isLoading={svcTable.isLoading || !svcTable.data}
            selection={{
              selected,
              onChange: (keys) => setPicked({ tab, keys }),
              max: BULK_MAX,
            }}
            emptyLabel={`No Services found${namespace ? ` in namespace "${namespace}"` : ""}.`}
          />
        ))}

      {tab === "ingress" &&
        (ingressError ? (
          <div className="text-sm text-err">
            Failed to load Ingresses: {friendlyErrorMessage(ingressError)}
          </div>
        ) : (
          <DataTable
            columns={ingressColumns}
            rows={ingressRows ?? []}
            rowKey={(r) => `${r.namespace}/${r.name}`}
            rowHref={(r) => openLink("networking.k8s.io/v1/ingresses", r)}
            selection={{
              selected,
              onChange: (keys) => setPicked({ tab, keys }),
              max: BULK_MAX,
            }}
            isLoading={ingressLoading || !ingressRows}
            emptyLabel={`No Ingresses found${namespace ? ` in namespace "${namespace}"` : ""}.`}
          />
        ))}

      {tab === "gateway" &&
        (gatewayError ? (
          <div className="text-sm text-err">
            Failed to load Gateway API resources:{" "}
            {friendlyErrorMessage(gatewayError)}
          </div>
        ) : gatewayLoading || !gatewayResult ? (
          <TableSkeleton columns={gatewayColumns.length} />
        ) : !gatewayResult.available ? (
          <div className="rounded-[10px] border border-border bg-surface p-5 text-sm text-text-muted">
            Gateway API isn&rsquo;t available on this cluster:{" "}
            {gatewayResult.reason}
          </div>
        ) : (
          <DataTable
            columns={gatewayColumns}
            rows={gatewayResult.rows ?? []}
            rowKey={(r) => `${r.namespace}/${r.kind}/${r.name}`}
            emptyLabel={`No Gateway API resources found${namespace ? ` in namespace "${namespace}"` : ""}.`}
          />
        ))}

      {createOpen && (
        <CreateResourceModal
          templates={networkTemplates[tab] ?? []}
          onClose={() => setCreateOpen(false)}
          onCreated={() => {
            void mutateIngresses();
            void svcTable.mutate();
          }}
        />
      )}
    </div>
  );
}
