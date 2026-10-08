"use client";

import { t } from "@/lib/i18n/translate";
import { useState } from "react";
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

export default function WorkloadsPage() {
  const [tab, setTab] = useState<WorkloadKind>("deployments");
  const [namespace, setNamespace] = useState("");
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
