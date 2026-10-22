"use client";

import { useState } from "react";
import { friendlyErrorMessage } from "@/lib/api/errors";
import type { Instance } from "@/lib/api/instances";
import { uninstallRelease } from "@/lib/api/catalog";
import { deleteDeployment } from "@/lib/api/deploymentActions";
import { deleteStatefulSet } from "@/lib/api/workloadDelete";
import { deletePVC } from "@/lib/api/storage";

// Deleting an instance (Roadmap Phase 77a) adds no write path of its own: a chart is uninstalled with
// Helm, an image install by deleting its workload (its Secret and Service belong to it), and volume
// claims only if the person ticks each one by name — the data goes with them. The instance's name must
// be typed.
export function DeleteInstanceModal({
  instance,
  onClose,
  onDone,
}: {
  instance: Instance;
  onClose: () => void;
  onDone: () => void;
}) {
  const [typed, setTyped] = useState("");
  const [claims, setClaims] = useState<string[]>([]);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const pvcs = instance.parts.filter((p) => p.kind === "PersistentVolumeClaim");
  const stays = instance.parts.filter(
    (p) => p.fate === "stays" && p.kind !== "PersistentVolumeClaim",
  );
  const workloads = instance.parts.filter(
    (p) => p.kind === "StatefulSet" || p.kind === "Deployment",
  );

  async function run() {
    setBusy(true);
    setError(null);
    try {
      if (instance.origin === "helm") {
        await uninstallRelease(instance.namespace, instance.name);
      } else {
        for (const w of workloads) {
          if (w.kind === "StatefulSet") {
            await deleteStatefulSet(instance.namespace, w.name);
          } else {
            await deleteDeployment(instance.namespace, w.name);
          }
        }
      }
      for (const c of claims) {
        await deletePVC(instance.namespace, c);
      }
      onDone();
    } catch (e) {
      setError(friendlyErrorMessage(e));
      setBusy(false);
    }
  }

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 p-4">
      <div className="max-h-[90vh] w-full max-w-lg overflow-auto rounded-xl border border-border bg-surface p-5">
        <div className="text-sm font-semibold">Delete {instance.name}?</div>
        <div className="mt-3 text-xs text-text-muted">
          Removed:{" "}
          <span className="font-mono text-text">
            {instance.parts
              .filter((p) => p.fate === "removed")
              .map((p) => `${p.kind} ${p.name}`)
              .join(", ")}
          </span>
        </div>
        {stays.length > 0 && (
          <div className="mt-2 text-xs text-text-muted">
            Stays in the cluster:{" "}
            <span className="font-mono text-text">
              {stays.map((p) => `${p.kind} ${p.name}`).join(", ")}
            </span>
          </div>
        )}
        {pvcs.length > 0 && (
          <div className="mt-3 rounded-lg border border-border bg-surface-2 p-3">
            <div className="text-xs text-text-muted">
              Volume claims stay unless you tick them. Deleting a claim usually
              deletes its data and cannot be undone.
            </div>
            {pvcs.map((p) => (
              <label
                key={p.name}
                className="mt-2 flex items-center gap-2 font-mono text-xs"
              >
                <input
                  type="checkbox"
                  checked={claims.includes(p.name)}
                  disabled={busy}
                  onChange={(e) =>
                    setClaims((c) =>
                      e.target.checked
                        ? [...c, p.name]
                        : c.filter((n) => n !== p.name),
                    )
                  }
                />
                {p.name} {p.detail ? `(${p.detail})` : ""}
              </label>
            ))}
          </div>
        )}
        <label className="mt-4 flex flex-col gap-1.5 text-xs text-text-muted">
          Type <span className="font-mono text-text">{instance.name}</span> to
          confirm
          <input
            autoFocus
            value={typed}
            onChange={(e) => setTyped(e.target.value)}
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
            onClick={onClose}
            disabled={busy}
            className="rounded-lg border border-border bg-surface-2 px-3.5 py-2 text-[12.5px] disabled:opacity-60"
          >
            Cancel
          </button>
          <button
            type="button"
            onClick={() => void run()}
            disabled={busy || typed !== instance.name}
            className="rounded-lg bg-err px-3.5 py-2 text-[12.5px] font-semibold text-white disabled:opacity-50"
          >
            {busy ? "Deleting…" : "Delete"}
          </button>
        </div>
      </div>
    </div>
  );
}
