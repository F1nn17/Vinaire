"use client";

import Link from "next/link";
import { useRouter, useSearchParams } from "next/navigation";
import { Suspense, useState } from "react";
import { useNamespace } from "@/lib/hooks/useNamespaces";
import { deleteNamespace } from "@/lib/api/namespaces";
import { friendlyErrorMessage } from "@/lib/api/errors";
import { ApiError } from "@/lib/api/http";
import { StatusDot } from "@/components/ui/StatusDot";
import { UnderlineTabs } from "@/components/ui/Tabs";
import { QuotaBar } from "@/components/ui/QuotaBar";
import { ActionBar, ActionButton } from "@/components/ui/ActionBar";
import { TypeToConfirmModal } from "@/components/ui/TypeToConfirmModal";
import { YamlPanel } from "@/components/editor/YamlPanel";
import { CreateResourceModal } from "@/components/editor/CreateResourceModal";
import {
  limitRangeTemplate,
  quotaTemplate,
} from "@/components/editor/templates";
import { TrashIcon } from "@/components/icons";

type Tab = "overview" | "resources" | "yaml";

const tabs: { key: Tab; label: string }[] = [
  { key: "overview", label: "Overview" },
  { key: "resources", label: "Resources" },
  { key: "yaml", label: "YAML" },
];

const resourceLabels: Record<string, string> = {
  pods: "Pods",
  deployments: "Deployments",
  statefulsets: "StatefulSets",
  daemonsets: "DaemonSets",
  jobs: "Jobs",
  services: "Services",
  ingresses: "Ingresses",
  configmaps: "ConfigMaps",
  secrets: "Secrets",
  persistentvolumeclaims: "PVCs",
};

// A static export can't pre-render a page per namespace name (unknowable at
// build time) — this route is a single static page, `/namespaces/detail`,
// that reads which namespace to show from `?name=`. See AGENTS.md's
// "Rendering model".
export default function NamespaceDetailPageRoute() {
  return (
    <Suspense
      fallback={<div className="p-8 text-sm text-text-muted">Loading…</div>}
    >
      <NamespaceDetailPage />
    </Suspense>
  );
}

function NamespaceDetailPage() {
  const router = useRouter();
  const searchParams = useSearchParams();
  const name = searchParams.get("name") ?? "";
  const { data: ns, isLoading, error, mutate } = useNamespace(name);
  const [tab, setTab] = useState<Tab>("overview");
  const [deleteOpen, setDeleteOpen] = useState(false);
  const [deleteBusy, setDeleteBusy] = useState(false);
  const [deleteError, setDeleteError] = useState<string | null>(null);
  const [adding, setAdding] = useState<"quota" | "limitrange" | null>(null);
  const [editing, setEditing] = useState<string | null>(null);

  async function handleDelete() {
    setDeleteBusy(true);
    setDeleteError(null);
    try {
      await deleteNamespace(name);
      router.push("/namespaces");
    } catch (err) {
      setDeleteError(
        err instanceof ApiError ? err.message : friendlyErrorMessage(err),
      );
    } finally {
      setDeleteBusy(false);
    }
  }

  if (error) {
    return (
      <div className="p-8 text-sm text-err">
        Failed to load namespace: {friendlyErrorMessage(error)}
      </div>
    );
  }
  if (isLoading) {
    return <div className="p-8 text-sm text-text-muted">Loading…</div>;
  }
  if (!ns) {
    return (
      <div className="p-8 text-sm text-text-muted">
        Namespace &ldquo;{name}&rdquo; not found.
      </div>
    );
  }

  return (
    <div className="flex flex-col gap-[18px] px-8 py-7">
      <div className="flex items-start justify-between">
        <div>
          <div className="font-mono text-xs text-text-faint">
            <Link href="/namespaces">Namespaces</Link>
          </div>
          <div className="mt-1.5 flex items-center gap-2.5">
            <span className="font-mono text-lg font-semibold">{ns.name}</span>
            <span className="flex items-center gap-1.5 rounded-full border border-border bg-surface px-2.5 py-[3px] text-xs text-text-muted">
              <StatusDot color={ns.statusColor} />
              {ns.status}
            </span>
          </div>
        </div>
        <ActionBar>
          <ActionButton
            icon={<TrashIcon />}
            label={
              ns.protected
                ? "System namespaces can't be deleted from Vinaire"
                : "Delete namespace"
            }
            danger
            disabled={ns.protected || ns.status === "Terminating"}
            onClick={() => {
              setDeleteError(null);
              setDeleteOpen(true);
            }}
          />
        </ActionBar>
      </div>

      <UnderlineTabs items={tabs} active={tab} onChange={setTab} />

      {tab === "overview" && (
        <div className="flex max-w-3xl flex-col gap-4">
          <div className="rounded-[10px] border border-border bg-surface p-5">
            <div className="mb-3.5 text-[13px] font-semibold">Metadata</div>
            <Row label="Age" value={ns.age} />
            <div className="mt-4 border-t border-border pt-4">
              <div className="mb-2 text-xs text-text-muted">Labels</div>
              <div className="flex flex-wrap gap-1.5">
                {Object.entries(ns.labels).map(([k, v]) => (
                  <span
                    key={k}
                    className="rounded-md border border-border bg-surface-2 px-2 py-1 font-mono text-[11px]"
                  >
                    {k}={v}
                  </span>
                ))}
              </div>
            </div>
          </div>

          <div className="rounded-[10px] border border-border bg-surface p-5">
            <div className="mb-3.5 flex items-center justify-between">
              <div className="text-[13px] font-semibold">Resource quotas</div>
              <button
                type="button"
                onClick={() => setAdding("quota")}
                className="rounded-lg border border-border bg-surface-2 px-3 py-1.5 text-xs"
              >
                + Add quota
              </button>
            </div>
            {ns.quotas.length === 0 ? (
              <div className="text-xs text-text-muted">
                No ResourceQuota — pods here aren&apos;t limited in aggregate.
              </div>
            ) : (
              <div className="flex flex-col gap-4">
                {ns.quotas.map((q) => (
                  <div key={q.name} className="flex flex-col gap-3">
                    <div className="flex items-center justify-between">
                      <span className="font-mono text-xs">{q.name}</span>
                      <button
                        type="button"
                        onClick={() =>
                          setEditing(
                            editing === `q:${q.name}` ? null : `q:${q.name}`,
                          )
                        }
                        className="text-xs text-accent-text"
                      >
                        {editing === `q:${q.name}` ? "Hide YAML" : "Edit YAML"}
                      </button>
                    </div>
                    <div className="flex flex-wrap gap-x-6 gap-y-3">
                      {q.items.map((i) => (
                        <QuotaBar key={i.resource} row={i} wide />
                      ))}
                    </div>
                    {editing === `q:${q.name}` && (
                      <YamlPanel yaml={q.yaml} onApplied={() => mutate()} />
                    )}
                  </div>
                ))}
              </div>
            )}
          </div>

          <div className="rounded-[10px] border border-border bg-surface p-5">
            <div className="mb-3.5 flex items-center justify-between">
              <div className="text-[13px] font-semibold">Limit ranges</div>
              <button
                type="button"
                onClick={() => setAdding("limitrange")}
                className="rounded-lg border border-border bg-surface-2 px-3 py-1.5 text-xs"
              >
                + Add limit range
              </button>
            </div>
            {ns.limitRanges.length === 0 ? (
              <div className="text-xs text-text-muted">No LimitRange.</div>
            ) : (
              <div className="flex flex-col gap-4">
                {ns.limitRanges.map((lr) => (
                  <div key={lr.name} className="flex flex-col gap-3">
                    <div className="flex items-center justify-between">
                      <span className="font-mono text-xs">{lr.name}</span>
                      <button
                        type="button"
                        onClick={() =>
                          setEditing(
                            editing === `l:${lr.name}` ? null : `l:${lr.name}`,
                          )
                        }
                        className="text-xs text-accent-text"
                      >
                        {editing === `l:${lr.name}` ? "Hide YAML" : "Edit YAML"}
                      </button>
                    </div>
                    <div className="grid grid-cols-[1fr_1fr_1fr_1fr_1fr_1fr] gap-2 font-mono text-[11px]">
                      {[
                        "Type",
                        "Resource",
                        "Min",
                        "Max",
                        "Default",
                        "Request",
                      ].map((h) => (
                        <div key={h} className="text-text-muted">
                          {h}
                        </div>
                      ))}
                      {lr.limits.flatMap((l, i) => [
                        <div key={`${i}t`}>{l.type}</div>,
                        <div key={`${i}r`}>{l.resource}</div>,
                        <div key={`${i}a`}>{l.min || "—"}</div>,
                        <div key={`${i}b`}>{l.max || "—"}</div>,
                        <div key={`${i}c`}>{l.default || "—"}</div>,
                        <div key={`${i}d`}>{l.defaultRequest || "—"}</div>,
                      ])}
                    </div>
                    {editing === `l:${lr.name}` && (
                      <YamlPanel yaml={lr.yaml} onApplied={() => mutate()} />
                    )}
                  </div>
                ))}
              </div>
            )}
          </div>
        </div>
      )}

      {tab === "resources" && (
        <div className="grid max-w-3xl grid-cols-2 gap-3 sm:grid-cols-3">
          {Object.entries(resourceLabels).map(([key, label]) => (
            <div
              key={key}
              className="rounded-[10px] border border-border bg-surface p-4"
            >
              <div className="text-xs text-text-muted">{label}</div>
              <div className="mt-1 font-mono text-xl font-semibold">
                {ns.resources[key] ?? "—"}
              </div>
            </div>
          ))}
        </div>
      )}

      {tab === "yaml" && (
        <YamlPanel yaml={ns.yaml} onApplied={() => mutate()} />
      )}

      {adding && (
        <CreateResourceModal
          templates={[
            adding === "quota"
              ? quotaTemplate(ns.name)
              : limitRangeTemplate(ns.name),
          ]}
          onClose={() => setAdding(null)}
          onCreated={() => void mutate()}
        />
      )}

      {deleteOpen && (
        <TypeToConfirmModal
          title={`Delete namespace ${ns.name}?`}
          message="This permanently deletes the namespace and everything inside it — workloads, ConfigMaps, Secrets, volumes claims. It can't be undone."
          expected={ns.name}
          confirmLabel="Delete namespace"
          busy={deleteBusy}
          error={deleteError}
          onConfirm={() => void handleDelete()}
          onCancel={() => setDeleteOpen(false)}
        />
      )}
    </div>
  );
}

function Row({ label, value }: { label: string; value: string }) {
  return (
    <div className="flex justify-between">
      <span className="text-xs text-text-muted">{label}</span>
      <span className="font-mono text-xs">{value}</span>
    </div>
  );
}
