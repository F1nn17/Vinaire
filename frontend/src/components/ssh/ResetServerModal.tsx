"use client";

import { useEffect, useRef, useState } from "react";
import { fetchClusterHostKey } from "@/lib/api/clusters";
import {
  startReset,
  type HostKey,
  type ProvisionEvent,
} from "@/lib/api/provision";
import { watchProvisionJob } from "@/lib/provisionWatch";
import { applyErrorMessage } from "@/components/editor/useYamlApply";
import {
  Fingerprint,
  LogPane,
  TargetFields,
  emptyTarget,
  inputClass,
  targetComplete,
  toSshTarget,
  type TargetForm,
} from "./shared";

type Phase = "form" | "confirm" | "running" | "done";

// "Reset this server" (Roadmap Phase 35): undo a Kubernetes install on a machine
// whose install failed partway, or that is being taken out of its cluster. It
// is destructive and can't be undone, so it needs the SSH login again (nothing
// is kept from the earlier run), a fresh host-key confirmation, and the server's
// address typed out — which the backend re-checks.
export function ResetServerModal({
  provider,
  host,
  port,
  username,
  onClose,
}: {
  provider: "k3s" | "kubeadm";
  host: string;
  port: string;
  username: string;
  onClose: () => void;
}) {
  const [phase, setPhase] = useState<Phase>("form");
  const [node, setNode] = useState<TargetForm>({
    ...emptyTarget(username),
    host,
    port,
  });
  const [hostKey, setHostKey] = useState<HostKey | null>(null);
  const [confirmed, setConfirmed] = useState(false);
  const [typed, setTyped] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [events, setEvents] = useState<ProvisionEvent[]>([]);
  const [result, setResult] = useState<{ ok: boolean; text: string } | null>(
    null,
  );
  const logRef = useRef<HTMLDivElement>(null);
  const stopRef = useRef<(() => void) | null>(null);

  useEffect(() => () => stopRef.current?.(), []);
  useEffect(() => {
    const el = logRef.current;
    if (el) el.scrollTop = el.scrollHeight;
  }, [events]);

  async function cont() {
    setBusy(true);
    setError(null);
    try {
      setHostKey(
        await fetchClusterHostKey(node.host.trim(), Number(node.port)),
      );
      setConfirmed(false);
      setPhase("confirm");
    } catch (err) {
      setError(applyErrorMessage(err));
    } finally {
      setBusy(false);
    }
  }

  async function run() {
    if (!hostKey) return;
    setBusy(true);
    setError(null);
    try {
      const { jobId } = await startReset({
        provider,
        node: toSshTarget(node, hostKey.fingerprint),
        confirmHost: typed,
      });
      setNode((n) => ({ ...n, password: "", privateKey: "", passphrase: "" }));
      setPhase("running");
      stopRef.current = watchProvisionJob(jobId, (evs) => {
        setEvents((prev) => {
          const seen = new Set(prev.map((e) => e.seq));
          const fresh = evs.filter((e) => !seen.has(e.seq));
          return fresh.length === 0 ? prev : [...prev, ...fresh];
        });
        const last = evs.find((e) => e.kind === "status");
        if (last?.status && last.status !== "running") {
          const ok = last.status === "succeeded";
          setResult({
            ok,
            text: ok
              ? "The server was reset. You can set it up again from scratch."
              : last.text,
          });
          setPhase("done");
        }
      });
    } catch (err) {
      setError(applyErrorMessage(err));
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="fixed inset-0 z-[70] flex items-center justify-center bg-black/70 p-4">
      <div className="flex max-h-[92vh] w-full max-w-xl flex-col rounded-xl border border-err/40 bg-surface p-5">
        <div className="text-sm font-semibold text-err">Reset this server</div>
        <div className="mt-1 text-xs text-text-muted">
          {provider === "kubeadm"
            ? "Runs kubeadm reset and clears the CNI configuration, iptables rules and Kubernetes state on the machine."
            : "Runs k3s's own uninstall script on the machine."}{" "}
          Anything running on it as part of a cluster is removed. This
          can&apos;t be undone.
        </div>

        <div className="mt-4 overflow-auto pr-1">
          {phase === "form" && (
            <TargetFields
              title="SSH login to the server"
              value={node}
              onChange={setNode}
              idPrefix="reset"
            />
          )}
          {phase === "confirm" && hostKey && (
            <div className="flex flex-col gap-4">
              <Fingerprint
                label="Server"
                hostKey={hostKey}
                host={node.host}
                confirmed={confirmed}
                onConfirm={setConfirmed}
              />
              <label className="flex flex-col gap-1.5 text-xs text-text-muted">
                Type <span className="font-mono text-text">{node.host}</span> to
                confirm the reset
                <input
                  aria-label="Confirm server address"
                  value={typed}
                  onChange={(e) => setTyped(e.target.value)}
                  className={inputClass}
                />
              </label>
            </div>
          )}
          {(phase === "running" || phase === "done") && (
            <>
              <LogPane
                events={events}
                running={phase === "running"}
                paneRef={logRef}
              />
              {result && (
                <div
                  className={`mt-3 rounded-lg border bg-surface-2 p-3 text-xs ${
                    result.ok
                      ? "border-ok/30 text-ok"
                      : "border-err/30 text-err"
                  }`}
                >
                  {result.text}
                </div>
              )}
            </>
          )}
        </div>

        {error && (
          <div className="mt-3 rounded-lg border border-err/30 bg-surface-2 p-3 text-xs text-err">
            {error}
          </div>
        )}

        <div className="mt-4 flex justify-end gap-2.5">
          {phase !== "running" && (
            <button
              type="button"
              onClick={onClose}
              className="rounded-lg border border-border bg-surface-2 px-3.5 py-2 text-[12.5px]"
            >
              {phase === "done" ? "Close" : "Cancel"}
            </button>
          )}
          {phase === "form" && (
            <button
              type="button"
              onClick={() => void cont()}
              disabled={!targetComplete(node) || busy}
              className="rounded-lg bg-accent px-3.5 py-2 text-[12.5px] font-semibold text-white disabled:opacity-60"
            >
              {busy ? "Checking host…" : "Continue"}
            </button>
          )}
          {phase === "confirm" && (
            <button
              type="button"
              onClick={() => void run()}
              disabled={!confirmed || typed !== node.host || busy}
              className="rounded-lg bg-err px-3.5 py-2 text-[12.5px] font-semibold text-white disabled:opacity-50"
            >
              {busy ? "Starting…" : "Reset server"}
            </button>
          )}
        </div>
      </div>
    </div>
  );
}
