"use client";

import { useEffect, useRef, useState } from "react";
import { ChevronDownIcon } from "@/components/icons";
import { useClusters } from "@/lib/hooks/useClusters";
import { useCurrentCluster, setCurrentCluster } from "@/lib/clusterStore";

// Roadmap Phase 11: a real dropdown sourced from GET /api/v1/clusters,
// replacing the static "prod-msk-01" label. Selection is stored in
// lib/clusterStore (localStorage-backed) and read by apiFetch/buildStreamURL
// to set the X-Cluster header/`?cluster=` param on every request.
export function ClusterPicker() {
  const { data: clusters } = useClusters();
  const selected = useCurrentCluster();
  const [open, setOpen] = useState(false);
  const rootRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    function onClickOutside(e: MouseEvent) {
      if (rootRef.current && !rootRef.current.contains(e.target as Node)) {
        setOpen(false);
      }
    }
    document.addEventListener("mousedown", onClickOutside);
    return () => document.removeEventListener("mousedown", onClickOutside);
  }, []);

  const activeName =
    selected ||
    clusters?.find((c) => c.default)?.name ||
    clusters?.[0]?.name ||
    "—";

  return (
    <div ref={rootRef} className="relative">
      <button
        type="button"
        onClick={() => setOpen((v) => !v)}
        className="flex items-center gap-2 rounded-lg border border-border bg-surface px-3 py-2"
      >
        <span className="font-mono text-[13px] font-semibold">
          {activeName}
        </span>
        <ChevronDownIcon />
      </button>

      {open && clusters && clusters.length > 0 && (
        <div className="absolute left-0 top-[calc(100%+6px)] z-20 min-w-[200px] rounded-lg border border-border bg-surface p-1 shadow-lg">
          {clusters.map((c) => (
            <button
              key={c.name}
              type="button"
              onClick={() => {
                setCurrentCluster(c.name);
                setOpen(false);
              }}
              className={`flex w-full items-center justify-between rounded-md px-3 py-2 text-left font-mono text-[12.5px] ${
                c.name === activeName
                  ? "bg-surface-2 text-accent-text"
                  : "text-text"
              }`}
            >
              {c.name}
              {c.default && (
                <span className="text-[11px] text-text-faint">default</span>
              )}
            </button>
          ))}
        </div>
      )}
    </div>
  );
}
