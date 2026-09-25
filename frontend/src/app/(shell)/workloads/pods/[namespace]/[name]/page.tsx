"use client";

import Link from "next/link";
import { useParams } from "next/navigation";
import { useState } from "react";
import { usePod } from "@/lib/hooks/useWorkloads";
import { StatusDot } from "@/components/ui/StatusDot";
import { UnderlineTabs } from "@/components/ui/Tabs";
import { CodeBlock } from "@/components/ui/CodeBlock";
import { EventRow } from "@/components/ui/EventRow";
import { TerminalPane } from "@/components/ui/TerminalPane";
import { LogsTab } from "./LogsTab";

type Tab = "overview" | "logs" | "terminal" | "events" | "yaml";

const tabs: { key: Tab; label: string }[] = [
  { key: "overview", label: "Overview" },
  { key: "logs", label: "Logs" },
  { key: "terminal", label: "Terminal" },
  { key: "events", label: "Events" },
  { key: "yaml", label: "YAML" },
];

export default function PodDetailPage() {
  const { namespace, name } = useParams<{ namespace: string; name: string }>();
  const podNamespace = decodeURIComponent(namespace);
  const podName = decodeURIComponent(name);
  const { data: pod, isLoading, error } = usePod(podNamespace, podName);
  const [tab, setTab] = useState<Tab>("overview");

  if (error) {
    return (
      <div className="p-8 text-sm text-err">Failed to load pod: {error.message}</div>
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
            <Link href="/workloads">Pods</Link>
          </div>
          <div className="mt-1.5 flex items-center gap-2.5">
            <span className="font-mono text-lg font-semibold">{pod.name}</span>
            <span className="flex items-center gap-1.5 rounded-full border border-border bg-surface px-2.5 py-[3px] text-xs text-text-muted">
              <StatusDot color={pod.statusColor} />
              {pod.status}
            </span>
          </div>
        </div>
        <div className="flex shrink-0 gap-2.5">
          <button
            type="button"
            title="TODO: wire up to backend — proposes a GitOps PR, does not call the Kubernetes API directly"
            className="rounded-lg border border-border bg-surface px-3.5 py-2 text-[12.5px]"
          >
            Restart
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
              <div className="mb-2.5 text-[13px] font-semibold">Containers</div>
              {pod.containers.map((c) => (
                <div key={c.name} className="flex items-center justify-between">
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
      )}

      {tab === "logs" && (
        <LogsTab
          namespace={pod.namespace}
          name={pod.name}
          containers={pod.containers}
        />
      )}

      {tab === "terminal" && (
        <TerminalPane
          connectedTo={pod.name}
          container={pod.containers[0]?.name ?? "—"}
          seedLines={[
            { text: "$ ls /app", color: "var(--ok)" },
            { text: "bin  config  logs  node_modules" },
          ]}
        />
      )}

      {tab === "events" && (
        <div className="flex flex-col rounded-[10px] border border-border bg-surface p-5">
          {pod.events.map((e, i) => (
            <EventRow key={i} {...e} />
          ))}
        </div>
      )}

      {tab === "yaml" && <CodeBlock code={pod.yaml} />}
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
