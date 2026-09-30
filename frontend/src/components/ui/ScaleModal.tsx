"use client";

import { useState } from "react";

// Roadmap Phase 33's Scale control. `currentReplicas` should be a freshly
// refetched value (the caller re-validates before rendering this) so a
// stale number can't silently overwrite a concurrent change made via
// `kubectl scale`. The caller only renders this component while open
// (`{open && <ScaleModal ... />}`), so every open is a fresh mount and
// `value` below always starts from the latest `currentReplicas` — no
// reset-on-prop-change effect needed.
export function ScaleModal({
  resourceName,
  currentReplicas,
  busy = false,
  orderNote,
  onApply,
  onCancel,
}: {
  resourceName: string;
  currentReplicas: number;
  busy?: boolean;
  // StatefulSet-specific: scale-down kills pods one at a time in
  // descending ordinal order, unlike a Deployment.
  orderNote?: string;
  onApply: (replicas: number) => void;
  onCancel: () => void;
}) {
  const [value, setValue] = useState(currentReplicas);

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 p-4">
      <div className="w-full max-w-sm rounded-xl border border-border bg-surface p-5">
        <div className="text-sm font-semibold">
          Scale &ldquo;{resourceName}&rdquo;
        </div>
        <div className="mt-1 text-xs text-text-muted">
          Current: {currentReplicas} replica{currentReplicas === 1 ? "" : "s"}
        </div>
        {orderNote && (
          <div className="mt-2 text-[11px] text-warn">{orderNote}</div>
        )}

        <div className="mt-4 flex items-center justify-center gap-3">
          <button
            type="button"
            onClick={() => setValue((v) => Math.max(0, v - 1))}
            disabled={busy || value <= 0}
            className="flex h-9 w-9 items-center justify-center rounded-lg border border-border text-lg disabled:opacity-40"
          >
            −
          </button>
          <input
            type="number"
            min={0}
            value={value}
            onChange={(e) => setValue(Math.max(0, Number(e.target.value) || 0))}
            disabled={busy}
            className="w-20 rounded-lg border border-border bg-surface-2 px-3 py-1.5 text-center font-mono text-sm outline-none"
          />
          <button
            type="button"
            onClick={() => setValue((v) => v + 1)}
            disabled={busy}
            className="flex h-9 w-9 items-center justify-center rounded-lg border border-border text-lg disabled:opacity-40"
          >
            +
          </button>
        </div>

        <div className="mt-5 flex justify-end gap-2.5">
          <button
            type="button"
            onClick={onCancel}
            disabled={busy}
            className="rounded-lg border border-border bg-surface-2 px-3.5 py-2 text-[12.5px] disabled:opacity-60"
          >
            Cancel
          </button>
          <button
            type="button"
            onClick={() => onApply(value)}
            disabled={busy}
            className="rounded-lg bg-accent px-3.5 py-2 text-[12.5px] font-semibold text-white disabled:opacity-60"
          >
            {busy ? "Applying…" : "Apply"}
          </button>
        </div>
      </div>
    </div>
  );
}
