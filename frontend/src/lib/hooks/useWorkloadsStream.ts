"use client";

import { useEffect, useRef } from "react";
import { useSWRConfig } from "swr";
import { buildStreamURL } from "@/lib/api/wsUrl";
import { useCurrentCluster } from "@/lib/clusterStore";
import type { WorkloadKind, WorkloadRow } from "@/lib/types";

type StreamEvent = {
  type: "added" | "modified" | "deleted";
  kind: WorkloadKind;
  row: WorkloadRow;
};

function rowKey(row: WorkloadRow): string {
  return `${row.namespace}/${row.name}`;
}

function applyEvent(rows: WorkloadRow[], event: StreamEvent): WorkloadRow[] {
  const eventKey = rowKey(event.row);

  if (event.type === "deleted") {
    return rows.filter((r) => rowKey(r) !== eventKey);
  }

  const exists = rows.some((r) => rowKey(r) === eventKey);
  if (exists) {
    return rows.map((r) => (rowKey(r) === eventKey ? event.row : r));
  }
  return [...rows, event.row];
}

// Opens a single WebSocket connection for the lifetime of the calling
// component and merges incoming add/modify/delete diffs into the matching
// useWorkloads(kind, namespace) SWR cache entry — no polling, no
// reconnect-on-filter-change. kind/namespace are read from refs inside the
// message handler so switching tabs or typing a namespace filter doesn't
// tear down and reopen the socket.
export function useWorkloadsStream(kind: WorkloadKind, namespace: string) {
  const { mutate } = useSWRConfig();
  const kindRef = useRef(kind);
  const namespaceRef = useRef(namespace);
  const cluster = useCurrentCluster();

  useEffect(() => {
    kindRef.current = kind;
    namespaceRef.current = namespace;
  }, [kind, namespace]);

  useEffect(() => {
    let ws: WebSocket | undefined;
    let cancelled = false;

    (async () => {
      const wsUrl = await buildStreamURL("/api/v1/stream/workloads");
      if (cancelled) return;
      ws = new WebSocket(wsUrl);

      ws.onmessage = (message) => {
        let event: StreamEvent;
        try {
          event = JSON.parse(message.data);
        } catch {
          return;
        }

        const activeKind = kindRef.current;
        const activeNamespace = namespaceRef.current;
        if (event.kind !== activeKind) return;
        if (activeNamespace && event.row.namespace !== activeNamespace) return;

        mutate(
          ["workloads", activeKind, activeNamespace, cluster],
          (current: WorkloadRow[] | undefined) =>
            applyEvent(current ?? [], event),
          { revalidate: false },
        );
      };
    })();

    return () => {
      cancelled = true;
      ws?.close();
    };
    // Reconnect when the selected cluster changes (a different cluster's
    // hub entirely) — kind/namespace are read live from refs above (kept
    // current by the effect above) rather than reconnecting on every
    // filter change.
  }, [mutate, cluster]);
}
