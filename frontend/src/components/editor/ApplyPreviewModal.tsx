"use client";

import type { DryRunResult } from "@/lib/api/resources";
import { YamlDiff } from "./YamlEditor";

export function ApplyPreviewModal({
  result,
  mode,
  busy,
  error,
  onConfirm,
  onCancel,
}: {
  result: DryRunResult;
  mode: "create" | "update";
  busy: boolean;
  error: string | null;
  onConfirm: () => void;
  onCancel: () => void;
}) {
  const unchanged = mode === "update" && result.before === result.after;
  const target = `${result.kind} ${result.namespace ? `${result.namespace}/` : ""}${result.name}`;

  return (
    <div className="fixed inset-0 z-[60] flex items-center justify-center bg-black/60 p-4">
      <div className="flex w-full max-w-4xl flex-col rounded-xl border border-border bg-surface p-5">
        <div className="text-sm font-semibold">
          {mode === "create" ? "Review new resource" : "Review changes"}
        </div>
        <div className="mt-1 font-mono text-xs text-text-muted">{target}</div>
        <div className="mt-1 text-xs text-text-faint">
          Validated by the cluster (dry-run) — nothing has been applied yet.
          Server-managed fields are hidden from the diff.
        </div>

        <div className="mt-3 overflow-hidden rounded-lg border border-border">
          {unchanged ? (
            <div className="p-6 text-sm text-text-muted">
              No differences — the manifest matches what&apos;s on the cluster.
            </div>
          ) : (
            <YamlDiff
              before={result.before}
              after={result.after}
              height="420px"
            />
          )}
        </div>

        {error && (
          <div className="mt-3 rounded-lg border border-err/30 bg-surface-2 p-3 text-xs text-err">
            {error}
          </div>
        )}

        <div className="mt-4 flex justify-end gap-2.5">
          <button
            type="button"
            onClick={onCancel}
            disabled={busy}
            className="rounded-lg border border-border bg-surface-2 px-3.5 py-2 text-[12.5px] disabled:opacity-60"
          >
            Back to editor
          </button>
          <button
            type="button"
            onClick={onConfirm}
            disabled={busy || unchanged}
            className="rounded-lg bg-accent px-3.5 py-2 text-[12.5px] font-semibold text-white disabled:opacity-60"
          >
            {busy
              ? "Applying…"
              : mode === "create"
                ? "Create"
                : "Confirm apply"}
          </button>
        </div>
      </div>
    </div>
  );
}
