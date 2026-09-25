"use client";

import { useState } from "react";
import { useSecrets } from "@/lib/hooks/useSecrets";
import { ListDetailSplit } from "@/components/ui/ListDetailSplit";
import { PlusIcon, SecretIcon } from "@/components/icons";

export default function SecretsPage() {
  const { data, isLoading } = useSecrets();
  const [selectedId, setSelectedId] = useState<string | null>(null);
  const [revealed, setRevealed] = useState<Set<string>>(new Set());

  if (isLoading || !data) {
    return <div className="p-8 text-sm text-text-muted">Loading…</div>;
  }

  const selected = data.find((s) => s.id === selectedId) ?? data[0];

  function selectSecret(id: string) {
    setSelectedId(id);
    setRevealed(new Set());
  }

  function toggleKey(keyId: string) {
    setRevealed((prev) => {
      const next = new Set(prev);
      if (next.has(keyId)) next.delete(keyId);
      else next.add(keyId);
      return next;
    });
  }

  return (
    <div className="flex flex-col gap-[18px] px-8 py-7">
      <div className="flex items-center justify-between">
        <div className="text-[15px] font-semibold">Secrets</div>
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
        getId={(s) => s.id}
        selectedId={selected.id}
        onSelect={selectSecret}
        renderRow={(s) => (
          <>
            <div className="flex items-center gap-2">
              <SecretIcon size={14} className="shrink-0 text-text-muted" />
              <span className="font-mono text-[13px] font-semibold">
                {s.name}
              </span>
            </div>
            <div className="mt-1.5 flex items-center gap-2 font-mono text-[11px] text-text-faint">
              <span>ns/{s.namespace}</span>
              <span>·</span>
              <span>{s.type}</span>
              <span>·</span>
              <span>{s.age}</span>
            </div>
          </>
        )}
      >
        <div className="shrink-0">
          <div className="font-mono text-[15px] font-semibold">
            {selected.name}
          </div>
          <div className="mt-0.5 font-mono text-[11px] text-text-faint">
            ns/{selected.namespace} · {selected.type} · {selected.age}
          </div>
        </div>
        <div className="flex flex-1 flex-col gap-3 overflow-auto">
          {selected.keys.map((key) => {
            const shown = revealed.has(key.id);
            return (
              <div
                key={key.id}
                className="rounded-[10px] border border-border bg-surface p-4"
              >
                <div className="mb-2 flex items-center justify-between">
                  <span className="font-mono text-xs text-text-muted">
                    {key.label}
                  </span>
                  <button
                    type="button"
                    onClick={() => toggleKey(key.id)}
                    className="rounded-md border border-border bg-surface-2 px-2 py-[3px] text-[11px] text-accent-text"
                  >
                    {shown ? "Hide" : "Show"}
                  </button>
                </div>
                <div className="break-all font-mono text-xs">
                  {shown ? key.value : "••••••••••••"}
                </div>
              </div>
            );
          })}
        </div>
      </ListDetailSplit>
    </div>
  );
}
