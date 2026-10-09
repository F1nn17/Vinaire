"use client";

import { t } from "@/lib/i18n/translate";
import { Suspense, useState } from "react";
import { useRouter, useSearchParams } from "next/navigation";
import { useRememberedState } from "@/lib/hooks/useRememberedState";
import { useWorkloads } from "@/lib/hooks/useWorkloads";
import { useProjects } from "@/lib/api/projects";
import { useWorkloadsStream } from "@/lib/hooks/useWorkloadsStream";
import { friendlyErrorMessage } from "@/lib/api/errors";
import { workloadTabs } from "@/lib/mock/workloads";
import type { WorkloadKind, WorkloadRow } from "@/lib/types";
import { DataTable, type DataTableColumn } from "@/components/ui/DataTable";
import { PillTabs } from "@/components/ui/Tabs";
import { StatusDot } from "@/components/ui/StatusDot";
import { CreateResourceModal } from "@/components/editor/CreateResourceModal";
import { workloadTemplates } from "@/components/editor/templates";
import { PlusIcon } from "@/components/icons";
import { BulkBar } from "@/components/workloads/BulkActions";
import { BULK_MAX } from "@/lib/api/bulk";

const columns: DataTableColumn<WorkloadRow>[] = [
  {
    header: "Name",
    width: "2.4fr",
    render: (r) => (
      <span className="truncate pr-3 font-mono text-[12.5px]">{r.name}</span>
    ),
  },
  {
    header: "Namespace",
    width: "1fr",
    render: (r) => (
      <span className="font-mono text-xs text-text-muted">{r.namespace}</span>
    ),
  },
  {
    header: "Ready",
    width: ".8fr",
    render: (r) => <span className="font-mono text-xs">{r.ready}</span>,
  },
  {
    header: "Status",
    width: "1.1fr",
    render: (r) => (
      <span className="flex items-center gap-1.5 text-xs">
        <StatusDot color={r.statusColor} />
        {r.status}
      </span>
    ),
  },
  {
    header: "Restarts",
    width: ".7fr",
    render: (r) => (
      <span className="font-mono text-xs text-text-muted">{r.restarts}</span>
    ),
  },
  {
    header: "Age",
    width: ".7fr",
    render: (r) => (
      <span className="font-mono text-xs text-text-muted">{r.age}</span>
    ),
  },
];

const kinds: WorkloadKind[] = [
  "deployments",
  "pods",
  "statefulsets",
  "daemonsets",
  "jobs",
];

export default function WorkloadsPage() {
  return (
    <Suspense
      fallback={<div className="p-8 text-sm text-text-muted">Loading…</div>}
    >
      <WorkloadsInner />
    </Suspense>
  );
}

// A link may open the list on a namespace and a tab (?namespace=app&tab=pods).
function WorkloadsInner() {
  const params = useSearchParams();
  const router = useRouter();
  const linkedTab = params.get("tab") as WorkloadKind | null;
  const linkedNs = params.get("namespace") ?? "";
  // the tab and the namespace filter are remembered, so coming back from a detail page lands
  // where the user was; a link that names them wins
  const [rememberedTab, rememberTab] = useRememberedState<WorkloadKind>(
    "workloads-tab",
    "deployments",
  );
  const [rememberedNs, rememberNs] = useRememberedState("workloads-ns", "");
  const tab: WorkloadKind =
    linkedTab && kinds.includes(linkedTab) ? linkedTab : rememberedTab;
  const setTab = (k: WorkloadKind) => {
    rememberTab(k);
    router.replace(`/workloads?tab=${k}`);
  };
  const namespace = /^[a-z0-9.-]+$/.test(linkedNs) ? linkedNs : rememberedNs;
  const setNamespace = (v: string) => {
    rememberNs(v);
    if (linkedNs) router.replace(`/workloads?tab=${tab}`);
  };
  const { data, isLoading, error, mutate } = useWorkloads(tab, namespace);
  const [createOpen, setCreateOpen] = useState(false);
  // Roadmap Phase 53: narrow the list to the namespaces of one project
  const [project, setProject] = useState("");
  const { data: projects } = useProjects();
  const projectNamespaces = projects?.find(
    (p) => p.name === project,
  )?.namespaces;
  const rows = projectNamespaces
    ? (data ?? []).filter((r) => projectNamespaces.includes(r.namespace))
    : (data ?? []);
  useWorkloadsStream(tab, namespace);

  // Roadmap Phase 58: tick rows to restart / delete several at once, on every tab; the
  // selection belongs to one tab.
  const bulkKind = tab;
  const [picked, setPicked] = useState<{
    tab: WorkloadKind;
    keys: Set<string>;
  }>({ tab: "deployments", keys: new Set() });
  const selected = picked.tab === tab ? picked.keys : new Set<string>();
  const selectedItems = rows
    .filter((r) => selected.has(`${r.namespace}/${r.name}`))
    .map((r) => ({ namespace: r.namespace, name: r.name }));

  return (
    <div className="flex flex-col gap-[18px] px-8 py-7">
      <div className="flex items-center justify-between">
        <div className="text-[15px] font-semibold">Workloads</div>
        <button
          type="button"
          onClick={() => setCreateOpen(true)}
          className="flex items-center gap-1.5 rounded-lg bg-accent px-3.5 py-2 text-[12.5px] font-semibold text-white"
        >
          <PlusIcon />
          Create
        </button>
      </div>

      <div className="flex items-center justify-between gap-3">
        <PillTabs items={workloadTabs} active={tab} onChange={setTab} />

        <div className="flex items-center gap-2">
          {(projects?.length ?? 0) > 0 && (
            <label className="flex items-center gap-2">
              <span className="sr-only">Filter by project</span>
              <select
                aria-label="Filter by project"
                value={project}
                onChange={(e) => setProject(e.target.value)}
                className="rounded-lg border border-border bg-surface px-3 py-1.75 font-mono text-xs outline-none"
              >
                <option value="">All projects</option>
                {projects!.map((p) => (
                  <option key={p.name} value={p.name}>
                    {p.name}
                  </option>
                ))}
              </select>
            </label>
          )}
          <label className="flex items-center gap-2">
            <span className="sr-only">Filter by namespace</span>
            <input
              type="text"
              value={namespace}
              onChange={(e) => setNamespace(e.target.value)}
              placeholder="All namespaces"
              className="rounded-lg border border-border bg-surface px-3 py-1.75 font-mono text-xs outline-none placeholder:text-text-faint"
            />
          </label>
        </div>
      </div>

      {bulkKind && (
        <BulkBar
          kind={bulkKind}
          items={selectedItems}
          onClear={() => setPicked({ tab, keys: new Set() })}
          onFinished={(done) => {
            const left = new Set(selected);
            for (const k of done) left.delete(k);
            setPicked({ tab, keys: left });
            void mutate();
          }}
        />
      )}

      {error ? (
        <div className="text-sm text-err">
          Failed to load {tab}: {friendlyErrorMessage(error)}
        </div>
      ) : (
        <DataTable
          columns={columns}
          rows={rows}
          rowKey={(r) => `${r.namespace}/${r.name}`}
          rowHref={(r) => r.href}
          isLoading={isLoading || !data}
          selection={
            bulkKind
              ? {
                  selected,
                  onChange: (keys) => setPicked({ tab, keys }),
                  max: BULK_MAX,
                }
              : undefined
          }
          emptyLabel={
            namespace
              ? t('No {tab} found in namespace "{ns}".', { tab, ns: namespace })
              : t("No {tab} found.", { tab })
          }
        />
      )}

      {createOpen && (
        <CreateResourceModal
          templates={workloadTemplates[tab] ?? []}
          onClose={() => setCreateOpen(false)}
          onCreated={() => void mutate()}
        />
      )}
    </div>
  );
}
