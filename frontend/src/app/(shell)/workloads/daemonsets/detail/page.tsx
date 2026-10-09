"use client";

import Link from "next/link";
import { useSearchParams } from "next/navigation";
import { Suspense, useState } from "react";
import { useDaemonSet } from "@/lib/hooks/useWorkloads";
import { friendlyErrorMessage } from "@/lib/api/errors";
import { StatusDot } from "@/components/ui/StatusDot";
import { WorkloadOverview } from "@/components/workloads/WorkloadOverview";
import { DeleteWorkloadAction } from "@/components/workloads/DeleteWorkloadAction";
import { ActionBar } from "@/components/ui/ActionBar";
import { UnderlineTabs } from "@/components/ui/Tabs";
import { YamlPanel } from "@/components/editor/YamlPanel";
import { EventRow } from "@/components/ui/EventRow";

type Tab = "overview" | "events" | "yaml";

const tabs: { key: Tab; label: string }[] = [
  { key: "overview", label: "Overview" },
  { key: "events", label: "Events" },
  { key: "yaml", label: "YAML" },
];

// A static export can't pre-render a page per daemonset name (unknowable at
// build time) — this route is a single static page,
// `/workloads/daemonsets/detail`, that reads which daemonset to show from
// `?namespace=&name=` instead of a `[namespace]/[name]` route segment. See
// AGENTS.md's "Rendering model".
export default function DaemonSetDetailPageRoute() {
  return (
    <Suspense
      fallback={<div className="p-8 text-sm text-text-muted">Loading…</div>}
    >
      <DaemonSetDetailPage />
    </Suspense>
  );
}

function DaemonSetDetailPage() {
  const searchParams = useSearchParams();
  const namespace = searchParams.get("namespace") ?? "";
  const name = searchParams.get("name") ?? "";
  const { data: ds, isLoading, error, mutate } = useDaemonSet(namespace, name);
  const [tab, setTab] = useState<Tab>("overview");

  if (error) {
    return (
      <div className="p-8 text-sm text-err">
        Failed to load daemonset: {friendlyErrorMessage(error)}
      </div>
    );
  }

  if (isLoading) {
    return <div className="p-8 text-sm text-text-muted">Loading…</div>;
  }

  if (!ds) {
    return (
      <div className="p-8 text-sm text-text-muted">
        DaemonSet &ldquo;{namespace}/{name}&rdquo; not found.
      </div>
    );
  }

  return (
    <div className="flex flex-col gap-[18px] px-8 py-7">
      <div className="flex items-start justify-between">
        <div>
          <div className="font-mono text-xs text-text-faint">
            <Link href="/workloads">Workloads</Link> /{" "}
            <Link href="/workloads?tab=daemonsets">DaemonSets</Link>
          </div>
          <div className="mt-1.5 flex items-center gap-2.5">
            <span className="font-mono text-lg font-semibold">{ds.name}</span>
            <span className="flex items-center gap-1.5 rounded-full border border-border bg-surface px-2.5 py-[3px] text-xs text-text-muted">
              <StatusDot color={ds.statusColor} />
              {ds.status}
            </span>
          </div>
        </div>
        <ActionBar>
          <DeleteWorkloadAction
            kind="daemonsets"
            namespace={namespace}
            name={name}
          />
        </ActionBar>
      </div>

      <UnderlineTabs items={tabs} active={tab} onChange={setTab} />

      {tab === "overview" && (
        <WorkloadOverview
          resourceType="daemonsets"
          namespace={namespace}
          name={name}
          pods={ds.pods}
          emptyLabel="No pods yet — no node matches this DaemonSet."
        >
          <div className="rounded-[10px] border border-border bg-surface p-5">
            <div className="mb-3.5 text-[13px] font-semibold">Metadata</div>
            <div className="flex flex-col gap-2.5">
              <Row label="Namespace" value={ds.namespace} />
              <Row label="Ready" value={ds.ready} />
              <Row label="Desired" value={String(ds.desired)} />
              <Row label="Current" value={String(ds.current)} />
              <Row label="Update strategy" value={ds.updateStrategy} />
              <Row label="Image" value={ds.image} />
              <Row label="Age" value={ds.age} />
            </div>
            <div className="mt-4 border-t border-border pt-4">
              <div className="mb-2 text-xs text-text-muted">Labels</div>
              <div className="flex flex-wrap gap-1.5">
                {Object.entries(ds.labels).map(([k, v]) => (
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
        </WorkloadOverview>
      )}

      {tab === "events" && (
        <div className="flex flex-col rounded-[10px] border border-border bg-surface p-5">
          {ds.events.map((e, i) => (
            <EventRow key={i} {...e} />
          ))}
        </div>
      )}

      {tab === "yaml" && (
        <YamlPanel yaml={ds.yaml} onApplied={() => mutate()} />
      )}
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
