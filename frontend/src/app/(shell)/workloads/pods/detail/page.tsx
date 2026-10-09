"use client";

import Link from "next/link";
import { useSearchParams, useRouter } from "next/navigation";
import { Suspense, useState } from "react";
import { usePod } from "@/lib/hooks/useWorkloads";
import { friendlyErrorMessage } from "@/lib/api/errors";
import { StatusDot } from "@/components/ui/StatusDot";
import { UnderlineTabs } from "@/components/ui/Tabs";
import { MetricsPanel } from "@/components/metrics/MetricsPanel";
import { YamlPanel } from "@/components/editor/YamlPanel";
import { EventRow } from "@/components/ui/EventRow";
import { ActionBar, ActionButton } from "@/components/ui/ActionBar";
import { ConfirmModal } from "@/components/ui/ConfirmModal";
import { Toast, type ToastState } from "@/components/ui/Toast";
import { PortForwardIcon, RestartIcon, TrashIcon } from "@/components/icons";
import { ForwardPortModal } from "@/components/ui/ForwardPortModal";
import { deletePod } from "@/lib/api/podActions";
import { LogsTab } from "./LogsTab";
import { TerminalTab } from "./TerminalTab";

type Tab = "overview" | "logs" | "terminal" | "events" | "yaml";

const tabs: { key: Tab; label: string }[] = [
  { key: "overview", label: "Overview" },
  { key: "logs", label: "Logs" },
  { key: "terminal", label: "Terminal" },
  { key: "events", label: "Events" },
  { key: "yaml", label: "YAML" },
];

// A static export can't pre-render a page per pod name (unknowable at
// build time) — this route is a single static page,
// `/workloads/pods/detail`, that reads which pod to show from
// `?namespace=&name=` instead of a `[namespace]/[name]` route segment. See
// AGENTS.md's "Rendering model".
export default function PodDetailPageRoute() {
  return (
    <Suspense
      fallback={<div className="p-8 text-sm text-text-muted">Loading…</div>}
    >
      <PodDetailPage />
    </Suspense>
  );
}

function PodDetailPage() {
  const router = useRouter();
  const searchParams = useSearchParams();
  const podNamespace = searchParams.get("namespace") ?? "";
  const podName = searchParams.get("name") ?? "";
  const { data: pod, isLoading, error, mutate } = usePod(podNamespace, podName);
  const [tab, setTab] = useState<Tab>("overview");
  const [confirmOpen, setConfirmOpen] = useState(false);
  const [forwardOpen, setForwardOpen] = useState(false);
  const [busy, setBusy] = useState(false);
  const [toast, setToast] = useState<ToastState>(null);

  async function handleDelete() {
    setBusy(true);
    try {
      await deletePod(podNamespace, podName);
      setToast({
        type: "success",
        message: pod?.hasController
          ? "Pod deleted — its controller will recreate it."
          : "Pod deleted.",
      });
      router.push("/workloads");
    } catch (err) {
      setToast({ type: "error", message: friendlyErrorMessage(err) });
      setBusy(false);
    }
  }

  if (error) {
    return (
      <div className="p-8 text-sm text-err">
        Failed to load pod: {friendlyErrorMessage(error)}
      </div>
    );
  }

  if (isLoading) {
    return <div className="p-8 text-sm text-text-muted">Loading…</div>;
  }

  if (!pod) {
    return (
      <div className="p-8 text-sm text-text-muted">
        Pod &ldquo;{podNamespace}/{podName}&rdquo; not found.
      </div>
    );
  }

  return (
    <div className="flex flex-col gap-[18px] px-8 py-7">
      <div className="flex items-start justify-between">
        <div>
          <div className="font-mono text-xs text-text-faint">
            <Link href="/workloads">Workloads</Link> /{" "}
            <Link href="/workloads?tab=pods">Pods</Link>
          </div>
          <div className="mt-1.5 flex items-center gap-2.5">
            <span className="font-mono text-lg font-semibold">{pod.name}</span>
            <span className="flex items-center gap-1.5 rounded-full border border-border bg-surface px-2.5 py-[3px] text-xs text-text-muted">
              <StatusDot color={pod.statusColor} />
              {pod.status}
            </span>
          </div>
        </div>
        <ActionBar>
          <ActionButton
            icon={<PortForwardIcon />}
            label="Forward port"
            hint="Open a port of this pod in a new tab"
            onClick={() => setForwardOpen(true)}
          />
          <ActionButton
            icon={pod.hasController ? <RestartIcon /> : <TrashIcon />}
            label={pod.hasController ? "Restart" : "Delete"}
            hint={
              pod.hasController
                ? "Delete the pod; its controller recreates it"
                : "Delete the pod — it has no controller, so it stays gone"
            }
            danger={!pod.hasController}
            onClick={() => setConfirmOpen(true)}
          />
        </ActionBar>
      </div>

      <UnderlineTabs items={tabs} active={tab} onChange={setTab} />

      {tab === "overview" && (
        <div className="flex flex-col gap-5">
          <div className="grid grid-cols-[1.3fr_1fr] gap-5">
            <div className="rounded-[10px] border border-border bg-surface p-5">
              <div className="mb-3.5 text-[13px] font-semibold">Metadata</div>
              <div className="flex flex-col gap-2.5">
                <Row label="Namespace" value={pod.namespace} />
                <Row label="Node" value={pod.node} />
                <Row label="Pod IP" value={pod.podIp} />
                <Row label="QoS class" value={pod.qosClass} />
                <Row label="Restarts" value={String(pod.restarts)} />
                <Row label="Image" value={pod.image} />
                <Row label="Age" value={pod.age} />
              </div>
              <div className="mt-4 border-t border-border pt-4">
                <div className="mb-2 text-xs text-text-muted">Labels</div>
                <div className="flex flex-wrap gap-1.5">
                  {Object.entries(pod.labels).map(([k, v]) => (
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

            <div className="flex flex-col gap-4">
              <div className="rounded-[10px] border border-border bg-surface p-[18px]">
                <div className="mb-2 flex items-center justify-between">
                  <span className="text-[13px] font-semibold">CPU</span>
                  <span className="font-mono text-xs text-text-muted">
                    {pod.cpu.used} / {pod.cpu.limit}
                  </span>
                </div>
                <div className="h-2 overflow-hidden rounded bg-surface-2">
                  <div
                    className="h-full bg-accent"
                    style={{ width: `${pod.cpu.pct}%` }}
                  />
                </div>
              </div>
              <div className="rounded-[10px] border border-border bg-surface p-[18px]">
                <div className="mb-2 flex items-center justify-between">
                  <span className="text-[13px] font-semibold">Memory</span>
                  <span className="font-mono text-xs text-text-muted">
                    {pod.memory.used} / {pod.memory.limit}
                  </span>
                </div>
                <div className="h-2 overflow-hidden rounded bg-surface-2">
                  <div
                    className="h-full bg-warn"
                    style={{ width: `${pod.memory.pct}%` }}
                  />
                </div>
              </div>
              <div className="rounded-[10px] border border-border bg-surface p-[18px]">
                <div className="mb-2.5 text-[13px] font-semibold">
                  Containers
                </div>
                {pod.containers.map((c) => (
                  <div
                    key={c.name}
                    className="flex items-center justify-between"
                  >
                    <span className="flex items-center gap-2 text-[12.5px]">
                      <StatusDot color={c.statusColor} size={7} />
                      {c.name}
                    </span>
                    <span className="font-mono text-[11px] text-text-muted">
                      {c.restarts} restarts
                    </span>
                  </div>
                ))}
              </div>
            </div>
          </div>
          <MetricsPanel
            resourceType="pods"
            namespace={podNamespace}
            name={podName}
          />
        </div>
      )}

      {tab === "logs" && (
        <LogsTab
          namespace={pod.namespace}
          name={pod.name}
          containers={pod.containers}
        />
      )}

      {tab === "terminal" && (
        <TerminalTab
          namespace={pod.namespace}
          name={pod.name}
          containers={pod.containers}
        />
      )}

      {tab === "events" && (
        <div className="flex flex-col rounded-[10px] border border-border bg-surface p-5">
          {pod.events.map((e, i) => (
            <EventRow key={i} {...e} />
          ))}
        </div>
      )}

      {tab === "yaml" && (
        <YamlPanel yaml={pod.yaml} onApplied={() => mutate()} />
      )}

      {forwardOpen && (
        <ForwardPortModal
          namespace={pod.namespace}
          pod={pod.name}
          ports={pod.ports}
          onClose={() => setForwardOpen(false)}
        />
      )}

      <ConfirmModal
        open={confirmOpen}
        title={pod.hasController ? "Restart pod?" : "Delete pod?"}
        message={
          pod.hasController
            ? `This deletes "${pod.name}" — its controller will recreate it shortly, same as a restart.`
            : `This pod has no controller — it will not be recreated. This permanently deletes "${pod.name}".`
        }
        confirmLabel={pod.hasController ? "Restart" : "Delete"}
        danger={!pod.hasController}
        busy={busy}
        onConfirm={handleDelete}
        onCancel={() => setConfirmOpen(false)}
      />
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
