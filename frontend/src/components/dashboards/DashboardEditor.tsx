"use client";

import { useState } from "react";
import useSWR from "swr";
import { friendlyErrorMessage } from "@/lib/api/errors";
import { queryNow } from "@/lib/dashboards/prom";
import { saveCustomDashboard } from "@/lib/dashboards/store";
import {
  MAX_PANELS,
  MAX_QUERIES,
  dashboardIdRe,
  validateDashboard,
  type Dashboard,
  type Panel,
  type PanelKind,
  type PanelUnit,
} from "@/lib/dashboards/model";
import { PanelView } from "./PanelView";

const input =
  "rounded-lg border border-border bg-surface-2 px-3 py-1.75 font-mono text-xs outline-none placeholder:text-text-faint";

const units: { key: PanelUnit; label: string }[] = [
  { key: "short", label: "Number" },
  { key: "percentunit", label: "Percent (0–1)" },
  { key: "percent", label: "Percent (0–100)" },
  { key: "bytes", label: "Bytes" },
  { key: "bytesps", label: "Bytes per second" },
  { key: "ops", label: "Per second" },
  { key: "seconds", label: "Seconds" },
];

export function slugify(title: string): string {
  return title
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, "-")
    .replace(/^-+|-+$/g, "")
    .slice(0, 42)
    .replace(/-+$/, "");
}

const blankPanel = (): Panel => ({
  title: "",
  kind: "line",
  unit: "short",
  queries: [{ expr: "" }],
});

// The metric finder: names that contain what was typed, from the cluster's Prometheus.
function MetricFinder({ onPick }: { onPick: (name: string) => void }) {
  const [term, setTerm] = useState("");
  const ok = /^[A-Za-z0-9_:]{2,40}$/.test(term);
  const { data, error } = useSWR(
    ok ? ["metric-names", term] : null,
    async () => {
      const rows = await queryNow(`count by (__name__) ({__name__=~".*${term}.*"})`);
      return rows.map((r) => r.metric.__name__).filter(Boolean).sort().slice(0, 40);
    },
    { revalidateOnFocus: false, shouldRetryOnError: false },
  );
  return (
    <div className="flex flex-col gap-2 rounded-lg border border-border p-3">
      <div className="text-xs font-medium">Find a metric</div>
      <input
        aria-label="Metric name contains"
        value={term}
        onChange={(e) => setTerm(e.target.value.trim())}
        placeholder="part of a metric name, e.g. redis or http_requests"
        className={input}
      />
      {term && !ok && (
        <div className="text-[11px] text-text-faint">
          Letters, digits, _ and : — at least 2 characters.
        </div>
      )}
      {error && <div className="text-[11px] text-err">{friendlyErrorMessage(error)}</div>}
      {data && data.length === 0 && (
        <div className="text-[11px] text-text-faint">No metric has that in its name.</div>
      )}
      {data && data.length > 0 && (
        <div className="flex max-h-36 flex-wrap gap-1.5 overflow-auto">
          {data.map((n) => (
            <button
              key={n}
              type="button"
              onClick={() => onPick(n)}
              className="rounded-md border border-border bg-surface-2 px-2 py-0.5 font-mono text-[11px] hover:border-accent"
            >
              {n}
            </button>
          ))}
        </div>
      )}
    </div>
  );
}

// "New dashboard" / "Edit" / "Customize a built-in one": a form of panels. Saving writes a ConfigMap
// through the generic YAML create/update (store.ts), as the signed-in user.
export function DashboardEditor({
  initial,
  initialNamespace,
  isEdit,
  onClose,
  onSaved,
}: {
  initial: Dashboard;
  initialNamespace: string;
  isEdit: boolean;
  onClose: () => void;
  onSaved: (id: string) => void;
}) {
  const [title, setTitle] = useState(initial.title);
  const [description, setDescription] = useState(initial.description ?? "");
  const [namespace, setNamespace] = useState(initialNamespace);
  const [panels, setPanels] = useState<Panel[]>(
    initial.panels.length ? structuredClone(initial.panels) : [blankPanel()],
  );
  const [sel, setSel] = useState({ panel: 0, query: 0 });
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  const id = isEdit ? initial.id : slugify(title);

  function patchPanel(i: number, patch: Partial<Panel>) {
    setPanels((ps) => ps.map((p, j) => (j === i ? { ...p, ...patch } : p)));
  }
  function patchQuery(i: number, q: number, patch: { expr?: string; legend?: string }) {
    setPanels((ps) =>
      ps.map((p, j) =>
        j === i
          ? { ...p, queries: p.queries.map((x, k) => (k === q ? { ...x, ...patch } : x)) }
          : p,
      ),
    );
  }
  function pickMetric(name: string) {
    const p = panels[sel.panel];
    if (!p) return;
    const cur = p.queries[sel.query]?.expr ?? "";
    patchQuery(sel.panel, sel.query, { expr: cur.trim() ? `${cur} ${name}` : name });
  }

  const draft: Dashboard = {
    id,
    group: "Custom",
    title: title.trim(),
    description: description.trim() || undefined,
    variables: initial.variables,
    requires: initial.requires,
    panels: panels.map((p) => ({
      ...p,
      title: p.title.trim(),
      queries: p.queries
        .filter((q) => q.expr.trim())
        .map((q) => ({ expr: q.expr.trim(), legend: q.legend?.trim() || undefined })),
    })),
  };
  const problem = validateDashboard(draft) || (dashboardIdRe.test(id) ? "" : "The name must be lower-case letters, digits and dashes.");

  async function save() {
    setBusy(true);
    setError("");
    try {
      await saveCustomDashboard(namespace.trim(), draft);
      onSaved(id);
    } catch (e) {
      setError(friendlyErrorMessage(e));
    } finally {
      setBusy(false);
    }
  }

  const sp = panels[sel.panel];

  return (
    <div className="fixed inset-0 z-50 flex items-start justify-center overflow-auto bg-black/60 p-4">
      <div className="w-full max-w-5xl rounded-xl border border-border bg-surface p-5">
        <div className="flex items-center justify-between">
          <div className="text-sm font-semibold">
            {isEdit ? "Edit the dashboard" : "New dashboard"}
          </div>
          <button type="button" onClick={onClose} className="text-xs text-text-muted">
            Close
          </button>
        </div>

        <div className="mt-4 grid gap-3 md:grid-cols-3">
          <label className="flex flex-col gap-1 text-xs text-text-muted">
            Title
            <input aria-label="Dashboard title" value={title} onChange={(e) => setTitle(e.target.value)} className={input} />
          </label>
          <label className="flex flex-col gap-1 text-xs text-text-muted">
            Description (optional)
            <input value={description} onChange={(e) => setDescription(e.target.value)} className={input} />
          </label>
          <label className="flex flex-col gap-1 text-xs text-text-muted">
            Save it in namespace
            <input aria-label="Namespace to save in" value={namespace} onChange={(e) => setNamespace(e.target.value)} className={input} />
          </label>
        </div>
        <div className="mt-1 text-[11px] text-text-faint">
          Stored as the ConfigMap <span className="font-mono">vinaire-dashboard-{id || "…"}</span> in that
          namespace; saving needs the right to create ConfigMaps there.
        </div>

        <div className="mt-4 grid gap-4 md:grid-cols-[1fr_320px]">
          <div className="flex flex-col gap-3">
            {panels.map((p, i) => (
              <div
                key={i}
                className={`rounded-lg border p-3 ${sel.panel === i ? "border-accent" : "border-border"}`}
                onFocus={() => setSel((s) => (s.panel === i ? s : { panel: i, query: 0 }))}
              >
                <div className="flex flex-wrap items-center gap-2">
                  <input
                    aria-label={`Panel ${i + 1} title`}
                    value={p.title}
                    onChange={(e) => patchPanel(i, { title: e.target.value })}
                    placeholder="Panel title"
                    className={`${input} flex-1`}
                  />
                  <select
                    aria-label={`Panel ${i + 1} kind`}
                    value={p.kind}
                    onChange={(e) => patchPanel(i, { kind: e.target.value as PanelKind })}
                    className={input}
                  >
                    <option value="line">Chart</option>
                    <option value="stat">Single number</option>
                  </select>
                  <select
                    aria-label={`Panel ${i + 1} unit`}
                    value={p.unit}
                    onChange={(e) => patchPanel(i, { unit: e.target.value as PanelUnit })}
                    className={input}
                  >
                    {units.map((u) => (
                      <option key={u.key} value={u.key}>
                        {u.label}
                      </option>
                    ))}
                  </select>
                  <button
                    type="button"
                    onClick={() => setPanels((ps) => ps.filter((_, j) => j !== i))}
                    disabled={panels.length === 1}
                    className="text-xs text-err disabled:opacity-40"
                  >
                    Remove
                  </button>
                </div>
                {p.queries.map((q, k) => (
                  <div key={k} className="mt-2 flex flex-wrap items-start gap-2">
                    <textarea
                      aria-label={`Panel ${i + 1} query ${k + 1}`}
                      value={q.expr}
                      rows={2}
                      onFocus={() => setSel({ panel: i, query: k })}
                      onChange={(e) => patchQuery(i, k, { expr: e.target.value })}
                      placeholder="PromQL, e.g. sum(rate(http_requests_total[5m]))"
                      className={`${input} min-w-[240px] flex-1`}
                    />
                    <input
                      aria-label={`Panel ${i + 1} legend ${k + 1}`}
                      value={q.legend ?? ""}
                      onChange={(e) => patchQuery(i, k, { legend: e.target.value })}
                      placeholder="legend, e.g. {pod}"
                      className={`${input} w-36`}
                    />
                    {p.queries.length > 1 && (
                      <button
                        type="button"
                        onClick={() => patchPanel(i, { queries: p.queries.filter((_, x) => x !== k) })}
                        className="text-xs text-text-muted"
                      >
                        ×
                      </button>
                    )}
                  </div>
                ))}
                {p.queries.length < MAX_QUERIES && (
                  <button
                    type="button"
                    onClick={() => patchPanel(i, { queries: [...p.queries, { expr: "" }] })}
                    className="mt-2 text-xs text-accent-text"
                  >
                    + another query on this panel
                  </button>
                )}
              </div>
            ))}
            {panels.length < MAX_PANELS && (
              <button
                type="button"
                onClick={() => setPanels((ps) => [...ps, blankPanel()])}
                className="w-fit rounded-lg border border-border bg-surface-2 px-3 py-1.5 text-xs"
              >
                + Add a panel
              </button>
            )}
          </div>

          <div className="flex flex-col gap-3">
            <MetricFinder onPick={pickMetric} />
            {sp && sp.queries.some((q) => q.expr.trim()) && (
              <div>
                <div className="mb-1 text-xs text-text-muted">Preview of panel {sel.panel + 1}</div>
                <PanelView
                  dashboardId="preview"
                  index={sel.panel}
                  panel={{ ...sp, title: sp.title || "Preview", queries: sp.queries.filter((q) => q.expr.trim()) }}
                  declared={(initial.variables ?? []).map((v) => v.name)}
                  chosen={{}}
                  rangeSec={3600}
                  paused={false}
                />
              </div>
            )}
          </div>
        </div>

        {(error || (problem && title)) && (
          <div className="mt-4 rounded-lg border border-err/30 bg-surface-2 p-3 text-xs text-err">
            {error || problem}
          </div>
        )}
        <div className="mt-5 flex justify-end gap-2.5">
          <button type="button" onClick={onClose} disabled={busy} className="rounded-lg border border-border bg-surface-2 px-3.5 py-2 text-[12.5px]">
            Cancel
          </button>
          <button
            type="button"
            onClick={() => void save()}
            disabled={busy || problem !== ""}
            className="rounded-lg bg-accent px-3.5 py-2 text-[12.5px] font-medium text-white disabled:opacity-50"
          >
            {busy ? "Saving…" : "Save"}
          </button>
        </div>
      </div>
    </div>
  );
}
