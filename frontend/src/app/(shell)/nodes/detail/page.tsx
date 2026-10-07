"use client";

import Link from "next/link";
import { useRouter, useSearchParams } from "next/navigation";
import { Suspense, useState } from "react";
import { useNode } from "@/lib/hooks/useWorkloads";
import { friendlyErrorMessage } from "@/lib/api/errors";
import type { NodeScheduledPodRow } from "@/lib/types";
import { StatusDot } from "@/components/ui/StatusDot";
import { UnderlineTabs } from "@/components/ui/Tabs";
import { MetricsPanel } from "@/components/metrics/MetricsPanel";
import { YamlPanel } from "@/components/editor/YamlPanel";
import { DataTable, type DataTableColumn } from "@/components/ui/DataTable";
import { ActionBar, ActionButton } from "@/components/ui/ActionBar";
import { Toast, type ToastState } from "@/components/ui/Toast";
import { ConfirmModal } from "@/components/ui/ConfirmModal";
import { TypeToConfirmModal } from "@/components/ui/TypeToConfirmModal";
import { DrainModal } from "@/components/nodes/DrainModal";
import { NodeMetaModal } from "@/components/nodes/NodeMetaModal";
import {
  DrainIcon,
  PauseIcon,
  PlayIcon,
  ShieldIcon,
  TagIcon,
  TrashIcon,
} from "@/components/icons";
import { cordonNode, deleteNode, uncordonNode } from "@/lib/api/nodeActions";

type Tab = "overview" | "pods" | "metrics" | "yaml";

const tabs: { key: Tab; label: string }[] = [
  { key: "overview", label: "Overview" },
  { key: "pods", label: "Pods" },
  { key: "metrics", label: "Metrics" },
  { key: "yaml", label: "YAML" },
];

const podColumns: DataTableColumn<NodeScheduledPodRow>[] = [
  {
    header: "Name",
    width: "2fr",
    render: (r) => <span className="font-mono text-[12.5px]">{r.name}</span>,
  },
  {
    header: "Namespace",
    width: "1.2fr",
    render: (r) => (
      <span className="font-mono text-xs text-text-muted">{r.namespace}</span>
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

// A static export can't pre-render a page per node name (unknowable at
// build time) — this route is a single static page, `/nodes/detail`, that
// reads which node to show from `?name=` instead of a `[name]` route
// segment. See AGENTS.md's "Rendering model".
export default function NodeDetailPageRoute() {
  return (
    <Suspense
      fallback={<div className="p-8 text-sm text-text-muted">Loading…</div>}
    >
      <NodeDetailPage />
    </Suspense>
  );
}

function NodeDetailPage() {
  const searchParams = useSearchParams();
  const name = searchParams.get("name") ?? "";
  const router = useRouter();
  const { data: node, isLoading, error, mutate } = useNode(name);
  const [tab, setTab] = useState<Tab>("overview");
  const [toast, setToast] = useState<ToastState>(null);
  const [busy, setBusy] = useState(false);
  const [drainOpen, setDrainOpen] = useState(false);
  const [metaKind, setMetaKind] = useState<"labels" | "taints" | null>(null);
  const [cordonOpen, setCordonOpen] = useState(false);
  const [deleteOpen, setDeleteOpen] = useState(false);
  const [deleteError, setDeleteError] = useState<string | null>(null);

  async function toggleCordon() {
    if (!node) return;
    setBusy(true);
    try {
      if (node.unschedulable) {
        await uncordonNode(node.name);
        setToast({ type: "success", message: "Node is schedulable again." });
      } else {
        await cordonNode(node.name);
        setToast({
          type: "success",
          message: "Node cordoned — no new pods will be scheduled on it.",
        });
      }
      setCordonOpen(false);
      await mutate();
    } catch (err) {
      setToast({ type: "error", message: friendlyErrorMessage(err) });
      setCordonOpen(false);
    } finally {
      setBusy(false);
    }
  }

  async function removeNode() {
    if (!node) return;
    setBusy(true);
    setDeleteError(null);
    try {
      await deleteNode(node.name);
      router.push("/nodes");
    } catch (err) {
      setDeleteError(friendlyErrorMessage(err));
      setBusy(false);
    }
  }

  if (error) {
    return (
      <div className="p-8 text-sm text-err">
        Failed to load node: {friendlyErrorMessage(error)}
      </div>
    );
  }

  if (isLoading) {
    return <div className="p-8 text-sm text-text-muted">Loading…</div>;
  }

  if (!node) {
    return (
      <div className="p-8 text-sm text-text-muted">
        Node &ldquo;{name}&rdquo; not found.
      </div>
    );
  }

  return (
    <div className="flex flex-col gap-[18px] px-8 py-7">
      <div className="flex items-start justify-between">
        <div>
          <div className="font-mono text-xs text-text-faint">
            <Link href="/nodes">Nodes</Link>
          </div>
          <div className="mt-1.5 flex items-center gap-2.5">
            <span className="font-mono text-lg font-semibold">{node.name}</span>
            <span className="flex items-center gap-1.5 rounded-full border border-border bg-surface px-2.5 py-[3px] text-xs text-text-muted">
              <StatusDot color={node.statusColor} />
              {node.status}
            </span>
            {node.unschedulable && (
              <span className="rounded-full border border-warn px-2.5 py-[3px] text-xs text-warn">
                Cordoned
              </span>
            )}
          </div>
        </div>
        <ActionBar>
          <ActionButton
            icon={node.unschedulable ? <PlayIcon /> : <PauseIcon />}
            label={node.unschedulable ? "Uncordon" : "Cordon"}
            hint={
              node.unschedulable
                ? "Let new pods be scheduled on this node again"
                : "Stop new pods from landing here; running pods stay"
            }
            onClick={() =>
              node.unschedulable ? void toggleCordon() : setCordonOpen(true)
            }
          />
          <ActionButton
            icon={<DrainIcon />}
            label="Drain"
            hint="Cordon, then move the pods to other nodes"
            onClick={() => setDrainOpen(true)}
          />
          <ActionButton
            icon={<TagIcon />}
            label="Edit labels"
            hint="Add, change or remove labels"
            onClick={() => setMetaKind("labels")}
          />
          <ActionButton
            icon={<ShieldIcon />}
            label="Edit taints"
            hint="Keep pods off this node unless they tolerate it"
            onClick={() => setMetaKind("taints")}
          />
          <ActionButton
            icon={<TrashIcon />}
            label="Delete node"
            hint="Remove the Node object — the machine itself is untouched"
            danger
            onClick={() => {
              setDeleteError(null);
              setDeleteOpen(true);
            }}
          />
        </ActionBar>
      </div>

      <UnderlineTabs items={tabs} active={tab} onChange={setTab} />

      {tab === "overview" && (
        <div className="flex max-w-3xl flex-col gap-4">
          <div className="rounded-[10px] border border-border bg-surface p-5">
            <div className="mb-3.5 text-[13px] font-semibold">Metadata</div>
            <div className="flex flex-col gap-2.5">
              <Row label="Roles" value={node.roles} />
              <Row label="Kubelet version" value={node.kubeletVersion} />
              <Row label="OS" value={node.os} />
              <Row label="Age" value={node.age} />
            </div>
          </div>

          <div className="rounded-[10px] border border-border bg-surface p-5">
            <div className="mb-3.5 text-[13px] font-semibold">
              Capacity / Allocatable
            </div>
            <div className="grid grid-cols-3 gap-4 text-xs">
              <div />
              <div className="text-text-muted">Capacity</div>
              <div className="text-text-muted">Allocatable</div>
              <div className="text-text-muted">CPU</div>
              <div className="font-mono">{node.capacity.cpu}</div>
              <div className="font-mono">{node.allocatable.cpu}</div>
              <div className="text-text-muted">Memory</div>
              <div className="font-mono">{node.capacity.memory}</div>
              <div className="font-mono">{node.allocatable.memory}</div>
              <div className="text-text-muted">Pods</div>
              <div className="font-mono">{node.capacity.pods}</div>
              <div className="font-mono">{node.allocatable.pods}</div>
            </div>
            {node.cpu && node.memory && (
              <div className="mt-4 flex flex-col gap-3 border-t border-border pt-4">
                <UsageRow
                  label="CPU used"
                  usage={node.cpu}
                  color="var(--accent)"
                />
                <UsageRow
                  label="Memory used"
                  usage={node.memory}
                  color="var(--warn)"
                />
              </div>
            )}
          </div>

          <div className="rounded-[10px] border border-border bg-surface p-5">
            <div className="mb-3.5 text-[13px] font-semibold">Conditions</div>
            <div className="flex flex-col gap-2">
              {node.conditions.map((c) => (
                <div
                  key={c.type}
                  className="flex items-start justify-between gap-4 text-xs"
                >
                  <span className="font-mono">{c.type}</span>
                  <span className="text-right text-text-muted">
                    {c.status}
                    {c.reason && ` · ${c.reason}`}
                  </span>
                </div>
              ))}
            </div>
          </div>

          <div className="rounded-[10px] border border-border bg-surface p-5">
            <div className="mb-3.5 text-[13px] font-semibold">Taints</div>
            {node.taints.length === 0 ? (
              <div className="text-xs text-text-faint">No taints.</div>
            ) : (
              <div className="flex flex-wrap gap-1.5">
                {node.taints.map((t, i) => (
                  <span
                    key={i}
                    className="rounded-md border border-border bg-surface-2 px-2 py-1 font-mono text-[11px]"
                  >
                    {t.key}
                    {t.value && `=${t.value}`}:{t.effect}
                  </span>
                ))}
              </div>
            )}
          </div>

          <div className="rounded-[10px] border border-border bg-surface p-5">
            <div className="mb-2 text-xs text-text-muted">Labels</div>
            <div className="flex flex-wrap gap-1.5">
              {Object.entries(node.labels).map(([k, v]) => (
                <span
                  key={k}
                  className="rounded-md border border-border bg-surface-2 px-2 py-1 font-mono text-[11px]"
                >
                  {k}={v}
                </span>
              ))}
            </div>
          </div>
        </div>
      )}

      {tab === "pods" && (
        <DataTable
          columns={podColumns}
          rows={node.pods}
          rowKey={(r) => `${r.namespace}/${r.name}`}
          rowHref={(r) =>
            `/workloads/pods/detail?namespace=${encodeURIComponent(r.namespace)}&name=${encodeURIComponent(r.name)}`
          }
          emptyLabel="No pods scheduled on this node."
        />
      )}

      {tab === "metrics" && (
        <MetricsPanel resourceType="nodes" namespace="" name={name} />
      )}

      {tab === "yaml" && (
        <YamlPanel yaml={node.yaml} onApplied={() => mutate()} />
      )}

      <ConfirmModal
        open={cordonOpen}
        title="Cordon this node?"
        message={`No new pods will be scheduled on "${node.name}". Pods already running keep running — use Drain to move them.`}
        confirmLabel="Cordon"
        busy={busy}
        onConfirm={() => void toggleCordon()}
        onCancel={() => setCordonOpen(false)}
      />
      {drainOpen && (
        <DrainModal
          node={node.name}
          onClose={() => setDrainOpen(false)}
          onDone={() => void mutate()}
        />
      )}
      {metaKind && (
        <NodeMetaModal
          node={node.name}
          kind={metaKind}
          labels={node.labels}
          taints={node.taints}
          onClose={() => setMetaKind(null)}
          onSaved={() => {
            setToast({ type: "success", message: "Saved." });
            void mutate();
          }}
        />
      )}
      {deleteOpen && (
        <TypeToConfirmModal
          title="Delete node?"
          message={`This removes the Node object "${node.name}" from the cluster. The machine and its kubelet are untouched — a running kubelet registers the node again. Use it for a node that is already gone or retired; drain it first.`}
          expected={node.name}
          confirmLabel="Delete node"
          busy={busy}
          error={deleteError}
          onConfirm={() => void removeNode()}
          onCancel={() => setDeleteOpen(false)}
        />
      )}
      <Toast toast={toast} onDismiss={() => setToast(null)} />
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

function UsageRow({
  label,
  usage,
  color,
}: {
  label: string;
  usage: { used: string; limit: string; pct: number };
  color: string;
}) {
  return (
    <div>
      <div className="mb-1.5 flex items-center justify-between">
        <span className="text-xs text-text-muted">{label}</span>
        <span className="font-mono text-xs text-text-muted">
          {usage.used} / {usage.limit}
        </span>
      </div>
      <div className="h-2 overflow-hidden rounded bg-surface-2">
        <div
          className="h-full"
          style={{ width: `${usage.pct}%`, background: color }}
        />
      </div>
    </div>
  );
}
