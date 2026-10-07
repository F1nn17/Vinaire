"use client";

import { useState } from "react";
import { ApiError } from "@/lib/api/http";
import { friendlyErrorMessage } from "@/lib/api/errors";
import { useAudit, type AuditEntry, type AuditFilter } from "@/lib/api/audit";
import { DataTable, type DataTableColumn } from "@/components/ui/DataTable";
import { ListSkeleton } from "@/components/ui/Skeleton";

const ranges: { value: string; label: string }[] = [
  { value: "1h", label: "Last hour" },
  { value: "24h", label: "Last 24 hours" },
  { value: "7d", label: "Last 7 days" },
  { value: "30d", label: "Last 30 days" },
  { value: "", label: "Everything kept" },
];

const input =
  "rounded-lg border border-border bg-surface px-3 py-1.75 text-xs outline-none placeholder:text-text-faint";

function when(iso: string): string {
  const d = new Date(iso);
  return Number.isNaN(d.getTime())
    ? iso
    : d.toLocaleString([], { dateStyle: "short", timeStyle: "medium" });
}

// Actions that end in -failed / -refused are the ones worth noticing.
function actionTone(a: string): string {
  if (/-(failed|refused)$/.test(a)) return "text-err";
  if (/^(delete|drain|uninstall|remove)|-(delete|uninstall)/.test(a))
    return "text-warn";
  return "text-text";
}

const columns: DataTableColumn<AuditEntry>[] = [
  {
    header: "Time",
    width: "1.3fr",
    sortValue: (e) => new Date(e.time).getTime(),
    render: (e) => (
      <span className="font-mono text-xs text-text-muted">{when(e.time)}</span>
    ),
  },
  {
    header: "Action",
    width: "1.4fr",
    render: (e) => (
      <span className={`font-mono text-xs ${actionTone(e.action)}`}>
        {e.action}
      </span>
    ),
  },
  {
    header: "Who",
    width: "1fr",
    sortValue: (e) => e.identity || "",
    render: (e) => (
      <span className="text-xs">
        {e.identity || <span className="text-text-faint">token session</span>}
      </span>
    ),
  },
  {
    header: "Object",
    width: "1.6fr",
    sortValue: (e) => `${e.namespace}/${e.name}`,
    render: (e) => (
      <span className="font-mono text-xs">
        {e.namespace ? `${e.namespace}/` : ""}
        {e.name}
      </span>
    ),
  },
  {
    header: "Details",
    width: "2fr",
    sortValue: (e) => (e.detail ? JSON.stringify(e.detail) : ""),
    render: (e) =>
      e.detail && Object.keys(e.detail).length > 0 ? (
        <span
          className="block truncate font-mono text-[11px] text-text-muted"
          title={JSON.stringify(e.detail, null, 2)}
        >
          {Object.entries(e.detail)
            .map(
              ([k, v]) =>
                `${k}=${typeof v === "string" ? v : JSON.stringify(v)}`,
            )
            .join("  ")}
        </span>
      ) : (
        <span className="text-text-faint">—</span>
      ),
  },
  {
    header: "From",
    width: "1fr",
    render: (e) => (
      <span className="font-mono text-[11px] text-text-faint">
        {e.remoteAddr}
      </span>
    ),
  },
];

export default function AuditPage() {
  const [filter, setFilter] = useState<AuditFilter>({
    action: "",
    identity: "",
    namespace: "",
    name: "",
    q: "",
    since: "24h",
  });
  const [auto, setAuto] = useState(true);
  const { data, error, isLoading, isValidating, mutate } = useAudit(
    filter,
    auto ? 15000 : 0,
  );
  const set = (k: keyof AuditFilter, v: string) =>
    setFilter((f) => ({ ...f, [k]: v }));

  const denied = error instanceof ApiError && error.status === 403;

  return (
    <div className="flex flex-col gap-[18px] px-8 py-7">
      <div>
        <div className="text-[15px] font-semibold">Audit log</div>
        <div className="mt-1 max-w-3xl text-xs text-text-muted">
          Every change Vinaire makes — who, what, which object, when. Values of
          Secrets and credentials are never written here. Reading it needs the
          permission to <span className="font-mono">get audit.vinaire.io</span>{" "}
          (the chart&apos;s <span className="font-mono">-admin</span> role).
        </div>
      </div>

      <div className="flex flex-wrap items-center gap-2.5">
        <select
          aria-label="Action"
          value={filter.action}
          onChange={(e) => set("action", e.target.value)}
          className={input}
        >
          <option value="">Any action</option>
          {[
            ...new Set(
              [...(data?.actions ?? []), filter.action].filter(Boolean),
            ),
          ]
            .sort()
            .map((a) => (
              <option key={a} value={a}>
                {a}
              </option>
            ))}
        </select>
        <input
          aria-label="Who"
          value={filter.identity}
          onChange={(e) => set("identity", e.target.value)}
          placeholder="Who"
          className={`${input} w-36`}
        />
        <input
          aria-label="Namespace"
          value={filter.namespace}
          onChange={(e) => set("namespace", e.target.value)}
          placeholder="Namespace"
          className={`${input} w-36 font-mono`}
        />
        <input
          aria-label="Object name"
          value={filter.name}
          onChange={(e) => set("name", e.target.value)}
          placeholder="Object name"
          className={`${input} w-40 font-mono`}
        />
        <input
          aria-label="Search everything"
          value={filter.q}
          onChange={(e) => set("q", e.target.value)}
          placeholder="Search the details…"
          className={`${input} w-52`}
        />
        <select
          aria-label="Time range"
          value={filter.since}
          onChange={(e) => set("since", e.target.value)}
          className={input}
        >
          {ranges.map((r) => (
            <option key={r.value} value={r.value}>
              {r.label}
            </option>
          ))}
        </select>
        <button
          type="button"
          onClick={() => void mutate()}
          className="rounded-lg border border-border bg-surface px-3 py-1.75 text-xs text-text-muted hover:border-accent"
        >
          {isValidating ? "Refreshing…" : "Refresh"}
        </button>
        <label className="flex items-center gap-1.5 text-xs text-text-muted">
          <input
            type="checkbox"
            checked={auto}
            onChange={(e) => setAuto(e.target.checked)}
          />
          Auto-refresh
        </label>
      </div>

      {denied ? (
        <div className="max-w-2xl rounded-[10px] border border-warn/40 bg-surface p-5 text-sm text-text-muted">
          <div className="font-semibold text-text">
            You can&apos;t read the audit log.
          </div>
          <div className="mt-1.5 text-xs">
            It shows every user&apos;s activity, so it needs its own permission:{" "}
            <span className="font-mono">get</span> on{" "}
            <span className="font-mono">audit.vinaire.io</span>. Bind the
            chart&apos;s <span className="font-mono">-admin</span> role to this
            account (in addition to the viewer role).{" "}
            {error instanceof Error ? error.message : ""}
          </div>
        </div>
      ) : error ? (
        <div className="text-sm text-err">
          Failed to load the audit log: {friendlyErrorMessage(error)}
        </div>
      ) : isLoading || !data ? (
        <ListSkeleton />
      ) : !data.available ? (
        <div className="max-w-2xl rounded-[10px] border border-border bg-surface p-5 text-sm text-text-muted">
          <div className="font-semibold text-text">
            No audit file on this install.
          </div>
          <div className="mt-1.5 text-xs">
            Actions are still written to Vinaire&apos;s log output; to keep them
            here, set <span className="font-mono">AUDIT_LOG_PATH</span>. The
            Helm chart does this and keeps the file on an{" "}
            <span className="font-mono">emptyDir</span>, so it is lost when the
            pod restarts — ship the log output to your log system for a durable
            trail.
          </div>
        </div>
      ) : (
        <>
          <div className="text-xs text-text-faint">
            {(data.entries ?? []).length} shown
            {data.truncated ? " (newest 500 — narrow the filters)" : ""}
            {data.oldest
              ? ` · the log reaches back to ${when(data.oldest)}`
              : ""}
          </div>
          <DataTable
            columns={columns}
            rows={data.entries ?? []}
            rowKey={(e) =>
              `${e.time}|${e.action}|${e.namespace}|${e.name}|${e.remoteAddr}`
            }
            emptyLabel="No records match."
          />
        </>
      )}
    </div>
  );
}
