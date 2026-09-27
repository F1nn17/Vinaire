"use client";

import { useEffect } from "react";
import { useSWRConfig } from "swr";
import { buildStreamURL } from "@/lib/api/wsUrl";
import { useCurrentCluster } from "@/lib/clusterStore";
import type { NodeRow } from "@/lib/types";

type StreamEvent = {
  type: "added" | "modified" | "deleted";
  kind: string;
  row: NodeRow;
};

// Node informer events (Roadmap Phase 19) never carry cpu/memory — those
// come from metrics-server, not the watched Node object itself (see
// backend's api.NodeRowOf) — so a live diff must keep whatever cpu/memory
// the last REST fetch (useNodes) already found for that node, instead of
// blowing it away with the event's always-null values.
function applyEvent(rows: NodeRow[], event: StreamEvent): NodeRow[] {
  if (event.type === "deleted") {
    return rows.filter((r) => r.name !== event.row.name);
  }

  const existing = rows.find((r) => r.name === event.row.name);
  const merged: NodeRow = {
    ...event.row,
    cpu: event.row.cpu ?? existing?.cpu ?? null,
    memory: event.row.memory ?? existing?.memory ?? null,
  };

  if (existing) {
    return rows.map((r) => (r.name === merged.name ? merged : r));
  }
  return [...rows, merged];
}

// Mirrors useWorkloadsStream's single-socket-per-component pattern, filtered
// to "nodes" events on the same shared per-cluster stream.
export function useNodesStream() {
  const { mutate } = useSWRConfig();
  const cluster = useCurrentCluster();

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
        if (event.kind !== "nodes") return;

        mutate(
          ["nodes", cluster],
          (current: NodeRow[] | undefined) => applyEvent(current ?? [], event),
          { revalidate: false },
        );
      };
    })();

    return () => {
      cancelled = true;
      ws?.close();
    };
  }, [mutate, cluster]);
}
