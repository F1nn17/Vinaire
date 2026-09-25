"use client";

import { useEffect, useState } from "react";
import { buildStreamURL } from "@/lib/api/wsUrl";
import type { LogLine } from "@/lib/types";

// Cap the rendered line buffer so a chatty container doesn't grow the DOM
// unbounded during a long-open session.
const MAX_LINES = 1000;

// Opens a WebSocket for one container's live logs (kubectl logs -f
// equivalent) while `enabled` is true, and closes it otherwise — this is
// what the Logs tab's Follow/Pause toggle controls. Changing container
// closes the current connection and opens a fresh one for the new
// container, since it's a genuinely different log stream, not a filter.
export function useLogsStream(
  namespace: string,
  name: string,
  container: string,
  enabled: boolean,
) {
  const [lines, setLines] = useState<LogLine[]>([]);
  const [connected, setConnected] = useState(false);

  useEffect(() => {
    if (!enabled) {
      // No socket to close/reset here — the previous run's cleanup already
      // closed it, and that close event already flipped `connected` to
      // false via `ws.onclose` below.
      return;
    }

    let ws: WebSocket | undefined;
    let cancelled = false;

    (async () => {
      const query = container
        ? `?container=${encodeURIComponent(container)}`
        : "";
      const wsUrl = await buildStreamURL(
        `/api/v1/pods/${encodeURIComponent(namespace)}/${encodeURIComponent(name)}/logs${query}`,
      );
      if (cancelled) return;
      ws = new WebSocket(wsUrl);

      ws.onopen = () => {
        // Clear the previous connection's lines once the new one actually
        // opens, rather than eagerly at the top of the effect.
        setLines([]);
        setConnected(true);
      };
      ws.onclose = () => setConnected(false);
      ws.onerror = () => setConnected(false);
      ws.onmessage = (event) => {
        let line: LogLine;
        try {
          line = JSON.parse(event.data);
        } catch {
          return;
        }
        setLines((prev) => {
          const next = [...prev, line];
          return next.length > MAX_LINES
            ? next.slice(next.length - MAX_LINES)
            : next;
        });
      };
    })();

    return () => {
      cancelled = true;
      ws?.close();
    };
  }, [namespace, name, container, enabled]);

  return { lines, connected };
}
