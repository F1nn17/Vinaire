"use client";

import { useState } from "react";
import { useNamespaces } from "@/lib/hooks/useNamespaces";
import {
  createProject,
  quotaPresets,
  validNamespaceName,
  validProjectName,
  type QuotaPreset,
  type StepResult,
} from "@/lib/api/projects";

const input =
  "rounded-lg border border-border bg-surface px-3 py-1.75 font-mono text-xs outline-none placeholder:text-text-faint";

// Roadmap Phase 53: a project is a label on namespaces, so creating one means creating
// namespaces with the label, and/or putting existing namespaces under it. Each step is a
// separate call as the signed-in user; the result lists which worked.
export function CreateProjectModal({
  onClose,
  onCreated,
}: {
  onClose: () => void;
  onCreated: (name: string) => void;
}) {
  const { data: namespaces } = useNamespaces();
  const [name, setName] = useState("");
  const [newNs, setNewNs] = useState("");
  const [existing, setExisting] = useState<string[]>([]);
  const [quota, setQuota] = useState<QuotaPreset>("small");
  const [busy, setBusy] = useState(false);
  const [steps, setSteps] = useState<StepResult[] | null>(null);

  const newList = newNs
    .split(/[\s,]+/)
    .map((s) => s.trim())
    .filter(Boolean);
  const badNew = newList.filter((n) => !validNamespaceName(n));
  // namespaces already in another project, or system ones, aren't offered
  const free = (namespaces ?? []).filter(
    (n) =>
      !n.project &&
      !["kube-system", "kube-public", "kube-node-lease"].includes(n.name),
  );
  const valid =
    validProjectName(name) &&
    badNew.length === 0 &&
    newList.length + existing.length > 0;

  async function submit() {
    setBusy(true);
    setSteps([]);
    const all = await createProject(
      { name, newNamespaces: newList, existingNamespaces: existing, quota },
      (r) => setSteps((s) => [...(s ?? []), r]),
    );
    setBusy(false);
    if (all.some((r) => r.ok)) onCreated(name);
  }

  const done = steps !== null && !busy;
  const failed = steps?.filter((s) => !s.ok) ?? [];

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 p-4">
      <div className="flex max-h-[90vh] w-full max-w-xl flex-col gap-4 overflow-y-auto rounded-xl border border-border bg-surface p-5">
        <div className="text-sm font-semibold">Create a project</div>
        <div className="text-xs text-text-muted">
          A project groups namespaces under the label{" "}
          <code className="font-mono">vinaire.io/project</code> — it is kept in
          the cluster, so <code className="font-mono">kubectl</code> sees the
          same grouping. Deleting a project later only removes the label.
        </div>

        <label className="flex flex-col gap-1.5 text-xs text-text-muted">
          Project name
          <input
            aria-label="Project name"
            value={name}
            onChange={(e) => setName(e.target.value.toLowerCase())}
            placeholder="shop"
            className={input}
            disabled={busy}
          />
          {name && !validProjectName(name) && (
            <span className="text-[11px] text-err">
              Lowercase letters, digits and “-”, up to 42 characters.
            </span>
          )}
        </label>

        <label className="flex flex-col gap-1.5 text-xs text-text-muted">
          New namespaces (separated by spaces or commas)
          <input
            aria-label="New namespaces"
            value={newNs}
            onChange={(e) => setNewNs(e.target.value)}
            placeholder="shop-dev, shop-prod"
            className={input}
            disabled={busy}
          />
          {badNew.length > 0 && (
            <span className="text-[11px] text-err">
              Not a valid namespace name: {badNew.join(", ")}
            </span>
          )}
        </label>

        {free.length > 0 && (
          <div className="flex flex-col gap-1.5 text-xs text-text-muted">
            Or put existing namespaces in it
            <div className="flex max-h-32 flex-wrap gap-1.5 overflow-y-auto rounded-lg border border-border p-2">
              {free.map((n) => (
                <label
                  key={n.name}
                  className="flex items-center gap-1.5 rounded bg-surface-2 px-2 py-1 font-mono text-[11px]"
                >
                  <input
                    type="checkbox"
                    checked={existing.includes(n.name)}
                    disabled={busy}
                    onChange={(e) =>
                      setExisting(
                        e.target.checked
                          ? [...existing, n.name]
                          : existing.filter((x) => x !== n.name),
                      )
                    }
                  />
                  {n.name}
                </label>
              ))}
            </div>
          </div>
        )}

        <label className="flex flex-col gap-1.5 text-xs text-text-muted">
          Resource quota for each namespace
          <select
            aria-label="Quota"
            value={quota}
            onChange={(e) => setQuota(e.target.value as QuotaPreset)}
            className={input}
            disabled={busy}
          >
            <option value="none">None</option>
            {(["small", "medium", "large"] as const).map((p) => (
              <option key={p} value={p}>
                {p[0].toUpperCase() + p.slice(1)} — {quotaPresets[p].pods} pods,{" "}
                {quotaPresets[p]["requests.cpu"]} CPU,{" "}
                {quotaPresets[p]["requests.memory"]} memory requested
              </option>
            ))}
          </select>
        </label>

        {steps && (
          <ul className="flex flex-col gap-1 text-xs">
            {steps.map((s, i) => (
              <li key={i} className={s.ok ? "text-ok" : "text-err"}>
                {s.ok ? "✓" : "✗"} {s.what}
                {s.error && (
                  <span className="text-text-muted"> — {s.error}</span>
                )}
              </li>
            ))}
          </ul>
        )}

        <div className="flex justify-end gap-2">
          <button
            type="button"
            onClick={onClose}
            className="rounded-lg border border-border px-3 py-1.5 text-xs"
          >
            {done ? "Close" : "Cancel"}
          </button>
          {!done || failed.length > 0 ? (
            <button
              type="button"
              disabled={!valid || busy}
              onClick={() => void submit()}
              className="rounded-lg bg-accent px-4 py-1.5 text-[12.5px] font-semibold text-white disabled:opacity-50"
            >
              {busy
                ? "Creating…"
                : failed.length > 0
                  ? "Try again"
                  : "Create project"}
            </button>
          ) : null}
        </div>
      </div>
    </div>
  );
}
