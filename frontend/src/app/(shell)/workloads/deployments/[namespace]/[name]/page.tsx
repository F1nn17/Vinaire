"use client";

import Link from "next/link";
import { useParams } from "next/navigation";
import { useState } from "react";
import { useDeployment } from "@/lib/hooks/useWorkloads";
import type { DeploymentPodRow } from "@/lib/types";
import { StatusDot } from "@/components/ui/StatusDot";
import { UnderlineTabs } from "@/components/ui/Tabs";
import { CodeBlock } from "@/components/ui/CodeBlock";
import { DataTable, type DataTableColumn } from "@/components/ui/DataTable";

type Tab = "overview" | "pods" | "revisions" | "yaml";

const tabs: { key: Tab; label: string }[] = [
  { key: "overview", label: "Overview" },
  { key: "pods", label: "Pods" },
  { key: "revisions", label: "Revisions" },
  { key: "yaml", label: "YAML" },
];

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

export default function DeploymentDetailPage() {
  const { namespace, name } = useParams<{ namespace: string; name: string }>();
  const deploymentNamespace = decodeURIComponent(namespace);
  const deploymentName = decodeURIComponent(name);
  const { data: deployment, isLoading } = useDeployment(
    deploymentNamespace,
    deploymentName,
  );
  const [tab, setTab] = useState<Tab>("overview");

  if (isLoading) {
    return <div className="p-8 text-sm text-text-muted">Loading…</div>;
  }

  if (!deployment) {
    return (
      <div className="p-8 text-sm text-text-muted">
        Deployment &ldquo;{deploymentNamespace}/{deploymentName}&rdquo; not
        found in mock data.
      </div>
    );
  }

  return (
    <div className="flex flex-col gap-[18px] px-8 py-7">
      <div className="flex items-start justify-between">
        <div>
          <div className="font-mono text-xs text-text-faint">
            <Link href="/workloads">Workloads</Link> /{" "}
            <Link href="/workloads">Deployments</Link>
          </div>
          <div className="mt-1.5 flex items-center gap-2.5">
            <span className="font-mono text-lg font-semibold">
              {deployment.name}
            </span>
            <span className="flex items-center gap-1.5 rounded-full border border-border bg-surface px-2.5 py-[3px] text-xs text-text-muted">
              <StatusDot color={deployment.statusColor} />
              {deployment.readySummary}
            </span>
          </div>
        </div>
        <div className="flex shrink-0 gap-2.5">
          <button
            type="button"
            title="TODO: wire up to backend — proposes a GitOps PR, does not call the Kubernetes API directly"
            className="rounded-lg border border-border bg-surface px-3.5 py-2 text-[12.5px]"
          >
            Restart rollout
          </button>
          <button
            type="button"
            title="TODO: wire up to backend — proposes a GitOps PR, does not call the Kubernetes API directly"
            className="rounded-lg border border-err px-3.5 py-2 text-[12.5px] text-err"
          >
            Delete
          </button>
        </div>
      </div>

      <UnderlineTabs items={tabs} active={tab} onChange={setTab} />

      {tab === "overview" && (
        <div className="max-w-xl rounded-[10px] border border-border bg-surface p-5">
          <div className="mb-3.5 text-[13px] font-semibold">Metadata</div>
          <div className="flex flex-col gap-2.5">
            <Row label="Namespace" value={deployment.namespace} />
            <Row label="Replicas" value={deployment.replicas} />
            <Row label="Strategy" value={deployment.strategy} />
            <Row label="Image" value={deployment.image} />
            <Row label="Managed by" value={deployment.managedBy} />
            <Row label="Age" value={deployment.age} />
          </div>
        </div>
      )}

      {tab === "pods" && (
        <DataTable
          columns={podColumns}
          rows={deployment.pods}
          rowKey={(r) => r.name}
          rowHref={(r) => `/workloads/pods/${deploymentNamespace}/${r.name}`}
        />
      )}

      {tab === "revisions" && (
        <div className="flex max-w-xl flex-col gap-2.5">
          {deployment.revisions.map((rev) => (
            <div
              key={rev.revision}
              className="flex items-center justify-between rounded-[10px] border p-4"
              style={{
                borderColor: rev.current ? "var(--accent)" : "var(--border)",
                background: "var(--surface)",
              }}
            >
              <div>
                <div className="text-[13px] font-semibold">
                  Revision {rev.revision}{" "}
                  {rev.current && (
                    <span className="text-[11px] font-normal text-accent-text">
                      · current
                    </span>
                  )}
                </div>
                <div className="mt-0.5 font-mono text-[11px] text-text-faint">
                  {rev.image} · {rev.age}
                </div>
              </div>
              {!rev.current && (
                <button
                  type="button"
                  title="TODO: wire up to backend — proposes a GitOps PR reverting the manifest, does not call the Kubernetes API directly"
                  className="rounded-md border border-border bg-surface-2 px-3 py-1.5 text-xs"
                >
                  Rollback
                </button>
              )}
            </div>
          ))}
        </div>
      )}

      {tab === "yaml" && <CodeBlock code={deployment.yaml} />}
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
