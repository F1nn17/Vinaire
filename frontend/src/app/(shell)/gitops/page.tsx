"use client";

import { useState } from "react";
import { useArgoApplications, useGitOps } from "@/lib/hooks/useGitOps";
import {
  removeGitSource,
  setGitOpsMode,
  syncArgoApplication,
  syncGitSource,
  type ArgoApplication,
  type GitOpsMode,
  type GitSource,
  type Phase,
} from "@/lib/api/gitops";
import { friendlyErrorMessage } from "@/lib/api/errors";
import { applyErrorMessage } from "@/components/editor/useYamlApply";
import { PillTabs } from "@/components/ui/Tabs";
import { DataTable, type DataTableColumn } from "@/components/ui/DataTable";
import { StatusDot } from "@/components/ui/StatusDot";
import { ConfirmModal } from "@/components/ui/ConfirmModal";
import { Toast, type ToastState } from "@/components/ui/Toast";
import { CreateResourceModal } from "@/components/editor/CreateResourceModal";
import { argoApplicationTemplates } from "@/components/editor/templates";
import { AddSourceModal } from "@/components/gitops/AddSourceModal";
import { PlusIcon } from "@/components/icons";
import type { StatusColor } from "@/lib/types";

const modeItems: { key: GitOpsMode; label: string }[] = [
  { key: "off", label: "Off" },
  { key: "native", label: "Native" },
  { key: "argocd", label: "Argo CD" },
];

const modeHelp: Record<GitOpsMode, string> = {
  off: "No GitOps for this cluster; the section is hidden from the menu.",
  native:
    "Vinaire's own small sync: it reads manifests from a git repository and applies them, as a ServiceAccount you provide. No health checks, hooks or pruning — for clusters without Argo CD.",
  argocd:
    "Show and drive the Argo CD that is already running in this cluster: its Applications, their sync and health, and Sync now.",
};

function phaseColor(p: Phase): StatusColor {
  if (p === "Synced") return "ok";
  if (p === "Error") return "err";
  return "warn";
}

function ago(iso?: string): string {
  if (!iso) return "—";
  const s = Math.max(
    0,
    Math.round((Date.now() - new Date(iso).getTime()) / 1000),
  );
  if (s < 60) return `${s}s ago`;
  if (s < 3600) return `${Math.round(s / 60)}m ago`;
  if (s < 86400) return `${Math.round(s / 3600)}h ago`;
  return `${Math.round(s / 86400)}d ago`;
}

export default function GitOpsPage() {
  const { data, error, isLoading, mutate } = useGitOps();
  const [toast, setToast] = useState<ToastState>(null);
  const [switching, setSwitching] = useState(false);

  async function changeMode(mode: GitOpsMode) {
    setSwitching(true);
    try {
      await mutate(setGitOpsMode(mode), { revalidate: false });
    } catch (err) {
      setToast({ type: "error", message: applyErrorMessage(err) });
    } finally {
      setSwitching(false);
    }
  }

  return (
    <div className="flex flex-col gap-[18px] px-8 py-7">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <div className="text-[15px] font-semibold">GitOps</div>
          {data && (
            <div className="mt-0.5 text-xs text-text-faint">
              cluster <span className="font-mono">{data.cluster}</span>
            </div>
          )}
        </div>
        {data?.available && (
          <PillTabs
            items={modeItems}
            active={data.mode}
            onChange={(m) => !switching && void changeMode(m)}
          />
        )}
      </div>

      {error ? (
        <div className="text-sm text-err">
          Failed to load GitOps: {friendlyErrorMessage(error)}
        </div>
      ) : isLoading || !data ? (
        <div className="text-sm text-text-muted">Loading…</div>
      ) : !data.available ? (
        <div className="rounded-lg border border-border bg-surface-2 p-4 text-sm text-text-muted">
          GitOps isn&apos;t available: Vinaire has nowhere to keep its
          configuration. Run it in a cluster, or set{" "}
          <span className="font-mono">VINAIRE_DATA_DIR</span> when running on a
          machine of your own.
        </div>
      ) : (
        <>
          <div className="text-xs text-text-muted">{modeHelp[data.mode]}</div>
          {data.warning && (
            <div className="rounded-lg border border-warn/30 bg-surface-2 p-3 text-xs text-warn">
              {data.warning}
            </div>
          )}
          {data.mode === "native" && (
            <NativeSection
              sources={data.sources}
              onChanged={() => void mutate()}
              onToast={setToast}
            />
          )}
          {data.mode === "argocd" && (
            <ArgoSection detected={data.argoDetected} onToast={setToast} />
          )}
        </>
      )}
      <Toast toast={toast} onDismiss={() => setToast(null)} />
    </div>
  );
}

// ------------------------------------------------------------------ native

function NativeSection({
  sources,
  onChanged,
  onToast,
}: {
  sources: GitSource[];
  onChanged: () => void;
  onToast: (t: ToastState) => void;
}) {
  const [adding, setAdding] = useState(false);
  const [removing, setRemoving] = useState<GitSource | null>(null);
  const [busy, setBusy] = useState(false);

  async function remove() {
    if (!removing) return;
    setBusy(true);
    try {
      await removeGitSource(removing.id);
      onToast({
        type: "success",
        message: `"${removing.name}" removed. What it applied stays in the cluster.`,
      });
      setRemoving(null);
      onChanged();
    } catch (err) {
      onToast({ type: "error", message: applyErrorMessage(err) });
    } finally {
      setBusy(false);
    }
  }

  return (
    <>
      <div className="flex items-center justify-between">
        <div className="text-[13px] font-semibold">Sources</div>
        <button
          type="button"
          onClick={() => setAdding(true)}
          className="flex items-center gap-1.5 rounded-lg bg-accent px-3.5 py-2 text-[12.5px] font-semibold text-white"
        >
          <PlusIcon />
          Add source
        </button>
      </div>

      {sources.length === 0 ? (
        <div className="rounded-lg border border-border bg-surface p-4 text-sm text-text-faint">
          No sources yet. Add a git repository and Vinaire will apply its
          manifests to this cluster.
        </div>
      ) : (
        <div className="flex flex-col gap-3">
          {sources.map((s) => (
            <SourceCard
              key={s.id}
              source={s}
              onRemove={() => setRemoving(s)}
              onToast={onToast}
              onChanged={onChanged}
            />
          ))}
        </div>
      )}

      {adding && (
        <AddSourceModal onClose={() => setAdding(false)} onAdded={onChanged} />
      )}
      <ConfirmModal
        open={removing !== null}
        title="Remove this source?"
        message={`Vinaire stops syncing "${removing?.name}". Objects it already applied are not deleted from the cluster.`}
        confirmLabel="Remove"
        danger
        busy={busy}
        onConfirm={() => void remove()}
        onCancel={() => setRemoving(null)}
      />
    </>
  );
}

function SourceCard({
  source: s,
  onRemove,
  onToast,
  onChanged,
}: {
  source: GitSource;
  onRemove: () => void;
  onToast: (t: ToastState) => void;
  onChanged: () => void;
}) {
  const [open, setOpen] = useState(false);
  const [syncing, setSyncing] = useState(false);
  const st = s.status;

  async function sync() {
    setSyncing(true);
    try {
      await syncGitSource(s.id);
      onChanged();
    } catch (err) {
      onToast({ type: "error", message: applyErrorMessage(err) });
    } finally {
      setSyncing(false);
    }
  }

  return (
    <div className="rounded-[10px] border border-border bg-surface p-4">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div className="min-w-0">
          <div className="flex items-center gap-2">
            <StatusDot color={phaseColor(st.phase)} />
            <span className="font-mono text-[13px] font-semibold">
              {s.name}
            </span>
            <span className="text-xs text-text-muted">{st.phase}</span>
          </div>
          <div className="mt-1 truncate font-mono text-[11.5px] text-text-muted">
            {s.repoURL}
            <span className="text-text-faint">
              {" "}
              @ {s.revision || "default branch"}
              {s.path ? ` : ${s.path}` : ""}
            </span>
          </div>
        </div>
        <div className="flex gap-2">
          <button
            type="button"
            onClick={() => void sync()}
            disabled={syncing || st.phase === "Syncing"}
            className="rounded-lg border border-border bg-surface-2 px-3 py-1.5 text-xs hover:border-accent disabled:opacity-60"
          >
            Sync now
          </button>
          <button
            type="button"
            onClick={onRemove}
            className="rounded-lg border border-border bg-surface-2 px-3 py-1.5 text-xs text-err"
          >
            Remove
          </button>
        </div>
      </div>

      {st.message && (
        <div
          className={`mt-2 text-xs ${st.phase === "Error" ? "text-err" : "text-text-muted"}`}
        >
          {st.message}
        </div>
      )}
      {s.warnings?.map((w) => (
        <div
          key={w}
          className="mt-2 rounded-md border border-warn/30 bg-surface-2 p-2 text-[11.5px] text-warn"
        >
          {w}
        </div>
      ))}

      <div className="mt-3 flex flex-wrap gap-x-6 gap-y-1 text-[11.5px] text-text-muted">
        <span>
          commit{" "}
          <span className="font-mono">{st.revision?.slice(0, 7) || "—"}</span>
        </span>
        <span>
          applied{" "}
          <span className="font-mono">
            {st.appliedRevision?.slice(0, 7) || "—"}
          </span>
        </span>
        <span>checked {ago(st.checkedAt)}</span>
        <span>last applied {ago(st.syncedAt)}</span>
        <span>
          applies as <span className="font-mono">{s.applyAs || "?"}</span>
        </span>
        <span>
          {s.autoSync ? "auto-sync" : "manual sync"}
          {s.force ? ", takes over fields" : ""} · every {s.intervalSeconds}s
        </span>
      </div>

      {(st.objects.length > 0 || (st.orphans?.length ?? 0) > 0) && (
        <button
          type="button"
          onClick={() => setOpen((o) => !o)}
          className="mt-3 text-xs text-accent-text underline"
        >
          {open ? "Hide" : "Show"} {st.objects.length} object
          {st.objects.length === 1 ? "" : "s"}
          {st.orphans?.length ? ` · ${st.orphans.length} no longer in git` : ""}
        </button>
      )}
      {open && (
        <div className="mt-2 flex flex-col gap-1">
          {st.objects.map((o) => (
            <div
              key={`${o.apiVersion}/${o.kind}/${o.namespace}/${o.name}`}
              className="flex items-start gap-2 text-xs"
            >
              <span className="mt-1">
                <StatusDot
                  color={
                    o.state === "Synced"
                      ? "ok"
                      : o.state === "Error"
                        ? "err"
                        : "warn"
                  }
                />
              </span>
              <span className="font-mono">
                {o.kind}/{o.namespace ? `${o.namespace}/` : ""}
                {o.name}
              </span>
              <span className="text-text-muted">{o.state}</span>
              {o.message && o.message !== "applied" && (
                <span className="min-w-0 flex-1 break-words text-text-faint">
                  — {o.message}
                </span>
              )}
            </div>
          ))}
          {st.orphans?.map((o) => (
            <div
              key={`orphan-${o.kind}/${o.namespace}/${o.name}`}
              className="flex items-start gap-2 text-xs text-text-faint"
            >
              <span className="mt-1">
                <StatusDot color="warn" />
              </span>
              <span className="font-mono">
                {o.kind}/{o.namespace ? `${o.namespace}/` : ""}
                {o.name}
              </span>
              <span>
                removed from git — still in the cluster (native sync never
                deletes)
              </span>
            </div>
          ))}
        </div>
      )}
    </div>
  );
}

// ------------------------------------------------------------------ Argo CD

function ArgoSection({
  detected,
  onToast,
}: {
  detected: boolean;
  onToast: (t: ToastState) => void;
}) {
  const { data, error, isLoading, mutate } = useArgoApplications(true);
  const [creating, setCreating] = useState(false);

  async function sync(a: ArgoApplication) {
    try {
      await syncArgoApplication(a.namespace, a.name);
      onToast({ type: "success", message: `Sync of "${a.name}" requested.` });
      void mutate();
    } catch (err) {
      onToast({ type: "error", message: applyErrorMessage(err) });
    }
  }

  const columns: DataTableColumn<ArgoApplication>[] = [
    {
      header: "Application",
      width: "1.6fr",
      render: (a) => (
        <span className="truncate pr-3 font-mono text-[12.5px]">
          {a.namespace}/{a.name}
        </span>
      ),
    },
    {
      header: "Source",
      width: "2.4fr",
      render: (a) => (
        <span className="truncate pr-3 font-mono text-[11.5px] text-text-muted">
          {a.repoURL}
          {a.path ? ` : ${a.path}` : ""}
          {a.targetRevision ? ` @ ${a.targetRevision}` : ""}
        </span>
      ),
    },
    {
      header: "Sync",
      width: "1fr",
      render: (a) => (
        <span className="flex items-center gap-1.5 text-xs">
          <StatusDot
            color={
              a.syncStatus === "Synced"
                ? "ok"
                : a.syncStatus === "OutOfSync"
                  ? "warn"
                  : "warn"
            }
          />
          {a.syncing ? "Syncing…" : a.syncStatus || "Unknown"}
        </span>
      ),
    },
    {
      header: "Health",
      width: "1fr",
      render: (a) => (
        <span className="flex items-center gap-1.5 text-xs">
          <StatusDot
            color={
              a.health === "Healthy"
                ? "ok"
                : a.health === "Degraded" || a.health === "Missing"
                  ? "err"
                  : "warn"
            }
          />
          {a.health || "—"}
        </span>
      ),
    },
    {
      header: "Revision",
      width: ".8fr",
      render: (a) => (
        <span className="font-mono text-xs text-text-muted">
          {a.revision?.slice(0, 7) || "—"}
        </span>
      ),
    },
    {
      header: "",
      width: ".8fr",
      render: (a) => (
        <span className="flex justify-end">
          <button
            type="button"
            onClick={() => void sync(a)}
            disabled={a.syncing}
            className="rounded-lg border border-border bg-surface-2 px-3 py-1.5 text-xs hover:border-accent disabled:opacity-60"
          >
            Sync now
          </button>
        </span>
      ),
    },
  ];

  return (
    <>
      <div className="flex items-center justify-between">
        <div className="text-[13px] font-semibold">Applications</div>
        {detected && (
          <button
            type="button"
            onClick={() => setCreating(true)}
            className="flex items-center gap-1.5 rounded-lg bg-accent px-3.5 py-2 text-[12.5px] font-semibold text-white"
          >
            <PlusIcon />
            Create Application
          </button>
        )}
      </div>

      {!detected ? (
        <div className="rounded-lg border border-border bg-surface-2 p-4 text-sm text-text-muted">
          Argo CD doesn&apos;t seem to be installed on this cluster (its{" "}
          <span className="font-mono">applications.argoproj.io</span> type
          isn&apos;t served). Install it from the Marketplace (the{" "}
          <span className="font-mono">argo</span> repository), or use Native
          mode instead.
        </div>
      ) : error ? (
        <div className="text-sm text-err">
          Failed to load Applications: {friendlyErrorMessage(error)}
        </div>
      ) : (
        <DataTable
          columns={columns}
          rows={data?.applications ?? []}
          rowKey={(a) => `${a.namespace}/${a.name}`}
          isLoading={isLoading || !data}
          emptyLabel="No Argo CD Applications."
        />
      )}

      {creating && (
        <CreateResourceModal
          templates={argoApplicationTemplates}
          onClose={() => setCreating(false)}
          onCreated={() => void mutate()}
        />
      )}
    </>
  );
}
