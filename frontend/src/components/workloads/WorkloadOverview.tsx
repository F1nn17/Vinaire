"use client";

import type { ReactNode } from "react";
import type { DeploymentPodRow, MetricResourceType } from "@/lib/types";
import { DataTable, type DataTableColumn } from "@/components/ui/DataTable";
import { StatusDot } from "@/components/ui/StatusDot";
import { MetricsPanel } from "@/components/metrics/MetricsPanel";

// The Overview of a workload's detail page (Roadmap Phase 66): its metadata on the left, its
// pods on the right, the usage history below — the layout the Deployment page has had since
// Phase 32, now shared by StatefulSets, DaemonSets and Jobs so the pages can't drift apart again.

const podColumns: DataTableColumn<DeploymentPodRow>[] = [
  {
    header: "Name",
    width: "2.4fr",
    render: (r) => <span className="font-mono text-[12.5px]">{r.name}</span>,
  },
  {
    header: "Node",
    width: "1fr",
    render: (r) => (
      <span className="font-mono text-xs text-text-muted">{r.node}</span>
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

export function WorkloadOverview({
  resourceType,
  namespace,
  name,
  pods,
  emptyLabel,
  children,
}: {
  resourceType: Exclude<MetricResourceType, "nodes" | "pods">;
  namespace: string;
  name: string;
  pods: DeploymentPodRow[];
  emptyLabel?: string;
  // the metadata card
  children: ReactNode;
}) {
  return (
    <div className="flex flex-col gap-5">
      <div className="grid gap-5 xl:grid-cols-[minmax(0,26rem)_minmax(0,1fr)]">
        <div>{children}</div>
        <div className="min-w-0">
          <div className="mb-2.5 text-[13px] font-semibold">
            Pods ({pods.length})
          </div>
          <DataTable
            columns={podColumns}
            rows={pods}
            rowKey={(r) => r.name}
            rowHref={(r) =>
              `/workloads/pods/detail?namespace=${encodeURIComponent(namespace)}&name=${encodeURIComponent(r.name)}`
            }
            emptyLabel={emptyLabel ?? "No pods."}
          />
        </div>
      </div>
      <MetricsPanel
        resourceType={resourceType}
        namespace={namespace}
        name={name}
      />
    </div>
  );
}
