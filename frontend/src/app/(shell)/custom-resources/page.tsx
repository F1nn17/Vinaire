"use client";

import { useSearchParams } from "next/navigation";
import { Suspense, useState } from "react";
import { useCRDInstances, useCRDs } from "@/lib/hooks/useCRDs";
import type { CRDInstanceRow } from "@/lib/types";
import { DataTable, type DataTableColumn } from "@/components/ui/DataTable";

// A static export can't pre-render a page per CRD (unknowable at build
// time) — this route is a single static page, `/custom-resources`, that
// reads which CRD to show from `?crd=` instead of a route segment. See
// AGENTS.md's "Rendering model".
export default function CustomResourcesPageRoute() {
  return (
    <Suspense
      fallback={<div className="p-8 text-sm text-text-muted">Loading…</div>}
    >
      <CustomResourcesPage />
    </Suspense>
  );
}

function CustomResourcesPage() {
  const searchParams = useSearchParams();
  const crdName = searchParams.get("crd") ?? "";
  const [namespace, setNamespace] = useState("");

  const { data: crds } = useCRDs();
  const crd = crds?.find((c) => c.name === crdName);
  const instances = useCRDInstances(crdName, namespace, !!crdName);

  const columns: DataTableColumn<CRDInstanceRow>[] = [
    {
      header: "Name",
      width: crd?.namespaced ? "2fr" : "3fr",
      render: (r) => (
        <span className="truncate pr-3 font-mono text-[12.5px]">{r.name}</span>
      ),
    },
    ...(crd?.namespaced
      ? [
          {
            header: "Namespace",
            width: "1.2fr",
            render: (r: CRDInstanceRow) => (
              <span className="font-mono text-xs text-text-muted">
                {r.namespace}
              </span>
            ),
          } satisfies DataTableColumn<CRDInstanceRow>,
        ]
      : []),
    {
      header: "Age",
      width: ".8fr",
      render: (r) => (
        <span className="font-mono text-xs text-text-muted">{r.age}</span>
      ),
    },
  ];

  if (!crdName) {
    return (
      <div className="flex flex-col gap-[18px] px-8 py-7">
        <div className="text-[15px] font-semibold">Custom Resources</div>
        <div className="text-sm text-text-muted">
          Select a Custom Resource from the sidebar to see its real instances.
        </div>
      </div>
    );
  }

  return (
    <div className="flex flex-col gap-[18px] px-8 py-7">
      <div>
        <div className="text-[15px] font-semibold">{crd?.kind ?? crdName}</div>
        <div className="mt-1 font-mono text-[11px] text-text-faint">
          {crd ? `${crd.group}/${crd.version}` : crdName}
        </div>
      </div>

      {crd?.namespaced && (
        <label className="flex w-fit items-center gap-2">
          <span className="sr-only">Filter by namespace</span>
          <input
            type="text"
            value={namespace}
            onChange={(e) => setNamespace(e.target.value)}
            placeholder="All namespaces"
            className="rounded-lg border border-border bg-surface px-3 py-1.75 font-mono text-xs outline-none placeholder:text-text-faint"
          />
        </label>
      )}

      {instances.error ? (
        <div className="text-sm text-err">
          Failed to load instances: {instances.error.message}
        </div>
      ) : instances.isLoading || !instances.data ? (
        <div className="text-sm text-text-muted">Loading…</div>
      ) : (
        <DataTable
          columns={columns}
          rows={instances.data}
          rowKey={(r) => `${r.namespace}/${r.name}`}
          rowHref={(r) =>
            `/custom-resources/detail?crd=${encodeURIComponent(crdName)}&namespace=${encodeURIComponent(r.namespace)}&name=${encodeURIComponent(r.name)}`
          }
          emptyLabel="No instances found."
        />
      )}
    </div>
  );
}
