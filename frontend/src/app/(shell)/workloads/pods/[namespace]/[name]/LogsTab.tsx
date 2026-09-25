"use client";

import { useEffect, useRef, useState } from "react";
import { useLogsStream } from "@/lib/hooks/useLogsStream";
import { LogLine } from "@/components/ui/LogLine";
import type { ContainerSummary } from "@/lib/types";

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

  useEffect(() => {
    if (autoScroll && scrollRef.current) {
      scrollRef.current.scrollTop = scrollRef.current.scrollHeight;
    }
  }, [lines, autoScroll]);

  return (
    <div className="flex flex-col gap-2.5">
      <div className="flex items-center justify-between">
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
        </div>
        <button
          type="button"
          onClick={() => setFollowing((f) => !f)}
          className="rounded-md border border-border bg-surface px-2.5 py-1 text-xs"
        >
          {following ? "Pause" : "Follow"}
        </button>
      </div>

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
        {lines.length === 0 ? (
          <div className="font-mono text-xs text-text-faint">
            {following ? "Waiting for logs…" : "Paused."}
          </div>
        ) : (
          lines.map((line, i) => <LogLine key={i} {...line} />)
        )}
      </div>
    </div>
  );
}
