"use client";

import { useEffect, useRef, useState } from "react";
import { useLogsStream } from "@/lib/hooks/useLogsStream";
import { useLogsSearch } from "@/lib/hooks/useObservability";
import { LogLine } from "@/components/ui/LogLine";
import type { ContainerSummary } from "@/lib/types";

const sinceOptions = [
  { value: "15m", label: "Last 15m" },
  { value: "1h", label: "Last 1h" },
  { value: "6h", label: "Last 6h" },
  { value: "24h", label: "Last 24h" },
];

export function LogsTab({
  namespace,
  name,
  containers,
}: {
  namespace: string;
  name: string;
  containers: ContainerSummary[];
}) {
  const [container, setContainer] = useState(containers[0]?.name ?? "");
  const [following, setFollowing] = useState(true);
  const [autoScroll, setAutoScroll] = useState(true);
  const { lines, connected } = useLogsStream(
    namespace,
    name,
    container,
    following,
  );
  const scrollRef = useRef<HTMLDivElement>(null);

  // Roadmap Phase 16: historical search via Loki, alongside the Phase 4
  // live tail above — a separate mode rather than merging the two, since
  // live tail is per-container while a historical search scopes to the
  // whole pod (Loki's label scheme here doesn't distinguish containers).
  const [mode, setMode] = useState<"live" | "history">("live");
  const [historyQuery, setHistoryQuery] = useState("");
  const [historySince, setHistorySince] = useState("1h");
  const [submitted, setSubmitted] = useState<{
    query: string;
    since: string;
  } | null>(null);
  const search = useLogsSearch(
    namespace,
    name,
    submitted?.query ?? "",
    submitted?.since ?? historySince,
    mode === "history" && submitted !== null,
  );

  useEffect(() => {
    if (mode === "live" && autoScroll && scrollRef.current) {
      scrollRef.current.scrollTop = scrollRef.current.scrollHeight;
    }
  }, [lines, autoScroll, mode]);

  return (
    <div className="flex flex-col gap-2.5">
      <div className="flex items-center justify-between">
        <div className="flex items-center gap-1 rounded-lg border border-border bg-surface p-1">
          <button
            type="button"
            onClick={() => setMode("live")}
            className={`rounded-md px-2.5 py-1 text-xs ${
              mode === "live"
                ? "bg-surface-2 text-accent-text"
                : "text-text-muted"
            }`}
          >
            Live
          </button>
          <button
            type="button"
            onClick={() => setMode("history")}
            className={`rounded-md px-2.5 py-1 text-xs ${
              mode === "history"
                ? "bg-surface-2 text-accent-text"
                : "text-text-muted"
            }`}
          >
            History
          </button>
        </div>

        {mode === "live" && (
          <div className="flex items-center gap-2 font-mono text-xs text-text-faint">
            <span>Container:</span>
            {containers.length > 1 ? (
              <select
                value={container}
                onChange={(e) => setContainer(e.target.value)}
                className="rounded-md border border-border bg-surface px-1.5 py-0.5 text-text"
              >
                {containers.map((c) => (
                  <option key={c.name} value={c.name}>
                    {c.name}
                  </option>
                ))}
              </select>
            ) : (
              <span className="text-text">{container || "—"}</span>
            )}
            <span>
              ·{" "}
              {following
                ? connected
                  ? "following live"
                  : "connecting…"
                : "paused"}
            </span>
            <button
              type="button"
              onClick={() => setFollowing((f) => !f)}
              className="rounded-md border border-border bg-surface px-2.5 py-1 text-xs text-text"
            >
              {following ? "Pause" : "Follow"}
            </button>
          </div>
        )}
      </div>

      {mode === "history" && (
        <div className="flex items-center gap-2">
          <input
            type="text"
            value={historyQuery}
            onChange={(e) => setHistoryQuery(e.target.value)}
            placeholder="Search text…"
            className="flex-1 rounded-md border border-border bg-surface px-2.5 py-1.5 font-mono text-xs outline-none placeholder:text-text-faint"
          />
          <select
            value={historySince}
            onChange={(e) => setHistorySince(e.target.value)}
            className="rounded-md border border-border bg-surface px-2 py-1.5 font-mono text-xs"
          >
            {sinceOptions.map((o) => (
              <option key={o.value} value={o.value}>
                {o.label}
              </option>
            ))}
          </select>
          <button
            type="button"
            onClick={() =>
              setSubmitted({ query: historyQuery, since: historySince })
            }
            className="rounded-md border border-border bg-surface px-3 py-1.5 text-xs"
          >
            Search
          </button>
        </div>
      )}

      <div
        ref={scrollRef}
        onScroll={(e) => {
          const el = e.currentTarget;
          const atBottom =
            el.scrollHeight - el.scrollTop - el.clientHeight < 24;
          setAutoScroll(atBottom);
        }}
        className="max-h-[440px] overflow-auto rounded-[10px] border border-border bg-code-bg p-4"
      >
        {mode === "live" ? (
          lines.length === 0 ? (
            <div className="font-mono text-xs text-text-faint">
              {following ? "Waiting for logs…" : "Paused."}
            </div>
          ) : (
            lines.map((line, i) => <LogLine key={i} {...line} />)
          )
        ) : !submitted ? (
          <div className="font-mono text-xs text-text-faint">
            Search historical logs across this pod via Loki.
          </div>
        ) : !search.data ? (
          <div className="font-mono text-xs text-text-faint">
            {search.error ? `Failed: ${search.error.message}` : "Searching…"}
          </div>
        ) : !search.data.available ? (
          <div className="font-mono text-xs text-text-faint">
            Historical log search unavailable: {search.data.reason}
          </div>
        ) : search.data.lines.length === 0 ? (
          <div className="font-mono text-xs text-text-faint">
            No matching lines in this time range.
          </div>
        ) : (
          search.data.lines.map((line, i) => <LogLine key={i} {...line} />)
        )}
      </div>
    </div>
  );
}
