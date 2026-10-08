"use client";

import { useRouter } from "next/navigation";
import { useState } from "react";
import { useProjects, type ProjectRow } from "@/lib/api/projects";
import { friendlyErrorMessage } from "@/lib/api/errors";
import { DataTable, type DataTableColumn } from "@/components/ui/DataTable";
import { QuotaBar } from "@/components/ui/QuotaBar";
import { CreateProjectModal } from "@/components/projects/CreateProjectModal";
import { PlusIcon } from "@/components/icons";

const columns: DataTableColumn<ProjectRow>[] = [
  {
    header: "Project",
    width: "1.3fr",
    render: (r) => (
      <span className="truncate pr-3 font-mono text-[12.5px]">{r.name}</span>
    ),
  },
  {
    header: "Namespaces",
    width: "2fr",
    sortValue: (r) => r.namespaces.length,
    render: (r) => (
      <span className="flex flex-wrap gap-1 py-1">
        {r.namespaces.map((n) => (
          <span
            key={n}
            className="rounded bg-surface-2 px-1.5 py-0.5 font-mono text-[11px] text-text-muted"
          >
            {n}
          </span>
        ))}
      </span>
    ),
  },
  {
    header: "Pods",
    width: ".6fr",
    render: (r) => (
      <span className="font-mono text-xs text-text-muted">{r.pods ?? "—"}</span>
    ),
  },
  {
    header: "Members",
    width: ".7fr",
    render: (r) => (
      <span className="font-mono text-xs text-text-muted">{r.members}</span>
    ),
  },
  {
    header: "Quota usage (whole project)",
    width: "2.4fr",
    render: (r) =>
      r.quota.length === 0 ? (
        <span className="text-xs text-text-faint">No quota</span>
      ) : (
        <div className="flex flex-wrap gap-x-4 gap-y-1.5 py-1">
          {r.quota.map((q) => (
            <QuotaBar key={q.resource} row={q} />
          ))}
        </div>
      ),
  },
];

export default function ProjectsPage() {
  const router = useRouter();
  const { data, isLoading, error, mutate } = useProjects();
  const [createOpen, setCreateOpen] = useState(false);

  return (
    <div className="flex flex-col gap-[18px] px-8 py-7">
      <div className="flex items-center justify-between">
        <div>
          <div className="text-[15px] font-semibold">Projects</div>
          <div className="mt-1 max-w-3xl text-xs text-text-muted">
            A project groups namespaces (the label{" "}
            <code className="font-mono">vinaire.io/project</code>) so quotas and
            people are managed together. It is kept in the cluster itself.
          </div>
        </div>
        <button
          type="button"
          onClick={() => setCreateOpen(true)}
          className="flex items-center gap-1.5 rounded-lg bg-accent px-3.5 py-2 text-[12.5px] font-semibold text-white"
        >
          <PlusIcon />
          Create project
        </button>
      </div>

      {error ? (
        <div className="text-sm text-err">
          Failed to load projects: {friendlyErrorMessage(error)}
        </div>
      ) : (
        <DataTable
          columns={columns}
          rows={data ?? []}
          rowKey={(r) => r.name}
          rowHref={(r) => r.href}
          isLoading={isLoading || !data}
          emptyLabel="No projects yet — create one to group namespaces."
        />
      )}

      {createOpen && (
        <CreateProjectModal
          onClose={() => setCreateOpen(false)}
          onCreated={(name) => {
            void mutate();
            router.prefetch(`/projects/detail?name=${name}`);
          }}
        />
      )}
    </div>
  );
}
