"use client";

import { useState } from "react";
import { friendlyErrorMessage } from "@/lib/api/errors";
import {
  setNodeLabels,
  setNodeTaints,
  type Taint,
} from "@/lib/api/nodeActions";

const inputClass =
  "w-full rounded-lg border border-border bg-surface-2 px-3 py-1.75 font-mono text-xs outline-none placeholder:text-text-faint";

const effects = ["NoSchedule", "PreferNoSchedule", "NoExecute"];

type LabelRow = { key: string; value: string; original: boolean };

// Edit a node's labels or taints. Labels: keys already on the node can be changed
// or removed; taints: the full list is replaced (a change made by someone else in
// the meantime is refused rather than overwritten).
export function NodeMetaModal({
  node,
  kind,
  labels,
  taints,
  onClose,
  onSaved,
}: {
  node: string;
  kind: "labels" | "taints";
  labels: Record<string, string>;
  taints: Taint[];
  onClose: () => void;
  onSaved: () => void;
}) {
  const [labelRows, setLabelRows] = useState<LabelRow[]>(
    Object.entries(labels)
      .filter(([k]) => k !== "kubernetes.io/hostname")
      .map(([key, value]) => ({ key, value, original: true })),
  );
  const [taintRows, setTaintRows] = useState<Taint[]>(
    taints.map((t) => ({ ...t })),
  );
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  async function save() {
    setBusy(true);
    setError(null);
    try {
      if (kind === "labels") {
        const set: Record<string, string> = {};
        for (const r of labelRows) {
          const k = r.key.trim();
          if (k && labels[k] !== r.value) set[k] = r.value;
        }
        const keep = new Set(labelRows.map((r) => r.key.trim()));
        const remove = Object.keys(labels).filter(
          (k) => k !== "kubernetes.io/hostname" && !keep.has(k),
        );
        if (Object.keys(set).length + remove.length === 0) {
          setError("Nothing changed.");
          return;
        }
        await setNodeLabels(node, { set, remove });
      } else {
        await setNodeTaints(
          node,
          taintRows
            .filter((t) => t.key.trim() !== "")
            .map((t) => ({ ...t, key: t.key.trim() })),
        );
      }
      onSaved();
      onClose();
    } catch (err) {
      setError(friendlyErrorMessage(err));
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 p-4">
      <div className="flex max-h-[90vh] w-full max-w-2xl flex-col rounded-xl border border-border bg-surface p-5">
        <div className="text-sm font-semibold">
          {kind === "labels" ? "Labels" : "Taints"} of{" "}
          <span className="font-mono">{node}</span>
        </div>
        <div className="mt-1 text-xs text-text-muted">
          {kind === "labels"
            ? "Labels are what nodeSelector and affinity match on. kubernetes.io/hostname can't be changed."
            : "A taint keeps pods off the node unless they tolerate it. NoSchedule: no new pods; PreferNoSchedule: avoid if possible; NoExecute: also evicts running pods that don't tolerate it."}
        </div>

        <div className="mt-4 min-h-0 flex-1 overflow-auto pr-1">
          {kind === "labels" ? (
            <div className="flex flex-col gap-2">
              {labelRows.map((r, i) => (
                <div key={i} className="flex gap-2">
                  <input
                    aria-label="Label key"
                    value={r.key}
                    placeholder="key"
                    disabled={r.original}
                    onChange={(e) =>
                      setLabelRows(
                        labelRows.map((x, j) =>
                          j === i ? { ...x, key: e.target.value } : x,
                        ),
                      )
                    }
                    className={`${inputClass} ${r.original ? "opacity-70" : ""}`}
                  />
                  <input
                    aria-label="Label value"
                    value={r.value}
                    placeholder="value"
                    onChange={(e) =>
                      setLabelRows(
                        labelRows.map((x, j) =>
                          j === i ? { ...x, value: e.target.value } : x,
                        ),
                      )
                    }
                    className={inputClass}
                  />
                  <button
                    type="button"
                    aria-label="Remove label"
                    onClick={() =>
                      setLabelRows(labelRows.filter((_, j) => j !== i))
                    }
                    className="rounded-lg border border-border bg-surface-2 px-2.5 text-xs text-text-muted"
                  >
                    ✕
                  </button>
                </div>
              ))}
              <button
                type="button"
                onClick={() =>
                  setLabelRows([
                    ...labelRows,
                    { key: "", value: "", original: false },
                  ])
                }
                className="w-fit rounded-lg border border-border bg-surface-2 px-3 py-1.5 text-xs"
              >
                + Add label
              </button>
            </div>
          ) : (
            <div className="flex flex-col gap-2">
              {taintRows.length === 0 && (
                <div className="text-xs text-text-faint">No taints.</div>
              )}
              {taintRows.map((t, i) => (
                <div key={i} className="flex gap-2">
                  <input
                    aria-label="Taint key"
                    value={t.key}
                    placeholder="key"
                    onChange={(e) =>
                      setTaintRows(
                        taintRows.map((x, j) =>
                          j === i ? { ...x, key: e.target.value } : x,
                        ),
                      )
                    }
                    className={inputClass}
                  />
                  <input
                    aria-label="Taint value"
                    value={t.value}
                    placeholder="value (optional)"
                    onChange={(e) =>
                      setTaintRows(
                        taintRows.map((x, j) =>
                          j === i ? { ...x, value: e.target.value } : x,
                        ),
                      )
                    }
                    className={inputClass}
                  />
                  <select
                    aria-label="Taint effect"
                    value={t.effect}
                    onChange={(e) =>
                      setTaintRows(
                        taintRows.map((x, j) =>
                          j === i ? { ...x, effect: e.target.value } : x,
                        ),
                      )
                    }
                    className={inputClass}
                  >
                    {effects.map((e) => (
                      <option key={e} value={e}>
                        {e}
                      </option>
                    ))}
                  </select>
                  <button
                    type="button"
                    aria-label="Remove taint"
                    onClick={() =>
                      setTaintRows(taintRows.filter((_, j) => j !== i))
                    }
                    className="rounded-lg border border-border bg-surface-2 px-2.5 text-xs text-text-muted"
                  >
                    ✕
                  </button>
                </div>
              ))}
              <button
                type="button"
                onClick={() =>
                  setTaintRows([
                    ...taintRows,
                    { key: "", value: "", effect: "NoSchedule" },
                  ])
                }
                className="w-fit rounded-lg border border-border bg-surface-2 px-3 py-1.5 text-xs"
              >
                + Add taint
              </button>
            </div>
          )}
        </div>

        {error && (
          <div className="mt-3 rounded-lg border border-err/30 bg-surface-2 p-3 text-xs text-err">
            {error}
          </div>
        )}
        <div className="mt-5 flex justify-end gap-2.5">
          <button
            type="button"
            onClick={onClose}
            disabled={busy}
            className="rounded-lg border border-border bg-surface-2 px-3.5 py-2 text-[12.5px] disabled:opacity-60"
          >
            Cancel
          </button>
          <button
            type="button"
            onClick={() => void save()}
            disabled={busy}
            className="rounded-lg bg-accent px-3.5 py-2 text-[12.5px] font-semibold text-white disabled:opacity-60"
          >
            {busy ? "Saving…" : "Save"}
          </button>
        </div>
      </div>
    </div>
  );
}
