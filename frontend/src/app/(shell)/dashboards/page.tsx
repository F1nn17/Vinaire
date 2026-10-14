"use client";

import Link from "next/link";
import { Suspense, useMemo, useState } from "react";
import { useRouter, useSearchParams } from "next/navigation";
import { friendlyErrorMessage } from "@/lib/api/errors";
import { t } from "@/lib/i18n/translate";
import { builtinDashboards, dashboardGroups } from "@/lib/dashboards/catalog";
import { toGrafana } from "@/lib/dashboards/grafana";
import { addToGrafana } from "@/lib/dashboards/store";
import type { Dashboard } from "@/lib/dashboards/model";
import {
  useCustomDashboards,
  useRequirement,
  useVariableOptions,
} from "@/lib/hooks/useDashboards";
import { PanelView } from "@/components/dashboards/PanelView";
import { DashboardEditor } from "@/components/dashboards/DashboardEditor";

const ranges = [
  { sec: 900, label: "Last 15 minutes" },
  { sec: 3600, label: "Last hour" },
  { sec: 6 * 3600, label: "Last 6 hours" },
  { sec: 24 * 3600, label: "Last 24 hours" },
];

const selectClass =
  "rounded-lg border border-border bg-surface px-2.5 py-1.5 text-xs outline-none";

export default function DashboardsPage() {
  return (
    <Suspense
      fallback={<div className="p-8 text-sm text-text-muted">Loading…</div>}
    >
      <DashboardsInner />
    </Suspense>
  );
}

type Editing = { dashboard: Dashboard; namespace: string; isEdit: boolean };

function DashboardsInner() {
  const router = useRouter();
  const params = useSearchParams();
  const custom = useCustomDashboards();
  const [editing, setEditing] = useState<Editing | null>(null);

  const all: (Dashboard & { namespace?: string })[] = useMemo(
    () => [...builtinDashboards, ...(custom.data ?? [])],
    [custom.data],
  );
  const wanted = params.get("d") ?? "";
  const current =
    all.find((d) => `${d.group === "Custom" ? "c:" : ""}${d.id}` === wanted) ??
    builtinDashboards[0];
  const key = (d: Dashboard) => `${d.group === "Custom" ? "c:" : ""}${d.id}`;
  const open = (d: Dashboard) =>
    router.replace(`/dashboards?d=${encodeURIComponent(key(d))}`);

  return (
    <div className="flex flex-col gap-[18px] px-8 py-7">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <h1 className="text-lg font-semibold">Dashboards</h1>
          <p className="text-xs text-text-muted">
            Live charts from the cluster&apos;s Prometheus, drawn by Vinaire — no
            Grafana needed. Build your own, or add any of these to Grafana.
          </p>
        </div>
        <button
          type="button"
          onClick={() =>
            setEditing({
              dashboard: {
                id: "",
                group: "Custom",
                title: "",
                panels: [],
              },
              namespace: "default",
              isEdit: false,
            })
          }
          className="rounded-lg bg-accent px-3.5 py-2 text-[12.5px] font-medium text-white"
        >
          + New dashboard
        </button>
      </div>

      <div className="grid gap-4 md:grid-cols-[220px_1fr]">
        <nav className="flex flex-col gap-3" aria-label="Dashboards">
          {[...dashboardGroups, "Custom"].map((g) => {
            const items = all.filter((d) => d.group === g);
            if (g === "Custom" && items.length === 0) return null;
            return (
              <div key={g}>
                <div className="mb-1 px-2 text-[10.5px] uppercase tracking-wide text-text-faint">
                  {g}
                </div>
                {items.map((d) => (
                  <button
                    key={key(d)}
                    type="button"
                    onClick={() => open(d)}
                    aria-current={key(d) === key(current) ? "page" : undefined}
                    className={`block w-full rounded-md px-2.5 py-1.5 text-left text-[12.5px] ${
                      key(d) === key(current)
                        ? "bg-surface-2 text-accent-text"
                        : "text-text-muted hover:bg-surface-2"
                    }`}
                  >
                    {d.title}
                  </button>
                ))}
              </div>
            );
          })}
          {custom.error && (
            <div className="px-2 text-[11px] text-err">
              {friendlyErrorMessage(custom.error)}
            </div>
          )}
        </nav>

        <DashboardView
          key={key(current)}
          dashboard={current}
          storedIn={(current as { namespace?: string }).namespace}
          onCustomize={(d, ns, isEdit) =>
            setEditing({ dashboard: d, namespace: ns, isEdit })
          }
        />
      </div>

      {editing && (
        <DashboardEditor
          initial={editing.dashboard}
          initialNamespace={editing.namespace}
          isEdit={editing.isEdit}
          onClose={() => setEditing(null)}
          onSaved={(id) => {
            setEditing(null);
            void custom.mutate().then(() =>
              router.replace(`/dashboards?d=${encodeURIComponent(`c:${id}`)}`),
            );
          }}
        />
      )}
    </div>
  );
}

function Variable({
  v,
  value,
  onChange,
}: {
  v: NonNullable<Dashboard["variables"]>[number];
  value: string;
  onChange: (x: string) => void;
}) {
  const { data } = useVariableOptions(v);
  return (
    <label className="flex items-center gap-2 text-xs text-text-muted">
      {v.label}
      <select
        aria-label={v.label}
        value={value}
        onChange={(e) => onChange(e.target.value)}
        className={selectClass}
      >
        <option value="">All</option>
        {(data ?? []).map((o) => (
          <option key={o} value={o}>
            {o}
          </option>
        ))}
      </select>
    </label>
  );
}

function DashboardView({
  dashboard,
  storedIn,
  onCustomize,
}: {
  dashboard: Dashboard;
  storedIn?: string;
  onCustomize: (d: Dashboard, namespace: string, isEdit: boolean) => void;
}) {
  const [rangeSec, setRangeSec] = useState(3600);
  const [paused, setPaused] = useState(false);
  const [chosen, setChosen] = useState<Record<string, string>>({});
  const [grafanaNs, setGrafanaNs] = useState("monitoring");
  const [grafanaOpen, setGrafanaOpen] = useState(false);
  const [busy, setBusy] = useState(false);
  const [note, setNote] = useState<{ ok: boolean; text: string } | null>(null);
  const req = useRequirement(dashboard.requires);
  const declared = (dashboard.variables ?? []).map((v) => v.name);
  const isCustom = dashboard.group === "Custom";

  function download() {
    const blob = new Blob([JSON.stringify(toGrafana(dashboard), null, 2)], {
      type: "application/json",
    });
    const url = URL.createObjectURL(blob);
    const a = document.createElement("a");
    a.href = url;
    a.download = `${dashboard.id}.json`;
    a.click();
    URL.revokeObjectURL(url);
  }

  async function sendToGrafana() {
    setBusy(true);
    setNote(null);
    try {
      const name = await addToGrafana(grafanaNs.trim(), dashboard);
      setNote({
        ok: true,
        text: t(
          "ConfigMap {name} saved in {ns}. Grafana's dashboard sidecar (on by default in kube-prometheus-stack, and in the Grafana chart when sidecar.dashboards.enabled=true) picks it up within a minute.",
          { name, ns: grafanaNs.trim() },
        ),
      });
    } catch (e) {
      setNote({ ok: false, text: friendlyErrorMessage(e) });
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="flex min-w-0 flex-col gap-3">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div>
          <h2 className="text-base font-semibold">{dashboard.title}</h2>
          {dashboard.description && (
            <p className="text-xs text-text-muted">{dashboard.description}</p>
          )}
        </div>
        <div className="flex flex-wrap items-center gap-2">
          {(dashboard.variables ?? []).map((v) => (
            <Variable
              key={v.name}
              v={v}
              value={chosen[v.name] ?? ""}
              onChange={(x) => setChosen((c) => ({ ...c, [v.name]: x }))}
            />
          ))}
          <select
            aria-label="Time range"
            value={rangeSec}
            onChange={(e) => setRangeSec(Number(e.target.value))}
            className={selectClass}
          >
            {ranges.map((r) => (
              <option key={r.sec} value={r.sec}>
                {r.label}
              </option>
            ))}
          </select>
          <button
            type="button"
            onClick={() => setPaused((p) => !p)}
            aria-pressed={paused}
            className="rounded-lg border border-border bg-surface px-3 py-1.5 text-xs"
          >
            {paused ? "Resume" : "Pause"}
          </button>
        </div>
      </div>

      <div className="flex flex-wrap items-center gap-2 text-xs">
        {isCustom ? (
          <button
            type="button"
            onClick={() => onCustomize(dashboard, storedIn ?? "default", true)}
            className="rounded-lg border border-border bg-surface px-3 py-1.5"
          >
            Edit
          </button>
        ) : (
          <button
            type="button"
            onClick={() =>
              onCustomize(
                { ...dashboard, id: "", title: `${dashboard.title} (copy)` },
                "default",
                false,
              )
            }
            className="rounded-lg border border-border bg-surface px-3 py-1.5"
          >
            Make my own copy
          </button>
        )}
        <button
          type="button"
          onClick={() => setGrafanaOpen((o) => !o)}
          aria-expanded={grafanaOpen}
          className="rounded-lg border border-border bg-surface px-3 py-1.5"
        >
          Add to Grafana
        </button>
        <button
          type="button"
          onClick={download}
          className="rounded-lg border border-border bg-surface px-3 py-1.5"
        >
          Download Grafana JSON
        </button>
        <span className="text-text-faint">
          {paused ? "paused" : "live, every 10 s"}
        </span>
      </div>

      {grafanaOpen && (
        <div className="flex flex-col gap-2 rounded-[10px] border border-border bg-surface p-3 text-xs">
          <div className="text-text-muted">
            Saves this dashboard as a ConfigMap labelled{" "}
            <span className="font-mono">grafana_dashboard=1</span> — what
            Grafana&apos;s dashboard sidecar watches for. Grafana&apos;s default
            data source has to be this Prometheus.{" "}
            <Link href="/marketplace/install?repo=grafana&chart=grafana" className="text-accent-text underline">
              Install Grafana →
            </Link>
          </div>
          <div className="flex items-center gap-2">
            <label className="flex items-center gap-2 text-text-muted">
              Namespace
              <input
                aria-label="Grafana namespace"
                value={grafanaNs}
                onChange={(e) => setGrafanaNs(e.target.value)}
                className="rounded-lg border border-border bg-surface-2 px-3 py-1.5 font-mono text-xs outline-none"
              />
            </label>
            <button
              type="button"
              disabled={busy || !grafanaNs.trim()}
              onClick={() => void sendToGrafana()}
              className="rounded-lg bg-accent px-3 py-1.5 font-medium text-white disabled:opacity-50"
            >
              {busy ? "Saving…" : "Add"}
            </button>
          </div>
          {note && (
            <div className={note.ok ? "text-ok" : "text-err"}>{note.text}</div>
          )}
        </div>
      )}

      {dashboard.requires && req.data === false && (
        <div className="rounded-[10px] border border-warn/40 bg-surface p-3 text-xs text-warn">
          {t("No series {metric} in this cluster's Prometheus.", {
            metric: dashboard.requires.metric,
          })}{" "}
          {dashboard.requires.hint}{" "}
          <Link href="/metrics-stack" className="underline">
            Metrics stack →
          </Link>
        </div>
      )}

      <div className="grid gap-3 md:grid-cols-4">
        {dashboard.panels.map((p, i) => (
          <PanelView
            key={`${dashboard.id}-${i}`}
            dashboardId={dashboard.id}
            index={i}
            panel={p}
            declared={declared}
            chosen={chosen}
            rangeSec={rangeSec}
            paused={paused}
          />
        ))}
      </div>
    </div>
  );
}
