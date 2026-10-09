"use client";

import { useState } from "react";
import { t } from "@/lib/i18n/translate";
import { useWorkloads } from "@/lib/hooks/useWorkloads";
import { useWorkloadsStream } from "@/lib/hooks/useWorkloadsStream";
import { friendlyErrorMessage } from "@/lib/api/errors";
import { workloadTabs } from "@/lib/mock/workloads";
import type { WorkloadKind, WorkloadRow } from "@/lib/types";
import { DataTable, type DataTableColumn } from "@/components/ui/DataTable";
import { PillTabs } from "@/components/ui/Tabs";
import { StatusDot } from "@/components/ui/StatusDot";

const columns: DataTableColumn<WorkloadRow>[] = [
  {
    header: "Name",
    width: "2.4fr",
    render: (r) => (
      <span className="truncate pr-3 font-mono text-[12.5px]">{r.name}</span>
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
    header: "Restarts",
    width: ".7fr",
    render: (r) => (
      <span className="font-mono text-xs text-text-muted">{r.restarts}</span>
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

// The workloads of one namespace, as on the Workloads page but already narrowed to it — what you
// expect to see when you open a namespace.
export function NamespaceWorkloads({ namespace }: { namespace: string }) {
  const [kind, setKind] = useState<WorkloadKind>("pods");
  const { data, isLoading, error } = useWorkloads(kind, namespace);
  useWorkloadsStream(kind, namespace);
  return (
    <div className="flex flex-col gap-3">
      <div>
        <PillTabs items={workloadTabs} active={kind} onChange={setKind} />
      </div>
      {error ? (
        <div className="text-sm text-err">
          {t("Failed to load")} {kind}: {friendlyErrorMessage(error)}
        </div>
      ) : (
        <DataTable
          columns={columns}
          rows={(data ?? []).filter((r) => r.namespace === namespace)}
          rowKey={(r) => `${r.namespace}/${r.name}`}
          rowHref={(r) => r.href}
          isLoading={isLoading || !data}
          emptyLabel={t('No {tab} found in namespace "{ns}".', {
            tab: kind,
            ns: namespace,
          })}
        />
      )}
    </div>
  );
}
