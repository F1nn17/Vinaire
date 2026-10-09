"use client";

import { useRef, useState } from "react";
import { t } from "@/lib/i18n/translate";
import {
  bulkKey,
  runBulk,
  type BulkItem,
  type BulkResult,
} from "@/lib/api/bulk";
import {
  deleteDeployment,
  restartDeployment,
} from "@/lib/api/deploymentActions";
import { deletePod } from "@/lib/api/podActions";
import {
  deleteDaemonSet,
  deleteJob,
  deleteStatefulSet,
} from "@/lib/api/workloadDelete";

// Roadmap Phase 58: the bar above a list that has rows ticked, and the window that runs the
// action. Only what already exists can be done in bulk — restart / delete a Deployment and
// delete a Pod — one request per row, each audited and decided by the caller's own RBAC.

export type BulkKind =
  "deployments" | "pods" | "statefulsets" | "daemonsets" | "jobs";
type Action = "delete" | "restart";

export function BulkBar({
  kind,
  items,
  onClear,
  onFinished,
}: {
  kind: BulkKind;
  items: BulkItem[];
  onClear: () => void;
  // called with the keys that were really acted on, so the list can refresh and
  // drop them from the selection
  onFinished: (done: string[]) => void;
}) {
  // the window keeps its own copy of what it was opened for: while it runs, the objects
  // disappear from the live list (and from the selection), and the window must not go with them
  const [open, setOpen] = useState<{
    action: Action;
    items: BulkItem[];
  } | null>(null);
  if (items.length === 0 && !open) return null;
  return (
    <div className="flex items-center gap-3 rounded-lg border border-accent/40 bg-surface-2 px-4 py-2 text-xs">
      <span className="font-semibold">
        {t("{n} selected", { n: items.length })}
      </span>
      {kind === "deployments" && (
        <button
          type="button"
          onClick={() => setOpen({ action: "restart", items })}
          className="rounded-md border border-border bg-surface px-3 py-1.5 hover:border-accent"
        >
          Restart
        </button>
      )}
      <button
        type="button"
        onClick={() => setOpen({ action: "delete", items })}
        className="rounded-md border border-err/50 bg-surface px-3 py-1.5 text-err hover:border-err"
      >
        Delete
      </button>
      <button
        type="button"
        onClick={onClear}
        className="ml-auto text-text-muted hover:text-text"
      >
        Clear selection
      </button>
      {open && (
        <BulkModal
          kind={kind}
          action={open.action}
          items={open.items}
          onClose={(done) => {
            setOpen(null);
            if (done.length > 0) onFinished(done);
          }}
        />
      )}
    </div>
  );
}

function run(kind: BulkKind, action: Action, item: BulkItem): Promise<void> {
  if (action === "restart") return restartDeployment(item.namespace, item.name);
  switch (kind) {
    case "deployments":
      return deleteDeployment(item.namespace, item.name);
    case "statefulsets":
      return deleteStatefulSet(item.namespace, item.name);
    case "daemonsets":
      return deleteDaemonSet(item.namespace, item.name);
    case "jobs":
      return deleteJob(item.namespace, item.name);
    default:
      return deletePod(item.namespace, item.name);
  }
}

const kindWord: Record<BulkKind, string> = {
  deployments: "deployments",
  pods: "pods",
  statefulsets: "statefulsets",
  daemonsets: "daemonsets",
  jobs: "jobs",
};

// what deleting this kind does, in one sentence
function deleteNote(kind: BulkKind): string {
  switch (kind) {
    case "deployments":
      return "Each deployment is deleted with its pods. This can't be undone.";
    case "statefulsets":
      return "Each StatefulSet is deleted with its pods; the volume claims it created stay. This can't be undone.";
    case "daemonsets":
      return "Each DaemonSet is deleted with its pods on every node. This can't be undone.";
    case "jobs":
      return "Each Job is deleted with its pods and their logs. This can't be undone.";
    default:
      return "A pod that belongs to a controller is recreated (a restart); a bare pod is gone for good.";
  }
}

function BulkModal({
  kind,
  action,
  items,
  onClose,
}: {
  kind: BulkKind;
  action: Action;
  items: BulkItem[];
  onClose: (succeeded: string[]) => void;
}) {
  const [phase, setPhase] = useState<"confirm" | "running" | "done">("confirm");
  const [results, setResults] = useState<BulkResult[]>([]);
  const [typed, setTyped] = useState("");
  const stop = useRef<AbortController | null>(null);
  const destructive = action === "delete";
  // deleting needs the count typed, so a reflex "Enter" can't remove a pile of objects
  const confirmed = !destructive || typed.trim() === String(items.length);
  const failed = results.filter((r) => !r.ok);
  const succeeded = results.filter((r) => r.ok).map((r) => r.key);

  async function start() {
    const ctl = new AbortController();
    stop.current = ctl;
    setPhase("running");
    await runBulk(items, (i) => run(kind, action, i), setResults, ctl.signal);
    setPhase("done");
  }

  const title =
    action === "restart"
      ? t("Restart {n} deployments", { n: items.length })
      : t("Delete {n} {kind}", { n: items.length, kind: t(kindWord[kind]) });

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 p-4">
      <div className="w-full max-w-lg rounded-xl border border-border bg-surface p-5 text-sm">
        <div className="font-semibold">{title}</div>

        {phase === "confirm" && (
          <>
            <div className="mt-2 text-text-muted">
              {action === "restart"
                ? "Each deployment's pods are replaced one by one, as with a single restart."
                : deleteNote(kind)}
            </div>
            <ul className="mt-3 max-h-44 overflow-auto rounded-lg border border-border bg-surface-2 p-2 font-mono text-xs">
              {items.map((i) => (
                <li key={bulkKey(i)} className="truncate">
                  <span className="text-text-muted">{i.namespace}/</span>
                  {i.name}
                </li>
              ))}
            </ul>
            {destructive && (
              <label className="mt-4 flex flex-col gap-1.5 text-xs text-text-muted">
                {t("Type {n} to confirm", { n: items.length })}
                <input
                  autoFocus
                  value={typed}
                  onChange={(e) => setTyped(e.target.value)}
                  onKeyDown={(e) => {
                    if (e.key === "Enter" && confirmed) void start();
                  }}
                  className="rounded-lg border border-border bg-surface-2 px-3 py-1.75 font-mono text-xs outline-none"
                />
              </label>
            )}
            <div className="mt-5 flex justify-end gap-2.5">
              <button
                type="button"
                onClick={() => onClose([])}
                className="rounded-lg border border-border bg-surface-2 px-3.5 py-2 text-[12.5px]"
              >
                Cancel
              </button>
              <button
                type="button"
                disabled={!confirmed}
                onClick={() => void start()}
                className={`rounded-lg px-3.5 py-2 text-[12.5px] font-semibold text-white disabled:opacity-50 ${
                  destructive ? "bg-err" : "bg-accent"
                }`}
              >
                {destructive ? "Delete" : "Restart"}
              </button>
            </div>
          </>
        )}

        {phase !== "confirm" && (
          <>
            <div className="mt-3 flex items-center gap-3 text-xs">
              <div className="h-1.5 flex-1 overflow-hidden rounded bg-surface-2">
                <div
                  className="h-full bg-accent transition-all"
                  style={{
                    width: `${(results.length / items.length) * 100}%`,
                  }}
                />
              </div>
              <span className="font-mono text-text-muted">
                {results.length} / {items.length}
              </span>
            </div>
            {phase === "done" && (
              <div className="mt-3 text-xs">
                <span className="text-ok">
                  {t("{n} done", { n: succeeded.length })}
                </span>
                {failed.length > 0 && (
                  <span className="ml-3 text-err">
                    {t("{n} with errors", { n: failed.length })}
                  </span>
                )}
                {results.length < items.length && (
                  <span className="ml-3 text-text-muted">
                    {t("{n} not attempted", {
                      n: items.length - results.length,
                    })}
                  </span>
                )}
              </div>
            )}
            {failed.length > 0 && (
              <ul className="mt-3 max-h-40 overflow-auto rounded-lg border border-err/30 bg-surface-2 p-2 text-xs">
                {failed.map((r) => (
                  <li key={r.key} className="py-0.5">
                    <span className="font-mono">{r.key}</span>
                    <span className="text-err"> — {r.error}</span>
                  </li>
                ))}
              </ul>
            )}
            <div className="mt-5 flex justify-end gap-2.5">
              {phase === "running" ? (
                <button
                  type="button"
                  onClick={() => stop.current?.abort()}
                  className="rounded-lg border border-border bg-surface-2 px-3.5 py-2 text-[12.5px]"
                >
                  Stop after this one
                </button>
              ) : (
                <button
                  type="button"
                  onClick={() => onClose(succeeded)}
                  className="rounded-lg bg-accent px-3.5 py-2 text-[12.5px] font-semibold text-white"
                >
                  Close
                </button>
              )}
            </div>
          </>
        )}
      </div>
    </div>
  );
}
