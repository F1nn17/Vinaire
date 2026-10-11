"use client";

import { NamespaceSelect } from "@/components/ui/NamespaceSelect";
import { Suspense, useState } from "react";
import { useSearchParams } from "next/navigation";
import { t } from "@/lib/i18n/translate";
import { useConfigMaps } from "@/lib/hooks/useConfigMaps";
import { friendlyErrorMessage } from "@/lib/api/errors";
import { ListDetailSplit } from "@/components/ui/ListDetailSplit";
import { ListSkeleton } from "@/components/ui/Skeleton";
import { CreateResourceModal } from "@/components/editor/CreateResourceModal";
import { configMapTemplates } from "@/components/editor/templates";
import { PlusIcon } from "@/components/icons";

export default function ConfigMapsPage() {
  return (
    <Suspense
      fallback={<div className="p-8 text-sm text-text-muted">Loading…</div>}
    >
      <ConfigMapsInner />
    </Suspense>
  );
}

// A link may open the page on one ConfigMap (?namespace=app&name=settings).
function ConfigMapsInner() {
  const params = useSearchParams();
  const linkedNs = params.get("namespace") ?? "";
  const linkedName = params.get("name") ?? "";
  const [namespace, setNamespace] = useState(linkedNs);
  const [query, setQuery] = useState("");
  const { data: all, isLoading, error, mutate } = useConfigMaps(namespace);
  const needle = query.trim().toLowerCase();
  const data = all?.filter(
    (cm) =>
      !needle ||
      cm.name.toLowerCase().includes(needle) ||
      cm.namespace.toLowerCase().includes(needle) ||
      cm.keys.some(
        (k) =>
          k.label.toLowerCase().includes(needle) ||
          k.value.toLowerCase().includes(needle),
      ),
  );
  const [createOpen, setCreateOpen] = useState(false);
  const [pickedId, setSelectedId] = useState<string | null>(null);
  const selectedId =
    pickedId ??
    (linkedName
      ? (all?.find((c) => c.name === linkedName && c.namespace === linkedNs)
          ?.id ?? null)
      : null);

  return (
    <div className="flex flex-col gap-[18px] px-8 py-7">
      <div className="flex items-center justify-between">
        <div className="text-[15px] font-semibold">ConfigMaps</div>
        <button
          type="button"
          onClick={() => setCreateOpen(true)}
          className="flex items-center gap-1.5 rounded-lg bg-accent px-3.5 py-2 text-[12.5px] font-semibold text-white"
        >
          <PlusIcon />
          Create
        </button>
      </div>

      <div className="flex flex-wrap items-center gap-3">
        <label className="flex min-w-64 items-center gap-2">
          <span className="sr-only">Search</span>
          <input
            type="search"
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            placeholder={t("Search by name, namespace, key or value")}
            className="w-full rounded-lg border border-border bg-surface px-3 py-1.75 text-xs outline-none placeholder:text-text-faint"
          />
        </label>
        <label className="flex w-fit items-center gap-2">
          <span className="sr-only">Filter by namespace</span>
          <NamespaceSelect value={namespace} onChange={setNamespace} />
        </label>
      </div>

      {error ? (
        <div className="text-sm text-err">
          Failed to load ConfigMaps: {friendlyErrorMessage(error)}
        </div>
      ) : isLoading || !data ? (
        <ListSkeleton />
      ) : data.length === 0 ? (
        <div className="text-sm text-text-muted">
          {needle
            ? t("Nothing matches the search.")
            : `No ConfigMaps found${namespace ? ` in namespace "${namespace}"` : ""}.`}
        </div>
      ) : (
        <ConfigMapsList
          data={data}
          selectedId={selectedId}
          onSelect={setSelectedId}
        />
      )}
      {createOpen && (
        <CreateResourceModal
          templates={configMapTemplates}
          onClose={() => setCreateOpen(false)}
          onCreated={() => void mutate()}
        />
      )}
    </div>
  );
}

function ConfigMapsList({
  data,
  selectedId,
  onSelect,
}: {
  data: NonNullable<ReturnType<typeof useConfigMaps>["data"]>;
  selectedId: string | null;
  onSelect: (id: string) => void;
}) {
  const selected = data.find((cm) => cm.id === selectedId) ?? data[0];

  return (
    <ListDetailSplit
      items={data}
      getId={(cm) => cm.id}
      selectedId={selected.id}
      onSelect={onSelect}
      renderRow={(cm) => (
        <>
          <div className="font-mono text-[13px] font-semibold">{cm.name}</div>
          <div className="mt-1.5 flex items-center gap-2 font-mono text-[11px] text-text-faint">
            <span>ns/{cm.namespace}</span>
            <span>·</span>
            <span>{cm.keys.length} keys</span>
            <span>·</span>
            <span>{cm.age}</span>
          </div>
        </>
      )}
    >
      <div className="shrink-0">
        <div className="font-mono text-[15px] font-semibold">
          {selected.name}
        </div>
        <div className="mt-0.5 font-mono text-[11px] text-text-faint">
          ns/{selected.namespace} · {selected.age}
        </div>
      </div>
      <div className="flex flex-1 flex-col gap-3 overflow-auto">
        {selected.keys.map((key) => (
          <div
            key={key.label}
            className="rounded-[10px] border border-border bg-surface p-4"
          >
            <div className="mb-2 font-mono text-xs text-text-muted">
              {key.label}
            </div>
            <pre className="m-0 whitespace-pre-wrap wrap-break-word font-mono text-xs leading-relaxed">
              {key.value}
            </pre>
          </div>
        ))}
      </div>
    </ListDetailSplit>
  );
}
