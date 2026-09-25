"use client";

import { useState } from "react";
import { useConfigMaps } from "@/lib/hooks/useConfigMaps";
import { ListDetailSplit } from "@/components/ui/ListDetailSplit";
import { PlusIcon } from "@/components/icons";

export default function ConfigMapsPage() {
  const { data, isLoading } = useConfigMaps();
  const [selectedId, setSelectedId] = useState<string | null>(null);

  if (isLoading || !data) {
    return <div className="p-8 text-sm text-text-muted">Loading…</div>;
  }

  const selected = data.find((cm) => cm.id === selectedId) ?? data[0];

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

      <ListDetailSplit
        items={data}
        getId={(cm) => cm.id}
        selectedId={selected.id}
        onSelect={setSelectedId}
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
              <pre className="m-0 whitespace-pre-wrap break-words font-mono text-xs leading-relaxed">
                {key.value}
              </pre>
            </div>
          ))}
        </div>
      </ListDetailSplit>
    </div>
  );
}
