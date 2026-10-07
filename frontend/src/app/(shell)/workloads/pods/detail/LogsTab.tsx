"use client";

import Link from "next/link";
import { useEffect, useMemo, useRef, useState } from "react";
import { useLogsStream } from "@/lib/hooks/useLogsStream";
import { useLogsSearch } from "@/lib/hooks/useObservability";
import { friendlyErrorMessage } from "@/lib/api/errors";
import { FillHeight } from "@/components/ui/FillHeight";
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

  // Find in the live tail: highlight, count, step through matches, or show only
  // the matching lines.
  const [find, setFind] = useState("");
  const [onlyMatches, setOnlyMatches] = useState(false);
  const [cursor, setCursor] = useState(0);
  const needle = find.trim().toLowerCase();
  const matches = useMemo(
    () =>
      needle
        ? lines.flatMap((l, i) =>
            l.text.toLowerCase().includes(needle) ? [i] : [],
          )
        : [],
    [lines, needle],
  );
  const matchSet = useMemo(() => new Set(matches), [matches]);
  const current = matches.length ? Math.min(cursor, matches.length - 1) : -1;
  const activeLine = current >= 0 ? matches[current] : -1;

  function step(delta: number) {
    if (!matches.length) return;
    setCursor(
      (c) =>
        (Math.min(c, matches.length - 1) + delta + matches.length) %
        matches.length,
    );
  }

  // jump to the active match
  useEffect(() => {
    if (activeLine >= 0) {
      document
        .getElementById(`log-line-${activeLine}`)
        ?.scrollIntoView({ block: "center" });
    }
  }, [activeLine, cursor]);

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
    if (mode === "live" && autoScroll && !needle && scrollRef.current) {
      scrollRef.current.scrollTop = scrollRef.current.scrollHeight;
    }
  }, [lines, autoScroll, mode, needle]);

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

      {mode === "live" && (
        <div className="flex items-center gap-2">
          <input
            type="text"
            aria-label="Find in logs"
            value={find}
            onChange={(e) => {
              setFind(e.target.value);
              setCursor(0);
            }}
            onKeyDown={(e) => {
              if (e.key === "Enter") {
                e.preventDefault();
                step(e.shiftKey ? -1 : 1);
              }
            }}
            placeholder="Find in the log… (Enter — next, Shift+Enter — previous)"
            className="flex-1 rounded-md border border-border bg-surface px-2.5 py-1.5 font-mono text-xs outline-none placeholder:text-text-faint"
          />
          <span className="w-24 shrink-0 text-right font-mono text-[11px] text-text-faint">
            {needle
              ? matches.length
                ? `${current + 1} / ${matches.length}`
                : "no matches"
              : ""}
          </span>
          <button
            type="button"
            onClick={() => step(-1)}
            disabled={!matches.length}
            aria-label="Previous match"
            className="rounded-md border border-border bg-surface px-2 py-1 text-xs disabled:opacity-40"
          >
            ↑
          </button>
          <button
            type="button"
            onClick={() => step(1)}
            disabled={!matches.length}
            aria-label="Next match"
            className="rounded-md border border-border bg-surface px-2 py-1 text-xs disabled:opacity-40"
          >
            ↓
          </button>
          <label className="flex items-center gap-1.5 text-xs text-text-muted">
            <input
              type="checkbox"
              checked={onlyMatches}
              onChange={(e) => setOnlyMatches(e.target.checked)}
            />
            Only matches
          </label>
        </div>
      )}

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

      <FillHeight className="overflow-hidden rounded-[10px] border border-border bg-code-bg">
        <div
          ref={scrollRef}
          onScroll={(e) => {
            const el = e.currentTarget;
            const atBottom =
              el.scrollHeight - el.scrollTop - el.clientHeight < 24;
            setAutoScroll(atBottom);
          }}
          className="h-full overflow-auto p-4"
        >
          {mode === "live" ? (
            lines.length === 0 ? (
              <div className="font-mono text-xs text-text-faint">
                {following ? "Waiting for logs…" : "Paused."}
              </div>
            ) : (
              lines.map((line, i) =>
                onlyMatches && needle && !matchSet.has(i) ? null : (
                  <LogLine
                    key={i}
                    id={`log-line-${i}`}
                    {...line}
                    highlight={needle}
                    active={i === activeLine}
                  />
                ),
              )
            )
          ) : !submitted ? (
            <div className="font-mono text-xs text-text-faint">
              Search historical logs across this pod via Loki.
            </div>
          ) : !search.data ? (
            <div className="font-mono text-xs text-text-faint">
              {search.error
                ? `Failed: ${friendlyErrorMessage(search.error)}`
                : "Searching…"}
            </div>
          ) : !search.data.available ? (
            <div className="font-mono text-xs text-text-faint">
              Historical log search unavailable: {search.data.reason}{" "}
              <Link href="/logging" className="text-accent-text underline">
                Set up the logs stack →
              </Link>
            </div>
          ) : (search.data.lines ?? []).length === 0 ? (
            <div className="font-mono text-xs text-text-faint">
              No matching lines in this time range.
            </div>
          ) : (
            (search.data.lines ?? []).map((line, i) => (
              <LogLine key={i} {...line} />
            ))
          )}
        </div>
      </FillHeight>
    </div>
  );
}
