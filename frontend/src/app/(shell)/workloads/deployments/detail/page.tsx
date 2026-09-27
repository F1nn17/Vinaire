"use client";

import Link from "next/link";
import { useSearchParams, useRouter } from "next/navigation";
import { Suspense, useState } from "react";
import { useDeployment } from "@/lib/hooks/useWorkloads";
import { friendlyErrorMessage } from "@/lib/api/errors";
import type { DeploymentPodRow } from "@/lib/types";
import { StatusDot } from "@/components/ui/StatusDot";
import { UnderlineTabs } from "@/components/ui/Tabs";
import { CodeBlock } from "@/components/ui/CodeBlock";
import { DataTable, type DataTableColumn } from "@/components/ui/DataTable";
import { ConfirmModal } from "@/components/ui/ConfirmModal";
import { Toast, type ToastState } from "@/components/ui/Toast";
import {
  deleteDeployment,
  restartDeployment,
  rollbackDeployment,
} from "@/lib/api/deploymentActions";

type PendingAction =
  | { kind: "restart" }
  | { kind: "delete" }
  | { kind: "rollback"; revision: number };

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

// A static export can't pre-render a page per deployment name (unknowable
// at build time) — this route is a single static page,
// `/workloads/deployments/detail`, that reads which deployment to show
// from `?namespace=&name=` instead of a `[namespace]/[name]` route
// segment. See AGENTS.md's "Rendering model".
export default function DeploymentDetailPageRoute() {
  return (
    <Suspense
      fallback={<div className="p-8 text-sm text-text-muted">Loading…</div>}
    >
      <DeploymentDetailPage />
    </Suspense>
  );
}

function DeploymentDetailPage() {
  const router = useRouter();
  const searchParams = useSearchParams();
  const deploymentNamespace = searchParams.get("namespace") ?? "";
  const deploymentName = searchParams.get("name") ?? "";
  const {
    data: deployment,
    isLoading,
    error,
    mutate,
  } = useDeployment(deploymentNamespace, deploymentName);
  const [tab, setTab] = useState<Tab>("overview");
  const [pending, setPending] = useState<PendingAction | null>(null);
  const [busy, setBusy] = useState(false);
  const [toast, setToast] = useState<ToastState>(null);

  async function handleConfirm() {
    if (!pending) return;
    setBusy(true);
    try {
      if (pending.kind === "restart") {
        await restartDeployment(deploymentNamespace, deploymentName);
        setToast({ type: "success", message: "Rollout restarted." });
        await mutate();
      } else if (pending.kind === "delete") {
        await deleteDeployment(deploymentNamespace, deploymentName);
        setToast({ type: "success", message: "Deployment deleted." });
        router.push("/workloads");
      } else {
        await rollbackDeployment(
          deploymentNamespace,
          deploymentName,
          pending.revision,
        );
        setToast({
          type: "success",
          message: `Rolled back to revision ${pending.revision}.`,
        });
        await mutate();
      }
      setPending(null);
    } catch (err) {
      setToast({
        type: "error",
        message: friendlyErrorMessage(err),
      });
    } finally {
      setBusy(false);
    }
  }

  if (error) {
    return (
      <div className="p-8 text-sm text-err">
        Failed to load deployment: {friendlyErrorMessage(error)}
      </div>
    );
  }

  if (isLoading) {
    return <div className="p-8 text-sm text-text-muted">Loading…</div>;
  }

  if (!deployment) {
    return (
      <div className="p-8 text-sm text-text-muted">
        Deployment &ldquo;{deploymentNamespace}/{deploymentName}&rdquo; not
        found.
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
            onClick={() => setPending({ kind: "restart" })}
            className="rounded-lg border border-border bg-surface px-3.5 py-2 text-[12.5px]"
          >
            Restart rollout
          </button>
          <button
            type="button"
            onClick={() => setPending({ kind: "delete" })}
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
          rowHref={(r) =>
            `/workloads/pods/detail?namespace=${encodeURIComponent(deploymentNamespace)}&name=${encodeURIComponent(r.name)}`
          }
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
                  onClick={() =>
                    setPending({ kind: "rollback", revision: rev.revision })
                  }
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

      <ConfirmModal
        open={pending !== null}
        title={confirmTitle(pending)}
        message={confirmMessage(pending, deploymentName)}
        confirmLabel={pending?.kind === "delete" ? "Delete" : "Confirm"}
        danger={pending?.kind === "delete"}
        busy={busy}
        onConfirm={handleConfirm}
        onCancel={() => setPending(null)}
      />
      <Toast toast={toast} onDismiss={() => setToast(null)} />
    </div>
  );
}

function confirmTitle(pending: PendingAction | null): string {
  if (!pending) return "";
  if (pending.kind === "restart") return "Restart rollout?";
  if (pending.kind === "delete") return "Delete deployment?";
  return "Rollback deployment?";
}

function confirmMessage(pending: PendingAction | null, name: string): string {
  if (!pending) return "";
  if (pending.kind === "restart") {
    return `This will trigger a rolling restart of "${name}". Pods will be recreated one by one.`;
  }
  if (pending.kind === "delete") {
    return `This permanently deletes "${name}" and all its pods. This cannot be undone.`;
  }
  return `This will roll "${name}" back to revision ${pending.revision}, replacing its current pod template.`;
}

function Row({ label, value }: { label: string; value: string }) {
  return (
    <div className="flex justify-between">
      <span className="text-xs text-text-muted">{label}</span>
      <span className="font-mono text-xs">{value}</span>
    </div>
  );
}
