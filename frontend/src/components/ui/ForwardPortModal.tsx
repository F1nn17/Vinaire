"use client";

import { useState } from "react";
import { useClusters } from "@/lib/hooks/useClusters";
import { useCurrentCluster } from "@/lib/clusterStore";
import { buildProxyUrl } from "@/lib/proxyUrl";
import type { PodPort } from "@/lib/types";

// Looks like a TLS port: named for it, or one of the usual suspects.
function looksLikeHttps(p: PodPort): boolean {
  return /https|tls|ssl/i.test(p.name) || p.port === 443 || p.port === 8443;
}

// "Forward port" (Roadmap Phase 26): pick a port of the pod and open its web
// UI in a new tab through Vinaire's authenticated proxy. Mounted only while
// open (parents render `{open && <ForwardPortModal/>}`), so every open starts
// fresh.
export function ForwardPortModal({
  namespace,
  pod,
  ports,
  onClose,
}: {
  namespace: string;
  pod: string;
  ports: PodPort[];
  onClose: () => void;
}) {
  const tcpPorts = ports.filter((p) => p.protocol === "TCP");
  const [selected, setSelected] = useState<string>(
    tcpPorts.length > 0 ? String(tcpPorts[0].port) : "custom",
  );
  const [custom, setCustom] = useState("");
  const [https, setHttps] = useState(
    tcpPorts.length > 0 ? looksLikeHttps(tcpPorts[0]) : false,
  );

  const currentCluster = useCurrentCluster();
  const { data: clusters } = useClusters();
  const cluster =
    currentCluster || clusters?.find((c) => c.default)?.name || "";

  const portValue = selected === "custom" ? custom.trim() : selected;
  const portNumber = Number(portValue);
  const valid =
    /^\d+$/.test(portValue) && portNumber >= 1 && portNumber <= 65535;

  function open() {
    if (!valid || !cluster) return;
    window.open(
      buildProxyUrl({ cluster, namespace, pod, port: portNumber, https }),
      "_blank",
      "noopener,noreferrer",
    );
    onClose();
  }

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 p-4">
      <div className="w-full max-w-md rounded-xl border border-border bg-surface p-5">
        <div className="text-sm font-semibold">Forward port</div>
        <div className="mt-1 font-mono text-xs text-text-muted">
          {namespace}/{pod}
        </div>

        <div className="mt-4 flex flex-col gap-2">
          {tcpPorts.map((p) => (
            <label
              key={`${p.container}-${p.port}`}
              className="flex cursor-pointer items-center gap-2.5 rounded-lg border border-border bg-surface-2 px-3 py-2 text-xs"
            >
              <input
                type="radio"
                name="port"
                checked={selected === String(p.port)}
                onChange={() => {
                  setSelected(String(p.port));
                  setHttps(looksLikeHttps(p));
                }}
              />
              <span className="font-mono">{p.port}</span>
              <span className="text-text-muted">
                {p.name ? `${p.name} · ` : ""}
                {p.container}
              </span>
            </label>
          ))}
          <label className="flex items-center gap-2.5 rounded-lg border border-border bg-surface-2 px-3 py-2 text-xs">
            <input
              type="radio"
              name="port"
              checked={selected === "custom"}
              onChange={() => setSelected("custom")}
            />
            <span className="text-text-muted">Other port</span>
            <input
              aria-label="Custom port"
              inputMode="numeric"
              value={custom}
              placeholder="8080"
              onFocus={() => setSelected("custom")}
              onChange={(e) => setCustom(e.target.value)}
              className="w-24 rounded-md border border-border bg-surface px-2 py-1 font-mono text-xs outline-none"
            />
          </label>
        </div>

        <label className="mt-3 flex items-center gap-2 text-xs text-text-muted">
          <input
            type="checkbox"
            checked={https}
            onChange={(e) => setHttps(e.target.checked)}
          />
          The pod serves HTTPS on this port
        </label>

        <div className="mt-3 text-[11px] leading-relaxed text-text-faint">
          Opens in a new tab through Vinaire&apos;s authenticated proxy (HTTP
          only, not raw TCP). The page runs sandboxed: no cookies or local
          storage, and apps that link to root-relative paths may not load fully.
          Closing the tab closes the connection.
        </div>

        <div className="mt-5 flex justify-end gap-2.5">
          <button
            type="button"
            onClick={onClose}
            className="rounded-lg border border-border bg-surface-2 px-3.5 py-2 text-[12.5px]"
          >
            Cancel
          </button>
          <button
            type="button"
            onClick={open}
            disabled={!valid || !cluster}
            className="rounded-lg bg-accent px-3.5 py-2 text-[12.5px] font-semibold text-white disabled:opacity-60"
          >
            Open in new tab
          </button>
        </div>
      </div>
    </div>
  );
}
