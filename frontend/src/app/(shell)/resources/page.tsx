"use client";

import { NamespaceSelect } from "@/components/ui/NamespaceSelect";
import Link from "next/link";
import { useRouter, useSearchParams } from "next/navigation";
import { Fragment, Suspense, useMemo, useState } from "react";
import { friendlyErrorMessage } from "@/lib/api/errors";
import {
  parseResourceKey,
  resourceKey,
  useAPIResources,
  useResourceTable,
  type APIResource,
} from "@/lib/api/allResources";
import { compareCells } from "@/components/ui/DataTable";
import { FillHeight } from "@/components/ui/FillHeight";
import { ListSkeleton } from "@/components/ui/Skeleton";

// The kinds most people look for first — shown as shortcuts when the cluster has them.
const shortcuts: { key: string; label: string }[] = [
  { key: "core/v1/events", label: "Events" },
  { key: "batch/v1/cronjobs", label: "CronJobs" },
  { key: "apps/v1/replicasets", label: "ReplicaSets" },
  { key: "autoscaling/v2/horizontalpodautoscalers", label: "HPA" },
  { key: "policy/v1/poddisruptionbudgets", label: "PodDisruptionBudgets" },
  { key: "networking.k8s.io/v1/networkpolicies", label: "NetworkPolicies" },
  { key: "core/v1/serviceaccounts", label: "ServiceAccounts" },
  { key: "core/v1/endpoints", label: "Endpoints" },
];

export default function AllResourcesPage() {
  return (
    <Suspense
      fallback={<div className="p-8 text-sm text-text-muted">Loading…</div>}
    >
      <AllResources />
    </Suspense>
  );
}

function AllResources() {
  const router = useRouter();
  const params = useSearchParams();
  const selectedKey = parseResourceKey(params.get("r"))
    ? params.get("r")
    : null;
  const namespaceParam = params.get("ns") ?? "";
  const selectorParam = params.get("l") ?? "";
  const [filter, setFilter] = useState("");
  const [nsInput, setNsInput] = useState(namespaceParam);
  const [selInput, setSelInput] = useState(selectorParam);

  const { data: resources, error, isLoading } = useAPIResources();
  const selected = useMemo(
    () => resources?.find((r) => resourceKey(r) === selectedKey),
    [resources, selectedKey],
  );

  function go(key: string, ns: string, selector: string) {
    const q = new URLSearchParams({ r: key });
    if (ns) q.set("ns", ns);
    if (selector) q.set("l", selector);
    router.replace(`/resources?${q.toString()}`);
  }

  // picking another kind starts from a clean namespace and selector
  function select(key: string) {
    setNsInput("");
    setSelInput("");
    go(key, "", "");
  }

  const groups = useMemo(() => {
    const f = filter.trim().toLowerCase();
    const by = new Map<string, APIResource[]>();
    for (const r of resources ?? []) {
      if (
        f &&
        !r.resource.includes(f) &&
        !r.kind.toLowerCase().includes(f) &&
        !(r.shortNames ?? []).some((s) => s.includes(f)) &&
        !r.group.includes(f)
      ) {
        continue;
      }
      const g = r.group || "core";
      by.set(g, [...(by.get(g) ?? []), r]);
    }
    return [...by.entries()];
  }, [resources, filter]);

  const available = new Set((resources ?? []).map(resourceKey));

  return (
    <div className="flex flex-col gap-[18px] px-8 py-7">
      <div>
        <div className="text-[15px] font-semibold">All resources</div>
        <div className="mt-1 max-w-3xl text-xs text-text-muted">
          Every kind this cluster serves — built-in and custom — listed as{" "}
          <span className="font-mono">kubectl get</span> would. What you see is
          what your own permissions allow. To change an object, open it and use
          the YAML tab.
        </div>
      </div>

      {resources && (
        <div className="flex flex-wrap gap-2">
          {shortcuts
            .filter((s) => available.has(s.key))
            .map((s) => (
              <button
                key={s.key}
                type="button"
                onClick={() => select(s.key)}
                className={`rounded-full border px-3 py-1 text-[12px] ${
                  selectedKey === s.key
                    ? "border-accent bg-surface-2 text-accent-text"
                    : "border-border text-text-muted hover:border-accent"
                }`}
              >
                {s.label}
              </button>
            ))}
        </div>
      )}

      {error ? (
        <div className="text-sm text-err">
          Failed to load the list of kinds: {friendlyErrorMessage(error)}
        </div>
      ) : isLoading || !resources ? (
        <ListSkeleton />
      ) : (
        <FillHeight className="flex gap-5">
          <div className="flex w-72 shrink-0 flex-col gap-2">
            <input
              type="text"
              aria-label="Find a kind"
              value={filter}
              onChange={(e) => setFilter(e.target.value)}
              placeholder="Find a kind — cron, hpa, ingress…"
              className="rounded-lg border border-border bg-surface px-3 py-1.75 text-xs outline-none placeholder:text-text-faint"
            />
            <div className="min-h-0 flex-1 overflow-auto rounded-[10px] border border-border bg-surface">
              {groups.length === 0 && (
                <div className="p-3 text-xs text-text-faint">
                  No kind matches.
                </div>
              )}
              {groups.map(([g, rs]) => (
                <div key={g}>
                  <div className="sticky top-0 border-b border-border bg-surface-2 px-3 py-1.5 font-mono text-[10.5px] uppercase tracking-wider text-text-faint">
                    {g}
                  </div>
                  {rs.map((r) => {
                    const k = resourceKey(r);
                    return (
                      <button
                        key={k}
                        type="button"
                        onClick={() => select(k)}
                        className={`flex w-full items-center justify-between gap-2 px-3 py-2 text-left text-[12.5px] ${
                          k === selectedKey
                            ? "bg-surface-2 text-accent-text"
                            : "text-text-muted hover:bg-surface-2"
                        }`}
                      >
                        <span className="truncate">{r.kind}</span>
                        <span className="shrink-0 font-mono text-[10.5px] text-text-faint">
                          {r.version}
                        </span>
                      </button>
                    );
                  })}
                </div>
              ))}
            </div>
          </div>

          <div className="flex min-w-0 flex-1 flex-col gap-3">
            {selected && selectedKey ? (
              <KindTable
                resource={selected}
                resourceKeyValue={selectedKey}
                namespace={namespaceParam}
                nsInput={nsInput}
                setNsInput={setNsInput}
                selector={selectorParam}
                selInput={selInput}
                setSelInput={setSelInput}
                onFilters={(ns, sel) => go(selectedKey, ns, sel)}
              />
            ) : (
              <div className="rounded-[10px] border border-border bg-surface p-6 text-sm text-text-muted">
                Pick a kind on the left — or one of the shortcuts above.
              </div>
            )}
          </div>
        </FillHeight>
      )}
    </div>
  );
}

function KindTable({
  resource,
  resourceKeyValue,
  namespace,
  nsInput,
  setNsInput,
  selector,
  selInput,
  setSelInput,
  onFilters,
}: {
  resource: APIResource;
  resourceKeyValue: string;
  namespace: string;
  nsInput: string;
  setNsInput: (v: string) => void;
  selector: string;
  selInput: string;
  setSelInput: (v: string) => void;
  onFilters: (ns: string, selector: string) => void;
}) {
  const { data, error, isLoading } = useResourceTable(
    resourceKeyValue,
    resource.namespaced ? namespace : "",
    selector,
  );
  const [sort, setSort] = useState<{ col: number; dir: 1 | -1 } | null>(null);
  const [rowFilter, setRowFilter] = useState("");
  const f = rowFilter.trim().toLowerCase();
  // a table of one namespace needs no Namespace column; a cluster-wide one does
  const showNamespace = resource.namespaced && !namespace;
  const filtered = (data?.rows ?? []).filter(
    (r) =>
      !f ||
      r.cells.some((c) => c.toLowerCase().includes(f)) ||
      r.name.toLowerCase().includes(f),
  );
  const rows = sort
    ? [...filtered].sort(
        (a, b) =>
          sort.dir *
          compareCells(a.cells[sort.col] ?? "", b.cells[sort.col] ?? ""),
      )
    : filtered;
  function onSort(col: number) {
    setSort(
      sort?.col !== col
        ? { col, dir: 1 }
        : sort.dir === 1
          ? { col, dir: -1 }
          : null,
    );
  }

  return (
    <>
      <div className="flex flex-wrap items-center gap-3">
        <div className="text-[13px] font-semibold">
          {resource.kind}{" "}
          <span className="font-mono text-[11px] font-normal text-text-faint">
            {resource.group ? `${resource.group}/` : ""}
            {resource.version} · {resource.resource}
          </span>
        </div>
        <form
          className="flex flex-wrap items-center gap-3"
          onSubmit={(e) => {
            e.preventDefault();
            onFilters(
              resource.namespaced ? nsInput.trim() : "",
              selInput.trim(),
            );
          }}
        >
          {resource.namespaced && (
            <NamespaceSelect value={nsInput} onChange={setNsInput} />
          )}
          <input
            type="text"
            aria-label="Label selector"
            value={selInput}
            onChange={(e) => setSelInput(e.target.value)}
            placeholder="Label selector — app=web,tier!=db"
            className="w-64 rounded-lg border border-border bg-surface px-3 py-1.75 font-mono text-xs outline-none placeholder:text-text-faint"
          />
          <button
            type="submit"
            className="rounded-lg border border-border bg-surface px-3 py-1.75 text-xs text-text-muted hover:border-accent"
          >
            Apply
          </button>
        </form>
        <input
          type="text"
          aria-label="Filter rows"
          value={rowFilter}
          onChange={(e) => setRowFilter(e.target.value)}
          placeholder="Filter rows"
          className="rounded-lg border border-border bg-surface px-3 py-1.75 text-xs outline-none placeholder:text-text-faint"
        />
      </div>

      {error ? (
        <div className="text-sm text-err">
          Failed to load {resource.kind}: {friendlyErrorMessage(error)}
        </div>
      ) : isLoading || !data ? (
        <ListSkeleton rows={5} />
      ) : (
        <>
          {data.truncated && (
            <div className="rounded-lg border border-warn/30 bg-surface-2 p-2.5 text-xs text-warn">
              Only the first {data.rows.length} objects are shown — narrow it
              down by namespace.
            </div>
          )}
          <div className="min-h-0 flex-1 overflow-auto rounded-[10px] border border-border bg-surface">
            {rows.length === 0 ? (
              <div className="p-4 text-sm text-text-faint">
                No {resource.kind} objects{f ? " match" : " found"}.
              </div>
            ) : (
              <table className="w-full border-collapse text-left text-xs">
                <thead className="sticky top-0 bg-surface-2 text-[10.5px] uppercase tracking-wider text-text-faint">
                  <tr>
                    {data.columns.map((c, i) => (
                      <Fragment key={c.name}>
                        <th
                          title={c.description}
                          aria-sort={
                            sort?.col === i
                              ? sort.dir === 1
                                ? "ascending"
                                : "descending"
                              : "none"
                          }
                          className="whitespace-nowrap px-3 py-2 font-medium"
                        >
                          <button
                            type="button"
                            onClick={() => onSort(i)}
                            className={`uppercase tracking-wider hover:text-text ${sort?.col === i ? "text-accent-text" : ""}`}
                          >
                            {c.name}
                            {sort?.col === i
                              ? sort.dir === 1
                                ? " ▲"
                                : " ▼"
                              : ""}
                          </button>
                        </th>
                        {i === 0 && showNamespace && (
                          <th className="whitespace-nowrap px-3 py-2 font-medium">
                            Namespace
                          </th>
                        )}
                      </Fragment>
                    ))}
                  </tr>
                </thead>
                <tbody>
                  {rows.map((r) => (
                    <tr
                      key={`${r.namespace ?? ""}/${r.name}`}
                      className="border-t border-border hover:bg-surface-2"
                    >
                      {r.cells.map((cell, i) => (
                        <Fragment key={i}>
                          <td className="max-w-md px-3 py-2 align-top">
                            {i === 0 ? (
                              <Link
                                href={`/resources/detail?r=${encodeURIComponent(resourceKeyValue)}&name=${encodeURIComponent(r.name)}${r.namespace ? `&ns=${encodeURIComponent(r.namespace)}` : ""}`}
                                className="font-mono text-accent-text hover:underline"
                              >
                                {cell}
                              </Link>
                            ) : (
                              <span
                                className={`break-words ${cell === "Warning" ? "text-warn" : "text-text-muted"}`}
                              >
                                {cell}
                              </span>
                            )}
                          </td>
                          {i === 0 && showNamespace && (
                            <td className="px-3 py-2 align-top font-mono text-text-muted">
                              {r.namespace}
                            </td>
                          )}
                        </Fragment>
                      ))}
                    </tr>
                  ))}
                </tbody>
              </table>
            )}
          </div>
        </>
      )}
    </>
  );
}
