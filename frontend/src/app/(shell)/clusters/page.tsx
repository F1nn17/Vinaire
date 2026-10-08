"use client";

import { useRouter, useSearchParams } from "next/navigation";
import { Suspense, useState } from "react";
import { t } from "@/lib/i18n/translate";
import { useClusters } from "@/lib/hooks/useClusters";
import {
  removeCluster,
  rotateClusterCredentials,
  setClusterType,
} from "@/lib/api/clusters";
import { setCurrentCluster } from "@/lib/clusterStore";
import { friendlyErrorMessage } from "@/lib/api/errors";
import type { Cluster, ClusterType } from "@/lib/types";
import { DataTable, type DataTableColumn } from "@/components/ui/DataTable";
import { ConfirmModal } from "@/components/ui/ConfirmModal";
import { Toast, type ToastState } from "@/components/ui/Toast";
import { StatusDot } from "@/components/ui/StatusDot";
import { AddClusterWizard } from "@/components/clusters/AddClusterWizard";
import {
  clusterTypeLabels,
  clusterTypes,
} from "@/components/clusters/ClusterTypeBadge";
import { PlusIcon } from "@/components/icons";

// The Clusters page (Roadmap Phase 34): every cluster Vinaire knows, whether
// it is reachable, what kind it is (correctable), and the wizard for adding
// more. Removing a cluster only removes Vinaire's own connection to it.
export default function ClustersPageRoute() {
  return (
    <Suspense
      fallback={<div className="p-8 text-sm text-text-muted">Loading…</div>}
    >
      <ClustersPage />
    </Suspense>
  );
}

function ClustersPage() {
  const router = useRouter();
  const searchParams = useSearchParams();
  const { data, isLoading, error, mutate } = useClusters();
  const [wizardOpen, setWizardOpen] = useState(searchParams.get("add") === "1");
  const [pendingRemove, setPendingRemove] = useState<Cluster | null>(null);
  const [removing, setRemoving] = useState(false);
  const [rotating, setRotating] = useState<Cluster | null>(null);
  const [newToken, setNewToken] = useState("");
  const [rotateBusy, setRotateBusy] = useState(false);
  const [rotateError, setRotateError] = useState<string | null>(null);
  const [toast, setToast] = useState<ToastState>(null);

  async function changeType(c: Cluster, type: ClusterType) {
    try {
      await setClusterType(c.name, type);
      await mutate();
    } catch (err) {
      setToast({ type: "error", message: friendlyErrorMessage(err) });
    }
  }

  async function handleRotate() {
    if (!rotating || !newToken.trim()) return;
    setRotateBusy(true);
    setRotateError(null);
    try {
      await rotateClusterCredentials(rotating.name, newToken.trim());
      setToast({
        type: "success",
        message: t('The token for "{name}" was replaced.', {
          name: rotating.name,
        }),
      });
      setRotating(null);
      setNewToken("");
      await mutate();
    } catch (err) {
      setRotateError(friendlyErrorMessage(err));
    } finally {
      setRotateBusy(false);
    }
  }

  async function handleRemove() {
    if (!pendingRemove) return;
    setRemoving(true);
    try {
      await removeCluster(pendingRemove.name);
      setToast({
        type: "success",
        message: `"${pendingRemove.name}" removed from Vinaire.`,
      });
      setPendingRemove(null);
      await mutate();
    } catch (err) {
      setToast({ type: "error", message: friendlyErrorMessage(err) });
    } finally {
      setRemoving(false);
    }
  }

  const columns: DataTableColumn<Cluster>[] = [
    {
      header: "Name",
      width: "1.6fr",
      render: (c) => (
        <span className="flex items-center gap-2 pr-3">
          <span className="truncate font-mono text-[12.5px]">{c.name}</span>
          {c.default && (
            <span className="text-[11px] text-text-faint">default</span>
          )}
        </span>
      ),
    },
    {
      header: "Type",
      width: "1.2fr",
      render: (c) => (
        <select
          aria-label={`Type of ${c.name}`}
          value={c.type}
          onChange={(e) => void changeType(c, e.target.value as ClusterType)}
          className="rounded-md border border-border bg-surface-2 px-2 py-1 font-mono text-[11.5px]"
        >
          {clusterTypes.map((t) => (
            <option key={t} value={t}>
              {clusterTypeLabels[t]}
            </option>
          ))}
        </select>
      ),
    },
    {
      header: "Status",
      width: "1fr",
      render: (c) => (
        <span className="flex items-center gap-1.5 text-xs">
          <StatusDot color={c.status === "connected" ? "ok" : "err"} />
          {c.status}
          {c.credential && (
            <span
              className="rounded bg-surface-2 px-1.5 py-0.5 text-[10px] text-warn"
              title={
                c.credential === "no-expiry"
                  ? "This cluster's token never expires — if it leaks, it stays valid. Replace it with a short-lived one."
                  : c.credential === "expiring"
                    ? `The token expires ${c.credentialExpiresAt ? new Date(c.credentialExpiresAt).toLocaleString() : "soon"} — replace it before then.`
                    : "The token has expired — replace it."
              }
            >
              {c.credential === "no-expiry"
                ? "token never expires"
                : c.credential === "expiring"
                  ? "token expires soon"
                  : "token expired"}
            </span>
          )}
        </span>
      ),
    },
    {
      header: "",
      width: "2.2fr",
      render: (c) => (
        <span className="flex justify-end gap-2">
          <button
            type="button"
            onClick={() => {
              setCurrentCluster(c.name);
              router.push("/gitops");
            }}
            className="rounded-lg border border-border bg-surface-2 px-3 py-1.5 text-xs"
          >
            GitOps
          </button>
          <button
            type="button"
            onClick={() => {
              setCurrentCluster(c.name);
              router.push("/workloads");
            }}
            className="rounded-lg border border-border bg-surface-2 px-3 py-1.5 text-xs"
          >
            Open
          </button>
          {c.removable && (
            <button
              type="button"
              onClick={() => {
                setRotateError(null);
                setNewToken("");
                setRotating(c);
              }}
              className="rounded-lg border border-border bg-surface-2 px-3 py-1.5 text-xs"
            >
              Replace token
            </button>
          )}
          {c.removable ? (
            <button
              type="button"
              onClick={() => setPendingRemove(c)}
              className="rounded-lg border border-border bg-surface-2 px-3 py-1.5 text-xs text-err"
            >
              Remove
            </button>
          ) : (
            <span className="px-1 py-1.5 text-[11px] text-text-faint">
              configured by environment
            </span>
          )}
        </span>
      ),
    },
  ];

  return (
    <div className="flex flex-col gap-[18px] px-8 py-7">
      <div className="flex items-center justify-between">
        <div className="text-[15px] font-semibold">Clusters</div>
        <button
          type="button"
          onClick={() => setWizardOpen(true)}
          className="flex items-center gap-1.5 rounded-lg bg-accent px-3.5 py-2 text-[12.5px] font-semibold text-white"
        >
          <PlusIcon />
          Add cluster
        </button>
      </div>

      {error ? (
        <div className="text-sm text-err">
          Failed to load clusters: {friendlyErrorMessage(error)}
        </div>
      ) : (
        <DataTable
          columns={columns}
          rows={data ?? []}
          rowKey={(c) => c.name}
          isLoading={isLoading || !data}
          emptyLabel="No clusters configured."
        />
      )}

      {wizardOpen && (
        <AddClusterWizard
          onClose={() => setWizardOpen(false)}
          onAdded={() => void mutate()}
          onAddNodes={(name) => {
            setCurrentCluster(name);
            router.push("/nodes");
          }}
        />
      )}

      {rotating && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 p-4">
          <div className="w-full max-w-lg rounded-xl border border-border bg-surface p-5">
            <div className="text-sm font-semibold">
              Replace the token for &ldquo;{rotating.name}&rdquo;
            </div>
            <div className="mt-1 text-xs text-text-muted">
              Paste a new bearer token for the same cluster. Vinaire checks it
              against the cluster before saving, then forgets the old one.
              Prefer a short-lived one:{" "}
              <code className="font-mono">
                kubectl create token &lt;serviceaccount&gt; --duration=720h
              </code>
              .
            </div>
            <textarea
              aria-label="New token"
              value={newToken}
              onChange={(e) => setNewToken(e.target.value)}
              rows={4}
              autoComplete="off"
              className="mt-3 w-full resize-none rounded-lg border border-border bg-surface-2 p-3 font-mono text-xs outline-none focus:border-accent"
            />
            {rotateError && (
              <div className="mt-2 text-xs text-err">{rotateError}</div>
            )}
            <div className="mt-4 flex justify-end gap-2">
              <button
                type="button"
                onClick={() => {
                  setRotating(null);
                  setNewToken("");
                }}
                className="rounded-lg border border-border px-3 py-1.5 text-xs"
              >
                Cancel
              </button>
              <button
                type="button"
                disabled={rotateBusy || !newToken.trim()}
                onClick={() => void handleRotate()}
                className="rounded-lg bg-accent px-4 py-1.5 text-[12.5px] font-semibold text-white disabled:opacity-50"
              >
                {rotateBusy ? "Checking…" : "Replace token"}
              </button>
            </div>
          </div>
        </div>
      )}

      <ConfirmModal
        open={pendingRemove !== null}
        title="Remove cluster from Vinaire?"
        message={`This only removes Vinaire's connection to "${pendingRemove?.name}" — the cluster itself is untouched and keeps running.`}
        confirmLabel="Remove"
        danger
        busy={removing}
        onConfirm={() => void handleRemove()}
        onCancel={() => setPendingRemove(null)}
      />
      <Toast toast={toast} onDismiss={() => setToast(null)} />
    </div>
  );
}
