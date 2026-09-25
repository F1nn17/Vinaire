"use client";

import { useEffect, useRef, useState } from "react";
import { Terminal } from "@xterm/xterm";
import { FitAddon } from "@xterm/addon-fit";
import "@xterm/xterm/css/xterm.css";
import { API_BASE } from "@/lib/api/config";
import type { ContainerSummary } from "@/lib/types";

type Status = "connecting" | "connected" | "disconnected";

// A real shell into the pod (kubectl exec -it ... -- /bin/sh equivalent),
// not a scripted demo. See internal/stream.ServeExec on the backend for the
// WebSocket <-> remotecommand bridge this connects to.
export function TerminalTab({
  namespace,
  name,
  containers,
}: {
  namespace: string;
  name: string;
  containers: ContainerSummary[];
}) {
  const [container, setContainer] = useState(containers[0]?.name ?? "");
  const [status, setStatus] = useState<Status>("connecting");
  const containerRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    const el = containerRef.current;
    if (!el) return;

    setStatus("connecting");

    const term = new Terminal({
      convertEol: true,
      fontFamily: "var(--font-mono), ui-monospace, monospace",
      fontSize: 12,
      theme: { background: "#0a0b0e", foreground: "#edeff3" },
    });
    const fitAddon = new FitAddon();
    term.loadAddon(fitAddon);
    term.open(el);
    fitAddon.fit();

    const query = container ? `?container=${encodeURIComponent(container)}` : "";
    const wsUrl = `${API_BASE.replace(/^http/, "ws")}/api/v1/pods/${encodeURIComponent(namespace)}/${encodeURIComponent(name)}/exec${query}`;
    const ws = new WebSocket(wsUrl);

    function sendResize() {
      if (ws.readyState === WebSocket.OPEN) {
        ws.send(JSON.stringify({ type: "resize", cols: term.cols, rows: term.rows }));
      }
    }

    ws.onopen = () => {
      setStatus("connected");
      sendResize();
    };
    ws.onclose = () => setStatus("disconnected");
    ws.onerror = () => setStatus("disconnected");
    ws.onmessage = (event) => {
      if (typeof event.data === "string") term.write(event.data);
    };

    const dataDisposable = term.onData((data) => {
      if (ws.readyState === WebSocket.OPEN) {
        ws.send(JSON.stringify({ type: "stdin", data }));
      }
    });

    const resizeObserver = new ResizeObserver(() => {
      fitAddon.fit();
      sendResize();
    });
    resizeObserver.observe(el);

    return () => {
      dataDisposable.dispose();
      resizeObserver.disconnect();
      ws.close();
      term.dispose();
    };
  }, [namespace, name, container]);

  return (
    <div className="flex flex-col gap-2.5">
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
        <span>· {status}</span>
      </div>
      <div
        ref={containerRef}
        className="h-[440px] rounded-[10px] border border-border bg-code-bg p-2"
      />
    </div>
  );
}
