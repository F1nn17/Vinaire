"use client";

import { useState } from "react";
import { useConfigMaps } from "@/lib/hooks/useConfigMaps";
import { ListDetailSplit } from "@/components/ui/ListDetailSplit";
import { PlusIcon } from "@/components/icons";

export default function ConfigMapsPage() {
  const [namespace, setNamespace] = useState("");
  const { data, isLoading, error } = useConfigMaps(namespace);
  const [selectedId, setSelectedId] = useState<string | null>(null);

  return (
    <div className="flex flex-col gap-[18px] px-8 py-7">
      <div className="flex items-center justify-between">
        <div className="text-[15px] font-semibold">ConfigMaps</div>
        <button
          type="button"
          className="flex items-center gap-1.5 rounded-lg bg-accent px-3.5 py-2 text-[12.5px] font-semibold text-white"
        >
          <PlusIcon />
          Create
        </button>
      </div>

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

      {error ? (
        <div className="text-sm text-err">
          Failed to load ConfigMaps: {error.message}
        </div>
      ) : isLoading || !data ? (
        <div className="text-sm text-text-muted">Loading…</div>
      ) : data.length === 0 ? (
        <div className="text-sm text-text-muted">
          No ConfigMaps found{namespace ? ` in namespace "${namespace}"` : ""}.
        </div>
      ) : (
        <ConfigMapsList
          data={data}
          selectedId={selectedId}
          onSelect={setSelectedId}
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
