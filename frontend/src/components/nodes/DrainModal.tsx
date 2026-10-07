"use client";

import { useState } from "react";
import useSWR from "swr";
import { friendlyErrorMessage } from "@/lib/api/errors";
import {
  drainNode,
  getDrainPlan,
  type DrainPod,
  type DrainResult,
} from "@/lib/api/nodeActions";

const label: Record<DrainPod["disposition"], string> = {
  evict: "will be evicted",
  skip: "left alone",
  unmanaged: "no controller — deleted for good",
  localdata: "emptyDir data is lost",
};

// Drain a node: the plan first (what happens to each pod, and which need explicit
// consent), then the node's name typed to confirm. Pods a PodDisruptionBudget
// protects are retried for a while and reported as pending — never forced.
export function DrainModal({
  node,
  onClose,
  onDone,
}: {
  node: string;
  onClose: () => void;
  onDone: () => void;
}) {
  const { data: plan, error } = useSWR(["drain-plan", node], () =>
    getDrainPlan(node),
  );
  const [evictUnmanaged, setEvictUnmanaged] = useState(false);
  const [deleteLocalData, setDeleteLocalData] = useState(false);
  const [typed, setTyped] = useState("");
  const [busy, setBusy] = useState(false);
  const [runError, setRunError] = useState<string | null>(null);
  const [result, setResult] = useState<DrainResult | null>(null);

  const unmanaged = (plan ?? []).filter((p) => p.disposition === "unmanaged");
  const localData = (plan ?? []).filter((p) => p.disposition === "localdata");
  const toEvict = (plan ?? []).filter((p) => p.disposition !== "skip").length;
  const blocked =
    (unmanaged.length > 0 && !evictUnmanaged) ||
    (localData.length > 0 && !deleteLocalData);

  async function run() {
    setBusy(true);
    setRunError(null);
    try {
      setResult(await drainNode(node, { evictUnmanaged, deleteLocalData }));
      onDone();
    } catch (err) {
      setRunError(friendlyErrorMessage(err));
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 p-4">
      <div className="flex max-h-[90vh] w-full max-w-2xl flex-col rounded-xl border border-border bg-surface p-5">
        <div className="text-sm font-semibold">
          Drain <span className="font-mono">{node}</span>
        </div>
        <div className="mt-1 text-xs text-text-muted">
          The node is cordoned first, then its pods are evicted so their
          controllers recreate them elsewhere. Evictions respect
          PodDisruptionBudgets: a pod a budget protects is retried for about a
          minute and then reported, not forced.
        </div>

        {result ? (
          <div className="mt-4 flex flex-col gap-2 text-sm">
            <div className="text-ok">
              Evicted {result.evicted}, left alone {result.skipped}.
            </div>
            {(result.pending?.length ?? 0) > 0 && (
              <div className="rounded-lg border border-warn/30 bg-surface-2 p-3 text-xs text-warn">
                Still on the node (a PodDisruptionBudget refuses for now — run
                the drain again later):
                <ul className="mt-1 list-disc pl-5 font-mono text-text-muted">
                  {result.pending!.map((p) => (
                    <li key={`${p.namespace}/${p.name}`}>
                      {p.namespace}/{p.name}
                    </li>
                  ))}
                </ul>
              </div>
            )}
            {(result.failed?.length ?? 0) > 0 && (
              <div className="rounded-lg border border-err/30 bg-surface-2 p-3 text-xs text-err">
                Could not evict:
                <ul className="mt-1 list-disc pl-5 font-mono">
                  {result.failed!.map((f) => (
                    <li key={f.pod}>
                      {f.pod} — {f.reason}
                    </li>
                  ))}
                </ul>
              </div>
            )}
            <div className="mt-2 flex justify-end">
              <button
                type="button"
                onClick={onClose}
                className="rounded-lg bg-accent px-3.5 py-2 text-[12.5px] font-semibold text-white"
              >
                Close
              </button>
            </div>
          </div>
        ) : (
          <>
            <div className="mt-4 min-h-0 flex-1 overflow-auto rounded-lg border border-border">
              {error ? (
                <div className="p-3 text-xs text-err">
                  Couldn&apos;t read the node&apos;s pods:{" "}
                  {friendlyErrorMessage(error)}
                </div>
              ) : !plan ? (
                <div className="p-3 text-xs text-text-muted">
                  Loading the plan…
                </div>
              ) : plan.length === 0 ? (
                <div className="p-3 text-xs text-text-muted">
                  No pods on this node — draining only cordons it.
                </div>
              ) : (
                <table className="w-full text-left text-xs">
                  <tbody>
                    {plan.map((p) => (
                      <tr
                        key={`${p.namespace}/${p.name}`}
                        className="border-t border-border first:border-t-0"
                      >
                        <td className="px-3 py-1.5 font-mono">
                          {p.namespace}/{p.name}
                        </td>
                        <td
                          className={`px-3 py-1.5 ${
                            p.disposition === "unmanaged" ||
                            p.disposition === "localdata"
                              ? "text-warn"
                              : "text-text-muted"
                          }`}
                        >
                          {label[p.disposition]}
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              )}
            </div>

            {unmanaged.length > 0 && (
              <label className="mt-3 flex items-start gap-2 text-xs text-warn">
                <input
                  type="checkbox"
                  checked={evictUnmanaged}
                  onChange={(e) => setEvictUnmanaged(e.target.checked)}
                  className="mt-0.5"
                />
                Also delete the {unmanaged.length} pod
                {unmanaged.length === 1 ? "" : "s"} with no controller — nothing
                will recreate them.
              </label>
            )}
            {localData.length > 0 && (
              <label className="mt-2 flex items-start gap-2 text-xs text-warn">
                <input
                  type="checkbox"
                  checked={deleteLocalData}
                  onChange={(e) => setDeleteLocalData(e.target.checked)}
                  className="mt-0.5"
                />
                Accept losing the emptyDir data of {localData.length} pod
                {localData.length === 1 ? "" : "s"}.
              </label>
            )}

            <label className="mt-4 flex flex-col gap-1.5 text-xs text-text-muted">
              Type <span className="font-mono text-text">{node}</span> to drain
              it ({toEvict} pod{toEvict === 1 ? "" : "s"} affected)
              <input
                value={typed}
                onChange={(e) => setTyped(e.target.value)}
                className="rounded-lg border border-border bg-surface-2 px-3 py-1.75 font-mono text-xs outline-none"
              />
            </label>
            {runError && (
              <div className="mt-3 rounded-lg border border-err/30 bg-surface-2 p-3 text-xs text-err">
                {runError}
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
                onClick={() => void run()}
                disabled={busy || !plan || typed !== node || blocked}
                className="rounded-lg bg-err px-3.5 py-2 text-[12.5px] font-semibold text-white disabled:opacity-50"
              >
                {busy ? "Draining… (up to a minute)" : "Drain"}
              </button>
            </div>
          </>
        )}
      </div>
    </div>
  );
}
