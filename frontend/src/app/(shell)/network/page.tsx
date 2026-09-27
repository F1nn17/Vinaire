"use client";

import { useState } from "react";
import { useGatewayResources, useIngresses } from "@/lib/hooks/useNetwork";
import { friendlyErrorMessage } from "@/lib/api/errors";
import type { GatewayRow, IngressRow } from "@/lib/types";
import { DataTable, type DataTableColumn } from "@/components/ui/DataTable";
import { PillTabs } from "@/components/ui/Tabs";
import { StatusDot } from "@/components/ui/StatusDot";
import { TableSkeleton } from "@/components/ui/Skeleton";
import { PlusIcon } from "@/components/icons";

type Tab = "ingress" | "gateway";

const tabs: { key: Tab; label: string }[] = [
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

export default function NetworkPage() {
  const [tab, setTab] = useState<Tab>("ingress");
  const [namespace, setNamespace] = useState("");
  const {
    data: ingressRows,
    isLoading: ingressLoading,
    error: ingressError,
  } = useIngresses(namespace);
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
            rows={gatewayResult.rows}
            rowKey={(r) => `${r.namespace}/${r.kind}/${r.name}`}
            emptyLabel={`No Gateway API resources found${namespace ? ` in namespace "${namespace}"` : ""}.`}
          />
        ))}
    </div>
  );
}
