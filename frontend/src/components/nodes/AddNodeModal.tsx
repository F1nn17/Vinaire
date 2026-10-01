"use client";

import { useEffect, useRef, useState } from "react";
import {
  fetchHostKey,
  getProvisionJob,
  provisionStreamUrl,
  startProvision,
  useProvisionDefaults,
  type HostKey,
  type ProvisionEvent,
} from "@/lib/api/provision";
import { applyErrorMessage } from "@/components/editor/useYamlApply";
import {
  Fingerprint,
  TargetFields,
  emptyTarget,
  inputClass,
  targetComplete,
  toSshTarget,
  type TargetForm,
} from "@/components/ssh/shared";

type Phase = "form" | "confirm" | "running" | "done";

// "Add node" (Roadmap Phase 30): point Vinaire at a bare server and it joins
// the selected cluster as a worker, over SSH. Three steps — fill in the form,
// confirm the server's host key fingerprint (nothing secret is sent before
// that), watch the live install log. Credentials live only in this
// component's state and are wiped as soon as the request is sent. Mounted only
// while open, so every open starts empty.
export function AddNodeModal({
  onClose,
  onFinished,
}: {
  onClose: () => void;
  onFinished: () => void;
}) {
  const { data: defaults } = useProvisionDefaults();

  const [phase, setPhase] = useState<Phase>("form");
  const [node, setNode] = useState<TargetForm>(emptyTarget("root"));
  const [cp, setCp] = useState<TargetForm>(emptyTarget("root"));
  const [tokenMode, setTokenMode] = useState<"manual" | "controlplane">(
    "manual",
  );
  const [token, setToken] = useState("");
  const [serverUrl, setServerUrl] = useState("");
  const [nodeName, setNodeName] = useState("");
  const [version, setVersion] = useState("");

  const [nodeKey, setNodeKey] = useState<HostKey | null>(null);
  const [cpKey, setCpKey] = useState<HostKey | null>(null);
  const [nodeOk, setNodeOk] = useState(false);
  const [cpOk, setCpOk] = useState(false);

  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [events, setEvents] = useState<ProvisionEvent[]>([]);
  const [result, setResult] = useState<{
    status: "succeeded" | "failed";
    text: string;
  } | null>(null);
  const logRef = useRef<HTMLDivElement>(null);

  const usingCp = tokenMode === "controlplane";
  const formValid =
    targetComplete(node) &&
    (usingCp ? targetComplete(cp) : token.trim() !== "") &&
    (usingCp || serverUrl.trim() !== "");

  async function handleContinue() {
    setBusy(true);
    setError(null);
    try {
      const nk = await fetchHostKey(node.host.trim(), Number(node.port));
      const ck = usingCp
        ? await fetchHostKey(cp.host.trim(), Number(cp.port))
        : null;
      setNodeKey(nk);
      setCpKey(ck);
      setNodeOk(false);
      setCpOk(false);
      setPhase("confirm");
    } catch (err) {
      setError(applyErrorMessage(err));
    } finally {
      setBusy(false);
    }
  }

  // Merge new events by sequence number: the WebSocket replays what the client
  // may already have, and the polling fallback overlaps it.
  function mergeEvents(incoming: ProvisionEvent[]) {
    setEvents((prev) => {
      const seen = new Set(prev.map((e) => e.seq));
      const fresh = incoming.filter((e) => !seen.has(e.seq));
      return fresh.length === 0 ? prev : [...prev, ...fresh];
    });
    const last = incoming.find((e) => e.kind === "status");
    if (last?.status && last.status !== "running") {
      setResult({
        status: last.status,
        text: last.status === "failed" ? last.text : "Node joined the cluster.",
      });
      setPhase("done");
    }
  }

  function watch(jobId: string) {
    let finished = false;
    let lastSeq = -1;
    const onEvents = (evs: ProvisionEvent[]) => {
      for (const e of evs) lastSeq = Math.max(lastSeq, e.seq);
      if (evs.some((e) => e.kind === "status")) finished = true;
      mergeEvents(evs);
    };

    const ws = new WebSocket(provisionStreamUrl(jobId, -1));
    ws.onmessage = (m) => onEvents([JSON.parse(m.data) as ProvisionEvent]);
    // If the stream drops before the run ends, fall back to polling the job's
    // log (the run itself keeps going on the server regardless).
    ws.onclose = () => {
      if (finished) return;
      const timer = setInterval(async () => {
        try {
          const snap = await getProvisionJob(jobId);
          onEvents(snap.events.filter((e) => e.seq > lastSeq));
          if (snap.status !== "running") clearInterval(timer);
        } catch {
          clearInterval(timer);
        }
      }, 3000);
    };
  }

  async function handleStart() {
    if (!nodeKey) return;
    setBusy(true);
    setError(null);
    try {
      const { jobId } = await startProvision({
        provider: "k3s",
        node: toSshTarget(node, nodeKey.fingerprint),
        controlPlane:
          usingCp && cpKey ? toSshTarget(cp, cpKey.fingerprint) : undefined,
        serverUrl: serverUrl.trim() || undefined,
        token: usingCp ? undefined : token.trim(),
        version: version.trim() || undefined,
        nodeName: nodeName.trim() || undefined,
      });
      // The request is gone; don't keep the secrets around in the page.
      setNode((n) => ({ ...n, password: "", privateKey: "", passphrase: "" }));
      setCp((n) => ({ ...n, password: "", privateKey: "", passphrase: "" }));
      setToken("");
      setPhase("running");
      watch(jobId);
    } catch (err) {
      setError(applyErrorMessage(err));
    } finally {
      setBusy(false);
    }
  }

  useEffect(() => {
    const el = logRef.current;
    if (el) el.scrollTop = el.scrollHeight;
  }, [events]);

  const locked = phase === "running";

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 p-4">
      <div className="flex max-h-[92vh] w-full max-w-2xl flex-col rounded-xl border border-border bg-surface p-5">
        <div className="text-sm font-semibold">Add node</div>
        <div className="mt-1 text-xs text-text-faint">
          Vinaire logs into the server over SSH and runs the k3s installer as
          root to join it to this cluster. Credentials are used once, held in
          memory only for the run, and never stored.
        </div>

        <div className="mt-4 overflow-auto pr-1">
          {phase === "form" && (
            <FormStep
              node={node}
              setNode={setNode}
              cp={cp}
              setCp={setCp}
              tokenMode={tokenMode}
              setTokenMode={setTokenMode}
              token={token}
              setToken={setToken}
              serverUrl={serverUrl}
              setServerUrl={setServerUrl}
              nodeName={nodeName}
              setNodeName={setNodeName}
              version={version}
              setVersion={setVersion}
              suggestedUrl={defaults?.serverUrl}
              suggestedVersion={defaults?.k3sVersion}
            />
          )}

          {phase === "confirm" && nodeKey && (
            <div className="flex flex-col gap-4">
              <div className="text-xs text-text-muted">
                Before any credentials are sent, confirm these are the servers
                you meant. On each machine, run{" "}
                <span className="font-mono">
                  ssh-keygen -lf /etc/ssh/ssh_host_*_key.pub
                </span>{" "}
                and compare. If they differ, stop — someone may be intercepting
                the connection.
              </div>
              <Fingerprint
                label="New node"
                hostKey={nodeKey}
                host={node.host}
                confirmed={nodeOk}
                onConfirm={setNodeOk}
              />
              {cpKey && (
                <Fingerprint
                  label="Control plane"
                  hostKey={cpKey}
                  host={cp.host}
                  confirmed={cpOk}
                  onConfirm={setCpOk}
                />
              )}
            </div>
          )}

          {(phase === "running" || phase === "done") && (
            <div className="flex flex-col gap-3">
              <div
                ref={logRef}
                className="h-72 overflow-auto rounded-lg border border-border bg-code-bg p-3 font-mono text-[11.5px] leading-[1.55]"
              >
                {events.map((e) => (
                  <div
                    key={e.seq}
                    className={
                      e.kind === "step"
                        ? "mt-2 font-semibold text-accent-text"
                        : e.kind === "status" && e.status === "failed"
                          ? "text-err"
                          : e.kind === "status"
                            ? "text-ok"
                            : "text-text-muted"
                    }
                  >
                    {e.kind === "step" ? `▸ ${e.text}` : e.text}
                  </div>
                ))}
                {phase === "running" && (
                  <div className="mt-1 text-text-faint">…</div>
                )}
              </div>
              {result && (
                <div
                  className={`rounded-lg border bg-surface-2 p-3 text-xs ${
                    result.status === "succeeded"
                      ? "border-ok/30 text-ok"
                      : "border-err/30 text-err"
                  }`}
                >
                  {result.text}
                </div>
              )}
            </div>
          )}
        </div>

        {error && (
          <div className="mt-3 rounded-lg border border-err/30 bg-surface-2 p-3 text-xs text-err">
            {error}
          </div>
        )}

        <div className="mt-4 flex justify-end gap-2.5">
          {phase === "confirm" && (
            <button
              type="button"
              onClick={() => setPhase("form")}
              disabled={busy}
              className="rounded-lg border border-border bg-surface-2 px-3.5 py-2 text-[12.5px]"
            >
              Back
            </button>
          )}
          {phase !== "done" && (
            <button
              type="button"
              onClick={onClose}
              disabled={locked}
              className="rounded-lg border border-border bg-surface-2 px-3.5 py-2 text-[12.5px] disabled:opacity-50"
            >
              Cancel
            </button>
          )}
          {phase === "form" && (
            <button
              type="button"
              onClick={() => void handleContinue()}
              disabled={!formValid || busy}
              className="rounded-lg bg-accent px-3.5 py-2 text-[12.5px] font-semibold text-white disabled:opacity-60"
            >
              {busy ? "Checking host…" : "Continue"}
            </button>
          )}
          {phase === "confirm" && (
            <button
              type="button"
              onClick={() => void handleStart()}
              disabled={!nodeOk || (cpKey !== null && !cpOk) || busy}
              className="rounded-lg bg-accent px-3.5 py-2 text-[12.5px] font-semibold text-white disabled:opacity-60"
            >
              {busy ? "Starting…" : "Add node"}
            </button>
          )}
          {phase === "done" && (
            <button
              type="button"
              onClick={() => {
                if (result?.status === "succeeded") onFinished();
                onClose();
              }}
              className="rounded-lg bg-accent px-3.5 py-2 text-[12.5px] font-semibold text-white"
            >
              Close
            </button>
          )}
        </div>
      </div>
    </div>
  );
}

function FormStep(props: {
  node: TargetForm;
  setNode: (t: TargetForm) => void;
  cp: TargetForm;
  setCp: (t: TargetForm) => void;
  tokenMode: "manual" | "controlplane";
  setTokenMode: (m: "manual" | "controlplane") => void;
  token: string;
  setToken: (v: string) => void;
  serverUrl: string;
  setServerUrl: (v: string) => void;
  nodeName: string;
  setNodeName: (v: string) => void;
  version: string;
  setVersion: (v: string) => void;
  suggestedUrl?: string;
  suggestedVersion?: string;
}) {
  const usingCp = props.tokenMode === "controlplane";
  return (
    <div className="flex flex-col gap-5">
      <TargetFields
        title="New node"
        value={props.node}
        onChange={props.setNode}
        idPrefix="node"
      />

      <div className="flex flex-col gap-3">
        <div className="text-xs font-semibold">Joining the cluster</div>
        <div className="flex gap-4 text-xs">
          <label className="flex items-center gap-1.5">
            <input
              type="radio"
              checked={!usingCp}
              onChange={() => props.setTokenMode("manual")}
            />
            I&apos;ll paste the join token
          </label>
          <label className="flex items-center gap-1.5">
            <input
              type="radio"
              checked={usingCp}
              onChange={() => props.setTokenMode("controlplane")}
            />
            Read it from the control plane over SSH
          </label>
        </div>

        {!usingCp && (
          <>
            <label className="flex flex-col gap-1.5 text-xs text-text-muted">
              Join token
              <input
                aria-label="Join token"
                type="password"
                autoComplete="off"
                value={props.token}
                onChange={(e) => props.setToken(e.target.value)}
                placeholder="K10…::server:…"
                className={inputClass}
              />
              <span className="text-[11px] text-text-faint">
                On a k3s server:{" "}
                <span className="font-mono">
                  sudo cat /var/lib/rancher/k3s/server/node-token
                </span>
              </span>
            </label>
            <label className="flex flex-col gap-1.5 text-xs text-text-muted">
              Cluster API address (as reachable from the new node)
              <input
                aria-label="Cluster API address"
                value={props.serverUrl}
                onChange={(e) => props.setServerUrl(e.target.value)}
                placeholder="https://10.0.0.10:6443"
                className={inputClass}
              />
              {props.suggestedUrl && (
                <button
                  type="button"
                  onClick={() => props.setServerUrl(props.suggestedUrl ?? "")}
                  className="w-fit text-left text-[11px] text-accent-text"
                >
                  Vinaire reaches this cluster at {props.suggestedUrl} — use it
                  (it may not be reachable from the new node)
                </button>
              )}
            </label>
          </>
        )}

        {usingCp && (
          <>
            <TargetFields
              title="Control plane"
              value={props.cp}
              onChange={props.setCp}
              idPrefix="cp"
            />
            <label className="flex flex-col gap-1.5 text-xs text-text-muted">
              Cluster API address (optional — defaults to the control plane on
              :6443)
              <input
                aria-label="Cluster API address"
                value={props.serverUrl}
                onChange={(e) => props.setServerUrl(e.target.value)}
                placeholder="https://10.0.0.10:6443"
                className={inputClass}
              />
            </label>
          </>
        )}

        <div className="grid grid-cols-2 gap-3">
          <label className="flex flex-col gap-1.5 text-xs text-text-muted">
            Node name (optional)
            <input
              aria-label="Node name"
              value={props.nodeName}
              onChange={(e) => props.setNodeName(e.target.value)}
              placeholder="defaults to the hostname"
              className={inputClass}
            />
          </label>
          <label className="flex flex-col gap-1.5 text-xs text-text-muted">
            k3s version (optional)
            <input
              aria-label="k3s version"
              value={props.version}
              onChange={(e) => props.setVersion(e.target.value)}
              placeholder={props.suggestedVersion || "latest"}
              className={inputClass}
            />
          </label>
        </div>
      </div>
    </div>
  );
}
