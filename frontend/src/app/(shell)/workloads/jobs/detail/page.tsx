"use client";

import Link from "next/link";
import { useSearchParams } from "next/navigation";
import { Suspense, useState } from "react";
import { useJob } from "@/lib/hooks/useWorkloads";
import { StatusDot } from "@/components/ui/StatusDot";
import { UnderlineTabs } from "@/components/ui/Tabs";
import { CodeBlock } from "@/components/ui/CodeBlock";
import { EventRow } from "@/components/ui/EventRow";

type Tab = "overview" | "events" | "yaml";

const tabs: { key: Tab; label: string }[] = [
  { key: "overview", label: "Overview" },
  { key: "events", label: "Events" },
  { key: "yaml", label: "YAML" },
];

// A static export can't pre-render a page per job name (unknowable at build
// time) — this route is a single static page, `/workloads/jobs/detail`,
// that reads which job to show from `?namespace=&name=` instead of a
// `[namespace]/[name]` route segment. See AGENTS.md's "Rendering model".
// Scoped to plain batch/v1 Jobs only — a CronJob isn't covered here.
export default function JobDetailPageRoute() {
  return (
    <Suspense
      fallback={<div className="p-8 text-sm text-text-muted">Loading…</div>}
    >
      <JobDetailPage />
    </Suspense>
  );
}

function JobDetailPage() {
  const searchParams = useSearchParams();
  const namespace = searchParams.get("namespace") ?? "";
  const name = searchParams.get("name") ?? "";
  const { data: job, isLoading, error } = useJob(namespace, name);
  const [tab, setTab] = useState<Tab>("overview");

  if (error) {
    return (
      <div className="p-8 text-sm text-err">
        Failed to load job: {error.message}
      </div>
    );
  }

  if (isLoading) {
    return <div className="p-8 text-sm text-text-muted">Loading…</div>;
  }

  if (!job) {
    return (
      <div className="p-8 text-sm text-text-muted">
        Job &ldquo;{namespace}/{name}&rdquo; not found.
      </div>
    );
  }

  return (
    <div className="flex flex-col gap-[18px] px-8 py-7">
      <div className="flex items-start justify-between">
        <div>
          <div className="font-mono text-xs text-text-faint">
            <Link href="/workloads">Workloads</Link> /{" "}
            <Link href="/workloads">Jobs</Link>
          </div>
          <div className="mt-1.5 flex items-center gap-2.5">
            <span className="font-mono text-lg font-semibold">{job.name}</span>
            <span className="flex items-center gap-1.5 rounded-full border border-border bg-surface px-2.5 py-[3px] text-xs text-text-muted">
              <StatusDot color={job.statusColor} />
              {job.status}
            </span>
          </div>
        </div>
      </div>

      <UnderlineTabs items={tabs} active={tab} onChange={setTab} />

      {tab === "overview" && (
        <div className="rounded-[10px] border border-border bg-surface p-5">
          <div className="mb-3.5 text-[13px] font-semibold">Metadata</div>
          <div className="flex flex-col gap-2.5">
            <Row label="Namespace" value={job.namespace} />
            <Row label="Completions" value={job.completions} />
            <Row label="Active" value={String(job.active)} />
            <Row label="Failed" value={String(job.failed)} />
            <Row label="Start time" value={job.startTime || "—"} />
            <Row label="Completion time" value={job.completionTime || "—"} />
            <Row label="Image" value={job.image} />
            <Row label="Age" value={job.age} />
          </div>
          <div className="mt-4 border-t border-border pt-4">
            <div className="mb-2 text-xs text-text-muted">Labels</div>
            <div className="flex flex-wrap gap-1.5">
              {Object.entries(job.labels).map(([k, v]) => (
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
          {job.events.map((e, i) => (
            <EventRow key={i} {...e} />
          ))}
        </div>
      )}

      {tab === "yaml" && <CodeBlock code={job.yaml} />}
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
