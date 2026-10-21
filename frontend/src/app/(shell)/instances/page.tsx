"use client";

import { useState } from "react";
import { useInstances, type Instance } from "@/lib/api/instances";
import { friendlyErrorMessage } from "@/lib/api/errors";
import { DataTable, type DataTableColumn } from "@/components/ui/DataTable";
import { UpgradeInstanceModal } from "@/components/instances/UpgradeInstanceModal";
import { UpgradeModal } from "@/components/marketplace/UpgradeModal";
import { useReleases } from "@/lib/hooks/useCatalog";
import { BackupsModal } from "@/components/instances/BackupsModal";
import { DeleteInstanceModal } from "@/components/instances/DeleteInstanceModal";

const mono = "font-mono text-xs text-text-muted";

export default function InstancesPage() {
  const { data, isLoading, error, mutate } = useInstances();
  const [open, setOpen] = useState<Instance | null>(null);
  const [deleting, setDeleting] = useState<Instance | null>(null);
  const [backing, setBacking] = useState<Instance | null>(null);
  const [upgrading, setUpgrading] = useState<Instance | null>(null);
  const { data: releases } = useReleases();
  const release =
    upgrading?.origin === "helm"
      ? (releases ?? []).find(
          (r) =>
            r.name === upgrading.name && r.namespace === upgrading.namespace,
        )
      : undefined;

  const columns: DataTableColumn<Instance>[] = [
    {
      header: "Instance",
      width: "1.4fr",
      render: (r) => (
        <span className="truncate pr-3 font-mono text-[12.5px]">{r.name}</span>
      ),
    },
    {
      header: "Namespace",
      width: "1fr",
      render: (r) => <span className={mono}>{r.namespace}</span>,
    },
    {
      header: "Application",
      width: "1fr",
      render: (r) => <span className={mono}>{r.app ?? "—"}</span>,
    },
    {
      header: "Installed with",
      width: ".8fr",
      render: (r) => (
        <span className={mono}>
          {r.origin === "helm" ? "Marketplace (Helm)" : "Image"}
        </span>
      ),
    },
    {
      header: "Version",
      width: ".7fr",
      render: (r) => <span className={mono}>{r.version ?? "—"}</span>,
    },
    {
      header: "Objects",
      width: ".6fr",
      sortValue: (r) => r.parts.length,
      render: (r) => <span className={mono}>{r.parts.length}</span>,
    },
    {
      header: "",
      width: "1.9fr",
      render: (r) => (
        <span className="flex justify-end gap-2 pr-2">
          <button
            type="button"
            onClick={(e) => {
              e.stopPropagation();
              setOpen(r);
            }}
            className="rounded-lg border border-border bg-surface-2 px-2.5 py-1 text-xs"
          >
            What is in it
          </button>
          <button
            type="button"
            disabled={r.system}
            onClick={(e) => {
              e.stopPropagation();
              setUpgrading(r);
            }}
            className="rounded-lg border border-border bg-surface-2 px-2.5 py-1 text-xs disabled:opacity-40"
          >
            Version
          </button>
          <button
            type="button"
            disabled={r.origin !== "image"}
            title={
              r.origin !== "image"
                ? "Backups are for applications installed from an image"
                : ""
            }
            onClick={(e) => {
              e.stopPropagation();
              setBacking(r);
            }}
            className="rounded-lg border border-border bg-surface-2 px-2.5 py-1 text-xs disabled:opacity-40"
          >
            Backups
          </button>
          <button
            type="button"
            disabled={r.system}
            title={r.system ? "Cluster namespaces are not deleted here" : ""}
            onClick={(e) => {
              e.stopPropagation();
              setDeleting(r);
            }}
            className="rounded-lg border border-border bg-surface-2 px-2.5 py-1 text-xs text-err disabled:opacity-40"
          >
            Delete
          </button>
        </span>
      ),
    },
  ];

  return (
    <div className="flex flex-col gap-[18px] px-8 py-7">
      <div>
        <div className="text-[15px] font-semibold">Instances</div>
        <div className="mt-1 max-w-3xl text-xs text-text-muted">
          Applications installed from the Marketplace or from an image, with
          everything that belongs to each. Deleting one shows what goes and what
          stays — volume claims always stay unless you choose them.
        </div>
      </div>

      {data && data.unreadable.length > 0 && (
        <div className="rounded-lg border border-border bg-surface-2 p-3 text-xs text-text-muted">
          You may not list {data.unreadable.join(", ")}, so those objects are
          missing from the instances below.
        </div>
      )}

      {error ? (
        <div className="text-sm text-err">
          Failed to load instances: {friendlyErrorMessage(error)}
        </div>
      ) : (
        <DataTable
          columns={columns}
          rows={data?.instances ?? []}
          rowKey={(r) => `${r.namespace}/${r.origin}/${r.name}`}
          isLoading={isLoading || !data}
          emptyLabel="No instances yet — install an application from the Marketplace."
        />
      )}

      {open && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 p-4">
          <div className="max-h-[90vh] w-full max-w-xl overflow-auto rounded-xl border border-border bg-surface p-5">
            <div className="text-sm font-semibold">{open.name}</div>
            <div className="mt-3 flex flex-col gap-1.5">
              {open.parts.map((p) => (
                <div
                  key={`${p.kind}/${p.name}`}
                  className="flex items-baseline justify-between gap-3 text-xs"
                >
                  <span className="font-mono">
                    {p.kind} {p.name}
                    {p.detail ? (
                      <span className="text-text-muted"> · {p.detail}</span>
                    ) : null}
                  </span>
                  <span
                    className={
                      p.fate === "stays" ? "text-warn" : "text-text-muted"
                    }
                    title={p.why}
                  >
                    {p.fate === "stays" ? "stays" : "removed"}
                  </span>
                </div>
              ))}
            </div>
            <div className="mt-5 flex justify-end">
              <button
                type="button"
                onClick={() => setOpen(null)}
                className="rounded-lg border border-border bg-surface-2 px-3.5 py-2 text-[12.5px]"
              >
                Close
              </button>
            </div>
          </div>
        </div>
      )}

      {upgrading && upgrading.origin === "image" && (
        <UpgradeInstanceModal
          instance={upgrading}
          onClose={() => setUpgrading(null)}
          onDone={() => {
            setUpgrading(null);
            void mutate();
          }}
        />
      )}
      {upgrading && release && (
        <UpgradeModal
          release={release}
          onClose={() => setUpgrading(null)}
          onDone={() => void mutate()}
        />
      )}

      {backing && (
        <BackupsModal instance={backing} onClose={() => setBacking(null)} />
      )}

      {deleting && (
        <DeleteInstanceModal
          instance={deleting}
          onClose={() => setDeleting(null)}
          onDone={() => {
            setDeleting(null);
            void mutate();
          }}
        />
      )}
    </div>
  );
}
