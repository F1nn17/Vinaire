"use client";

import { useEffect, useRef, useState } from "react";
import { useRouter } from "next/navigation";
import { useSearch } from "@/lib/hooks/useSearch";
import { friendlyErrorMessage } from "@/lib/api/errors";
import type { SearchResult } from "@/lib/types";
import {
  ConfigMapIcon,
  LayersIcon,
  NetworkIcon,
  PodIcon,
  SearchIcon,
  SecretIcon,
  ServerIcon,
  StorageIcon,
} from "@/components/icons";

function kindIcon(kind: string) {
  switch (kind) {
    case "Pod":
      return <PodIcon size={14} />;
    case "Node":
      return <ServerIcon size={14} />;
    case "PVC":
      return <StorageIcon size={14} />;
    case "ConfigMap":
      return <ConfigMapIcon size={14} />;
    case "Secret":
      return <SecretIcon size={14} />;
    case "Ingress":
      return <NetworkIcon size={14} />;
    default:
      // Deployment / StatefulSet / DaemonSet / Job
      return <LayersIcon size={14} />;
  }
}

// Global ⌘K / Ctrl+K command palette (Roadmap Phase 20). The parent
// (Topbar) only renders this component while `open`, so every open is a
// fresh mount — query/activeIndex start clean with no reset effect needed.
export function CommandPalette({ onClose }: { onClose: () => void }) {
  const router = useRouter();
  const [query, setQuery] = useState("");
  const [activeIndex, setActiveIndex] = useState(0);
  const inputRef = useRef<HTMLInputElement>(null);
  const { data: results, error, isLoading } = useSearch(query);
  const rows: SearchResult[] = results ?? [];

  useEffect(() => {
    inputRef.current?.focus();
  }, []);

  useEffect(() => {
    function onKeyDown(e: KeyboardEvent) {
      if (e.key === "Escape") onClose();
    }
    window.addEventListener("keydown", onKeyDown);
    return () => window.removeEventListener("keydown", onKeyDown);
  }, [onClose]);

  function navigateTo(result: SearchResult) {
    router.push(result.href);
    onClose();
  }

  function handleQueryChange(value: string) {
    setQuery(value);
    setActiveIndex(0);
  }

  function handleKeyDown(e: React.KeyboardEvent) {
    if (e.key === "ArrowDown") {
      e.preventDefault();
      setActiveIndex((i) => Math.min(i + 1, rows.length - 1));
    } else if (e.key === "ArrowUp") {
      e.preventDefault();
      setActiveIndex((i) => Math.max(i - 1, 0));
    } else if (e.key === "Enter") {
      e.preventDefault();
      const result = rows[Math.min(activeIndex, rows.length - 1)];
      if (result) navigateTo(result);
    }
  }

  return (
    <div
      className="fixed inset-0 z-50 flex items-start justify-center bg-black/60 p-4 pt-[15vh]"
      onClick={onClose}
    >
      <div
        className="w-full max-w-lg rounded-xl border border-border bg-surface shadow-xl"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="flex items-center gap-2.5 border-b border-border px-4 py-3">
          <SearchIcon className="text-text-muted" />
          <input
            ref={inputRef}
            type="search"
            value={query}
            onChange={(e) => handleQueryChange(e.target.value)}
            onKeyDown={handleKeyDown}
            placeholder="Search pods, deployments, nodes…"
            className="flex-1 border-none bg-transparent font-mono text-[13px] outline-none"
          />
          <span className="rounded-[5px] border border-border px-1.5 py-0.5 font-mono text-[11px] text-text-faint">
            Esc
          </span>
        </div>

        <div className="max-h-[360px] overflow-auto p-1.5">
          {!query.trim() ? (
            <div className="p-4 text-center text-xs text-text-faint">
              Start typing to search across the current cluster.
            </div>
          ) : error ? (
            <div className="p-4 text-center text-xs text-err">
              {friendlyErrorMessage(error)}
            </div>
          ) : isLoading ? (
            <div className="p-4 text-center text-xs text-text-muted">
              Searching…
            </div>
          ) : rows.length === 0 ? (
            <div className="p-4 text-center text-xs text-text-faint">
              No matches for &ldquo;{query}&rdquo;.
            </div>
          ) : (
            rows.map((r, i) => (
              <button
                key={`${r.kind}/${r.namespace}/${r.name}`}
                type="button"
                onMouseEnter={() => setActiveIndex(i)}
                onClick={() => navigateTo(r)}
                className={`flex w-full items-center gap-2.5 rounded-lg px-3 py-2 text-left text-[12.5px] ${
                  i === activeIndex ? "bg-surface-2" : ""
                }`}
              >
                <span className="flex shrink-0 text-text-muted">
                  {kindIcon(r.kind)}
                </span>
                <span className="flex-1 truncate font-mono">{r.name}</span>
                {r.namespace && (
                  <span className="shrink-0 font-mono text-[11px] text-text-faint">
                    {r.namespace}
                  </span>
                )}
                <span className="shrink-0 rounded-full border border-border px-1.5 py-[1px] text-[10px] text-text-muted">
                  {r.kind}
                </span>
              </button>
            ))
          )}
        </div>
      </div>
    </div>
  );
}
