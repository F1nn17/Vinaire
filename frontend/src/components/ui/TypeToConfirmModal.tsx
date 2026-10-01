"use client";

import { useState } from "react";

// Confirmation for irreversible, cascading actions (Roadmap Phase 27's
// namespace delete): the confirm button stays disabled until the exact name is
// typed, so a stray Enter or a reflex "Yes" can't trigger it. Mounted only
// while open (parents render `{open && <TypeToConfirmModal/>}`), so the input
// always starts empty.
export function TypeToConfirmModal({
  title,
  message,
  expected,
  confirmLabel,
  busy = false,
  error,
  onConfirm,
  onCancel,
}: {
  title: string;
  message: string;
  expected: string;
  confirmLabel: string;
  busy?: boolean;
  error?: string | null;
  onConfirm: () => void;
  onCancel: () => void;
}) {
  const [typed, setTyped] = useState("");
  const matches = typed === expected;

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 p-4">
      <div className="w-full max-w-md rounded-xl border border-border bg-surface p-5">
        <div className="text-sm font-semibold">{title}</div>
        <div className="mt-2 text-sm text-text-muted">{message}</div>
        <label className="mt-4 flex flex-col gap-1.5 text-xs text-text-muted">
          Type <span className="font-mono text-text">{expected}</span> to
          confirm
          <input
            autoFocus
            value={typed}
            onChange={(e) => setTyped(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === "Enter" && matches && !busy) onConfirm();
            }}
            className="rounded-lg border border-border bg-surface-2 px-3 py-1.75 font-mono text-xs outline-none"
          />
        </label>
        {error && (
          <div className="mt-3 rounded-lg border border-err/30 bg-surface-2 p-3 text-xs text-err">
            {error}
          </div>
        )}
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
            onClick={onConfirm}
            disabled={!matches || busy}
            className="rounded-lg bg-err px-3.5 py-2 text-[12.5px] font-semibold text-white disabled:opacity-50"
          >
            {busy ? "Working…" : confirmLabel}
          </button>
        </div>
      </div>
    </div>
  );
}
