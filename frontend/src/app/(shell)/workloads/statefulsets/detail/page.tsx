"use client";

import Link from "next/link";
import { useSearchParams } from "next/navigation";
import { Suspense, useState } from "react";
import { useStatefulSet } from "@/lib/hooks/useWorkloads";
import { friendlyErrorMessage } from "@/lib/api/errors";
import { StatusDot } from "@/components/ui/StatusDot";
import { UnderlineTabs } from "@/components/ui/Tabs";
import { CodeBlock } from "@/components/ui/CodeBlock";
import { EventRow } from "@/components/ui/EventRow";
import { ActionBar, ActionButton } from "@/components/ui/ActionBar";
import { ScaleModal } from "@/components/ui/ScaleModal";
import { Toast, type ToastState } from "@/components/ui/Toast";
import { ScaleIcon } from "@/components/icons";
import { scaleStatefulSet } from "@/lib/api/statefulsetActions";

type Tab = "overview" | "events" | "yaml";

const tabs: { key: Tab; label: string }[] = [
  { key: "overview", label: "Overview" },
  { key: "events", label: "Events" },
  { key: "yaml", label: "YAML" },
];

// A static export can't pre-render a page per statefulset name (unknowable
// at build time) — this route is a single static page,
// `/workloads/statefulsets/detail`, that reads which statefulset to show
// from `?namespace=&name=` instead of a `[namespace]/[name]` route segment.
// See AGENTS.md's "Rendering model".
export default function StatefulSetDetailPageRoute() {
  return (
    <Suspense
      fallback={<div className="p-8 text-sm text-text-muted">Loading…</div>}
    >
      <StatefulSetDetailPage />
    </Suspense>
  );
}

function StatefulSetDetailPage() {
  const searchParams = useSearchParams();
  const namespace = searchParams.get("namespace") ?? "";
  const name = searchParams.get("name") ?? "";
  const {
    data: sts,
    isLoading,
    error,
    mutate,
  } = useStatefulSet(namespace, name);
  const [tab, setTab] = useState<Tab>("overview");
  const [scaleOpen, setScaleOpen] = useState(false);
  const [scaleBusy, setScaleBusy] = useState(false);
  const [toast, setToast] = useState<ToastState>(null);

  async function handleOpenScale() {
    // Refetch *before* mounting the modal — see the Deployment detail
    // page's identical comment for why.
    await mutate();
    setScaleOpen(true);
  }

  async function handleScale(replicas: number) {
    setScaleBusy(true);
    try {
      await scaleStatefulSet(namespace, name, replicas);
      setToast({
        type: "success",
        message: `Scaled to ${replicas} replica${replicas === 1 ? "" : "s"}.`,
      });
      setScaleOpen(false);
      await mutate();
    } catch (err) {
      setToast({ type: "error", message: friendlyErrorMessage(err) });
    } finally {
      setScaleBusy(false);
    }
  }

  if (error) {
    return (
      <div className="p-8 text-sm text-err">
        Failed to load statefulset: {friendlyErrorMessage(error)}
      </div>
    );
  }

  if (isLoading) {
    return <div className="p-8 text-sm text-text-muted">Loading…</div>;
  }

  if (!sts) {
    return (
      <div className="p-8 text-sm text-text-muted">
        StatefulSet &ldquo;{namespace}/{name}&rdquo; not found.
      </div>
    );
  }

  return (
    <div className="flex flex-col gap-[18px] px-8 py-7">
      <div className="flex items-start justify-between">
        <div>
          <div className="font-mono text-xs text-text-faint">
            <Link href="/workloads">Workloads</Link> /{" "}
            <Link href="/workloads">StatefulSets</Link>
          </div>
          <div className="mt-1.5 flex items-center gap-2.5">
            <span className="font-mono text-lg font-semibold">{sts.name}</span>
            <span className="flex items-center gap-1.5 rounded-full border border-border bg-surface px-2.5 py-[3px] text-xs text-text-muted">
              <StatusDot color={sts.statusColor} />
              {sts.status}
            </span>
          </div>
        </div>
        <ActionBar>
          <ActionButton
            icon={<ScaleIcon />}
            label="Scale"
            onClick={handleOpenScale}
          />
        </ActionBar>
      </div>

      <UnderlineTabs items={tabs} active={tab} onChange={setTab} />

      {tab === "overview" && (
        <div className="rounded-[10px] border border-border bg-surface p-5">
          <div className="mb-3.5 text-[13px] font-semibold">Metadata</div>
          <div className="flex flex-col gap-2.5">
            <Row label="Namespace" value={sts.namespace} />
            <Row label="Ready" value={sts.ready} />
            <Row label="Service name" value={sts.serviceName} />
            <Row label="Update strategy" value={sts.updateStrategy} />
            <Row label="Image" value={sts.image} />
            <Row label="Age" value={sts.age} />
          </div>
          <div className="mt-4 border-t border-border pt-4">
            <div className="mb-2 text-xs text-text-muted">Labels</div>
            <div className="flex flex-wrap gap-1.5">
              {Object.entries(sts.labels).map(([k, v]) => (
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

      {tab === "events" && (
        <div className="flex flex-col rounded-[10px] border border-border bg-surface p-5">
          {sts.events.map((e, i) => (
            <EventRow key={i} {...e} />
          ))}
        </div>
      )}

      {tab === "yaml" && <CodeBlock code={sts.yaml} />}

      {scaleOpen && (
        <ScaleModal
          resourceName={name}
          currentReplicas={sts.desiredReplicas}
          busy={scaleBusy}
          orderNote="Scaling down a StatefulSet terminates pods one at a time, in descending ordinal order — unlike a Deployment."
          onApply={handleScale}
          onCancel={() => setScaleOpen(false)}
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
