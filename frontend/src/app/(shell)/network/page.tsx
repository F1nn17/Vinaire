"use client";

import { useState } from "react";
import { useGatewayResources, useIngresses } from "@/lib/hooks/useNetwork";
import type { GatewayRow, IngressRow } from "@/lib/types";
import { DataTable, type DataTableColumn } from "@/components/ui/DataTable";
import { PillTabs } from "@/components/ui/Tabs";
import { StatusDot } from "@/components/ui/StatusDot";
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
  const { data: ingressRows, isLoading: ingressLoading } = useIngresses();
  const { data: gatewayRows, isLoading: gatewayLoading } =
    useGatewayResources();

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

      <PillTabs items={tabs} active={tab} onChange={setTab} />

      {tab === "ingress" &&
        (ingressLoading || !ingressRows ? (
          <div className="text-sm text-text-muted">Loading…</div>
        ) : (
          <DataTable
            columns={ingressColumns}
            rows={ingressRows}
            rowKey={(r) => r.name}
          />
        ))}

      {tab === "gateway" &&
        (gatewayLoading || !gatewayRows ? (
          <div className="text-sm text-text-muted">Loading…</div>
        ) : (
          <DataTable
            columns={gatewayColumns}
            rows={gatewayRows}
            rowKey={(r) => r.name}
          />
        ))}
    </div>
  );
}
