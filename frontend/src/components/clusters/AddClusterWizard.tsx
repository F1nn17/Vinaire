"use client";

import { useEffect, useRef, useState } from "react";
import useSWR from "swr";
import {
  addCluster,
  addDiscoveredCluster,
  discoverClusters,
  fetchClusterHostKey,
  getClusterManagement,
  importKubeconfig,
  previewKubeconfig,
  startClusterSsh,
  type DiscoveredCluster,
  type KubeconfigPreview,
} from "@/lib/api/clusters";
import {
  useProvisionOptions,
  type HostKey,
  type ProvisionEvent,
  type Provider,
} from "@/lib/api/provision";
import { ResetServerModal } from "@/components/ssh/ResetServerModal";
import { watchProvisionJob } from "@/lib/provisionWatch";
import type { ClusterType } from "@/lib/types";
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
} from "@/components/ssh/shared";
import {
  ClusterTypeBadge,
  clusterTypeLabels,
  clusterTypes,
} from "./ClusterTypeBadge";

type Mode = "discovered" | "kubeconfig" | "ssh-import" | "ssh-create" | "token";

const modeInfo: Record<Mode, { title: string; blurb: string }> = {
  discovered: {
    title: "Found on this machine",
    blurb:
      "Another cluster running next to Vinaire — a second k3s, k3d, kind or minikube — found from the kubeconfig files Vinaire can read.",
  },
  "ssh-import": {
    title: "Import existing (SSH)",
    blurb:
      "A k3s server on another machine: Vinaire logs in over SSH, reads its kubeconfig and connects.",
  },
  "ssh-create": {
    title: "Create new (SSH)",
    blurb:
      "A clean server becomes a new single-server k3s cluster, installed by Vinaire over SSH.",
  },
  token: {
    title: "Server URL + token",
    blurb:
      "The cluster's API address and a ServiceAccount token — the narrowest credential, and the safest to hand over.",
  },
  kubeconfig: {
    title: "Import via kubeconfig",
    blurb:
      "Paste or upload a kubeconfig — any distribution, including managed clusters. No SSH involved.",
  },
};

const TypeSelect = ({
  value,
  onChange,
}: {
  value: ClusterType | "";
  onChange: (v: ClusterType | "") => void;
}) => (
  <label className="flex flex-col gap-1.5 text-xs text-text-muted">
    Cluster type
    <select
      aria-label="Cluster type"
      value={value}
      onChange={(e) => onChange(e.target.value as ClusterType | "")}
      className={inputClass}
    >
      <option value="">Detect automatically</option>
      {clusterTypes.map((t) => (
        <option key={t} value={t}>
          {clusterTypeLabels[t]}
        </option>
      ))}
    </select>
  </label>
);

const NameField = ({
  value,
  onChange,
}: {
  value: string;
  onChange: (v: string) => void;
}) => (
  <label className="flex flex-col gap-1.5 text-xs text-text-muted">
    Name in Vinaire
    <input
      aria-label="Cluster name"
      value={value}
      onChange={(e) => onChange(e.target.value)}
      placeholder="home-lab"
      className={inputClass}
    />
  </label>
);

function ErrorBox({ message }: { message: string | null }) {
  if (!message) return null;
  return (
    <div className="rounded-lg border border-err/30 bg-surface-2 p-3 text-xs text-err">
      {message}
    </div>
  );
}

const AdminWarning = () => (
  <div className="rounded-lg border border-warn/30 bg-surface-2 p-3 text-xs text-warn">
    This credential can do <b>everything</b> in that cluster. Vinaire keeps it
    encrypted, but whoever controls Vinaire then controls that cluster too.
    Where you can, create a ServiceAccount with only the permissions you need
    and use its token instead.
  </div>
);

// "Add cluster" (Roadmap Phase 34): four ways to connect a cluster, one
// wizard. Nothing is stored until Vinaire has actually connected to the
// cluster; secrets typed here stay in this component's state and are wiped once
// sent.
export function AddClusterWizard({
  onClose,
  onAdded,
  onAddNodes,
}: {
  onClose: () => void;
  onAdded: () => void;
  // Called after a *created* cluster, for the optional "add worker nodes" step.
  onAddNodes: (clusterName: string) => void;
}) {
  const [mode, setMode] = useState<Mode | null>(null);
  const { data: mgmt } = useSWR("cluster-management", getClusterManagement);

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 p-4">
      <div className="flex max-h-[92vh] w-full max-w-2xl flex-col rounded-xl border border-border bg-surface p-5">
        <div className="flex items-center justify-between">
          <div className="text-sm font-semibold">
            Add cluster
            {mode && (
              <span className="font-normal text-text-muted">
                {" "}
                — {modeInfo[mode].title}
              </span>
            )}
          </div>
          <button
            type="button"
            onClick={onClose}
            aria-label="Close"
            className="text-text-muted"
          >
            ✕
          </button>
        </div>

        <div className="mt-4 overflow-auto pr-1">
          {!mgmt ? (
            <div className="p-6 text-sm text-text-muted">Loading…</div>
          ) : !mgmt.available ? (
            <div className="rounded-lg border border-border bg-surface-2 p-4 text-sm text-text-muted">
              {mgmt.reason}
            </div>
          ) : mode === null ? (
            <div className="flex flex-col gap-3">
              <div className="text-xs text-text-faint">
                Added clusters are kept encrypted
                {mgmt.store === "file"
                  ? " in Vinaire's data directory"
                  : " in a Secret in Vinaire's namespace"}
                {mgmt.encrypted
                  ? "."
                  : " — but this install has no encryption key yet, so they are stored UNENCRYPTED. Set CLUSTER_STORE_KEY or grant access to the vinaire-clusters-key Secret."}
              </div>
              {(Object.keys(modeInfo) as Mode[]).map((m) => (
                <button
                  key={m}
                  type="button"
                  onClick={() => setMode(m)}
                  className="rounded-lg border border-border bg-surface-2 p-3.5 text-left hover:border-accent"
                >
                  <div className="text-[13px] font-semibold">
                    {modeInfo[m].title}
                  </div>
                  <div className="mt-1 text-xs text-text-muted">
                    {modeInfo[m].blurb}
                  </div>
                </button>
              ))}
            </div>
          ) : mode === "discovered" ? (
            <DiscoveredPanel
              onAdded={onAdded}
              onClose={onClose}
              onSwitch={setMode}
            />
          ) : mode === "kubeconfig" ? (
            <KubeconfigPanel onAdded={onAdded} onClose={onClose} />
          ) : mode === "token" ? (
            <TokenPanel onAdded={onAdded} onClose={onClose} />
          ) : (
            <SshPanel
              mode={mode === "ssh-create" ? "create" : "import"}
              onAdded={onAdded}
              onClose={onClose}
              onAddNodes={onAddNodes}
            />
          )}
        </div>

        {mode !== null && mgmt?.available && (
          <div className="mt-3">
            <button
              type="button"
              onClick={() => setMode(null)}
              className="text-xs text-text-muted"
            >
              ← Choose another way
            </button>
          </div>
        )}
      </div>
    </div>
  );
}

// --- Found on this machine ---

function DiscoveredPanel({
  onAdded,
  onClose,
  onSwitch,
}: {
  onAdded: () => void;
  onClose: () => void;
  onSwitch: (m: Mode) => void;
}) {
  const { data, isLoading, mutate, isValidating } = useSWR(
    "cluster-discover",
    discoverClusters,
    { revalidateOnFocus: false },
  );
  const [picked, setPicked] = useState<DiscoveredCluster | null>(null);
  const [name, setName] = useState("");
  const [type, setType] = useState<ClusterType | "">("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  async function add() {
    if (!picked) return;
    setBusy(true);
    setError(null);
    try {
      await addDiscoveredCluster({
        id: picked.id,
        name: name.trim() || undefined,
        type: type || undefined,
      });
      onAdded();
      onClose();
    } catch (err) {
      setError(applyErrorMessage(err));
    } finally {
      setBusy(false);
    }
  }

  if (isLoading || !data) {
    return <div className="p-6 text-sm text-text-muted">Scanning…</div>;
  }
  if (!data.available) {
    return (
      <div className="flex flex-col gap-3">
        <div className="rounded-lg border border-border bg-surface-2 p-4 text-sm text-text-muted">
          {data.reason}
        </div>
        <div className="rounded-lg border border-border bg-surface-2 p-4 text-xs text-text-muted">
          <div className="font-semibold text-text">
            To add a cluster that runs on your own machine anyway
          </div>
          <div className="mt-1.5">
            Print its kubeconfig in a terminal on that machine and paste it into{" "}
            <button
              type="button"
              onClick={() => onSwitch("kubeconfig")}
              className="text-accent-text"
            >
              Import via kubeconfig
            </button>
            :
          </div>
          <pre className="mt-1.5 overflow-auto rounded-md bg-code-bg p-2.5 font-mono text-[11px]">
            kubectl config get-contexts{"\n"}kubectl config view --raw --minify
            --flatten --context &lt;context-name&gt;
          </pre>
          <div className="mt-1.5">
            The cluster&apos;s address must be reachable from where Vinaire runs
            (inside a cluster, <span className="font-mono">127.0.0.1</span>{" "}
            means the pod itself — use the machine&apos;s LAN address or{" "}
            <span className="font-mono">host.docker.internal</span> for k3d on
            Docker Desktop).
          </div>
        </div>
      </div>
    );
  }

  if (picked) {
    return (
      <div className="flex flex-col gap-4">
        <div className="rounded-lg border border-border bg-surface-2 p-3 text-xs">
          <div className="font-mono text-[12.5px]">{picked.context}</div>
          <div className="mt-1 text-text-muted">
            {picked.server} · from {picked.source}
          </div>
        </div>
        <NameField value={name} onChange={setName} />
        <TypeSelect value={type} onChange={setType} />
        <div className="text-xs text-text-faint">
          Only this one cluster&apos;s credentials are taken from the file —
          nothing else in it is read or kept.
        </div>
        {picked.clusterAdmin && <AdminWarning />}
        <ErrorBox message={error} />
        <div className="flex justify-end gap-2.5">
          <button
            type="button"
            onClick={() => setPicked(null)}
            disabled={busy}
            className="rounded-lg border border-border bg-surface-2 px-3.5 py-2 text-[12.5px]"
          >
            Back
          </button>
          <button
            type="button"
            onClick={() => void add()}
            disabled={busy}
            className="rounded-lg bg-accent px-3.5 py-2 text-[12.5px] font-semibold text-white disabled:opacity-60"
          >
            {busy ? "Connecting…" : "Add cluster"}
          </button>
        </div>
      </div>
    );
  }

  return (
    <div className="flex flex-col gap-3">
      <div className="flex items-center justify-between">
        <div className="text-xs text-text-faint">
          Looked in: {data.searchedPaths.join(", ")}
        </div>
        <button
          type="button"
          onClick={() => void mutate()}
          disabled={isValidating}
          className="rounded-lg border border-border bg-surface-2 px-3 py-1.5 text-xs"
        >
          {isValidating ? "Scanning…" : "Rescan"}
        </button>
      </div>

      {data.candidates.length === 0 && (
        <div className="rounded-lg border border-border bg-surface-2 p-4 text-sm text-text-muted">
          No kubeconfig with any cluster in it was found at those locations.
        </div>
      )}

      {data.candidates.map((c) => (
        <div
          key={c.id}
          className={`flex items-center justify-between gap-3 rounded-lg border border-border bg-surface-2 p-3 ${
            c.alreadyAdded ? "opacity-60" : ""
          }`}
        >
          <div className="min-w-0">
            <div className="flex items-center gap-2">
              <span className="font-mono text-[12.5px]">{c.context}</span>
              <ClusterTypeBadge type={c.type} />
              <span
                className={`text-[11px] ${c.reachable ? "text-ok" : "text-text-faint"}`}
              >
                {c.reachable ? "reachable" : "not reachable"}
              </span>
            </div>
            <div className="mt-0.5 truncate text-[11px] text-text-muted">
              {c.server}
              {c.version ? ` · ${c.version}` : ""}
              {c.clusterAdmin ? " · cluster-admin" : ""}
            </div>
            {c.problem && (
              <div className="mt-1 text-[11px] text-err">{c.problem}</div>
            )}
          </div>
          {c.alreadyAdded ? (
            <span className="text-xs text-text-faint">already added</span>
          ) : (
            <button
              type="button"
              disabled={!!c.problem || !c.reachable}
              onClick={() => {
                setPicked(c);
                setName(c.name);
                setType("");
              }}
              className="rounded-lg bg-accent px-3 py-1.5 text-xs font-semibold text-white disabled:opacity-50"
            >
              Add
            </button>
          )}
        </div>
      ))}

      {data.unreadable.map((u) => (
        <div
          key={u.path}
          className="rounded-lg border border-warn/30 bg-surface-2 p-3 text-xs text-warn"
        >
          <div className="font-mono">{u.path}</div>
          <div className="mt-1 text-text-muted">{u.hint}</div>
        </div>
      ))}
      {data.listeningApis.map((l) => (
        <div
          key={l.address}
          className="rounded-lg border border-border bg-surface-2 p-3 text-xs text-text-muted"
        >
          <span className="font-mono">{l.address}</span> — {l.note}
        </div>
      ))}
    </div>
  );
}

// --- Server URL + token ---

function TokenPanel({
  onAdded,
  onClose,
}: {
  onAdded: () => void;
  onClose: () => void;
}) {
  const [name, setName] = useState("");
  const [server, setServer] = useState("");
  const [token, setToken] = useState("");
  const [caData, setCaData] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  async function add() {
    setBusy(true);
    setError(null);
    try {
      await addCluster({
        name: name.trim(),
        server: server.trim(),
        token: token.trim(),
        caData: caData.trim() || undefined,
      });
      setToken("");
      onAdded();
      onClose();
    } catch (err) {
      setError(applyErrorMessage(err));
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="flex flex-col gap-4">
      <NameField value={name} onChange={setName} />
      <label className="flex flex-col gap-1.5 text-xs text-text-muted">
        Server URL
        <input
          aria-label="Server URL"
          value={server}
          onChange={(e) => setServer(e.target.value)}
          placeholder="https://10.0.0.20:6443"
          className={inputClass}
        />
      </label>
      <label className="flex flex-col gap-1.5 text-xs text-text-muted">
        ServiceAccount token
        <textarea
          aria-label="ServiceAccount token"
          rows={3}
          autoComplete="off"
          spellCheck={false}
          value={token}
          onChange={(e) => setToken(e.target.value)}
          placeholder="kubectl create token <serviceaccount> -n <namespace> --duration=..."
          className={inputClass}
        />
      </label>
      <label className="flex flex-col gap-1.5 text-xs text-text-muted">
        CA certificate (optional, PEM)
        <textarea
          aria-label="CA certificate"
          rows={3}
          spellCheck={false}
          value={caData}
          onChange={(e) => setCaData(e.target.value)}
          placeholder="-----BEGIN CERTIFICATE-----"
          className={inputClass}
        />
        {!caData.trim() && (
          <span className="text-[11px] text-warn">
            No CA certificate — TLS certificate verification will be skipped for
            this cluster.
          </span>
        )}
      </label>
      <ErrorBox message={error} />
      <div className="flex justify-end">
        <button
          type="button"
          onClick={() => void add()}
          disabled={!name.trim() || !server.trim() || !token.trim() || busy}
          className="rounded-lg bg-accent px-3.5 py-2 text-[12.5px] font-semibold text-white disabled:opacity-60"
        >
          {busy ? "Connecting…" : "Add cluster"}
        </button>
      </div>
    </div>
  );
}

// --- Import via kubeconfig ---

function KubeconfigPanel({
  onAdded,
  onClose,
}: {
  onAdded: () => void;
  onClose: () => void;
}) {
  const [text, setText] = useState("");
  const [context, setContext] = useState("");
  const [preview, setPreview] = useState<KubeconfigPreview | null>(null);
  const [name, setName] = useState("");
  const [type, setType] = useState<ClusterType | "">("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  async function check(ctx?: string) {
    setBusy(true);
    setError(null);
    try {
      const p = await previewKubeconfig(text, ctx);
      setPreview(p);
      setContext(p.selected || ctx || "");
      if (!name && p.selected) setName(p.selected);
    } catch (err) {
      setPreview(null);
      setError(applyErrorMessage(err));
    } finally {
      setBusy(false);
    }
  }

  async function add() {
    setBusy(true);
    setError(null);
    try {
      await importKubeconfig({
        name: name.trim(),
        kubeconfig: text,
        context: context || undefined,
        type: type || undefined,
      });
      setText("");
      onAdded();
      onClose();
    } catch (err) {
      setError(applyErrorMessage(err));
    } finally {
      setBusy(false);
    }
  }

  async function onFile(file: File | undefined) {
    if (!file) return;
    setText(await file.text());
    setPreview(null);
  }

  return (
    <div className="flex flex-col gap-4">
      <label className="flex flex-col gap-1.5 text-xs text-text-muted">
        Kubeconfig
        <textarea
          aria-label="Kubeconfig"
          rows={8}
          spellCheck={false}
          autoComplete="off"
          value={text}
          onChange={(e) => {
            setText(e.target.value);
            setPreview(null);
          }}
          placeholder="apiVersion: v1&#10;kind: Config&#10;…"
          className={inputClass}
        />
      </label>
      <label className="flex items-center gap-2 text-xs text-text-muted">
        …or upload a file
        <input
          type="file"
          aria-label="Upload kubeconfig"
          onChange={(e) => void onFile(e.target.files?.[0])}
          className="text-xs"
        />
      </label>
      <div className="text-[11px] text-text-faint">
        Certificates and tokens must be inline in the file. Exec/auth-provider
        plugins and file references are refused — they would run commands or
        read files on the Vinaire machine.
      </div>

      {!preview && (
        <div className="flex justify-end">
          <button
            type="button"
            onClick={() => void check()}
            disabled={!text.trim() || busy}
            className="rounded-lg bg-accent px-3.5 py-2 text-[12.5px] font-semibold text-white disabled:opacity-60"
          >
            {busy ? "Checking…" : "Check"}
          </button>
        </div>
      )}

      {preview && (
        <>
          {preview.contexts.length > 1 && (
            <label className="flex flex-col gap-1.5 text-xs text-text-muted">
              Context
              <select
                aria-label="Context"
                value={context}
                onChange={(e) => void check(e.target.value)}
                className={inputClass}
              >
                {preview.contexts.map((c) => (
                  <option key={c} value={c}>
                    {c}
                  </option>
                ))}
              </select>
            </label>
          )}
          {preview.problem ? (
            <ErrorBox message={preview.problem} />
          ) : (
            <>
              <div className="rounded-lg border border-border bg-surface-2 p-3 text-xs">
                <div className="font-mono">{preview.server}</div>
                <div className="mt-1 text-text-muted">
                  credential: {preview.credentialKind}
                </div>
              </div>
              {preview.insecureTls && (
                <div className="rounded-lg border border-warn/30 bg-surface-2 p-3 text-xs text-warn">
                  This kubeconfig turns TLS verification off for the cluster.
                </div>
              )}
              {preview.credentialKind === "client-certificate" && (
                <AdminWarning />
              )}
              <NameField value={name} onChange={setName} />
              <TypeSelect value={type} onChange={setType} />
              <div className="flex justify-end">
                <button
                  type="button"
                  onClick={() => void add()}
                  disabled={!name.trim() || busy}
                  className="rounded-lg bg-accent px-3.5 py-2 text-[12.5px] font-semibold text-white disabled:opacity-60"
                >
                  {busy ? "Connecting…" : "Add cluster"}
                </button>
              </div>
            </>
          )}
        </>
      )}
      <ErrorBox message={error} />
    </div>
  );
}

// --- Import / create over SSH ---

type SshPhase = "form" | "confirm" | "running" | "done";

function SshPanel({
  mode,
  onAdded,
  onClose,
  onAddNodes,
}: {
  mode: "import" | "create";
  onAdded: () => void;
  onClose: () => void;
  onAddNodes: (clusterName: string) => void;
}) {
  const [phase, setPhase] = useState<SshPhase>("form");
  const [name, setName] = useState("");
  const [node, setNode] = useState<TargetForm>(emptyTarget("root"));
  const [address, setAddress] = useState("");
  const [serverUrl, setServerUrl] = useState("");
  const [version, setVersion] = useState("");
  // Roadmap Phase 35: which distribution to install/import, and kubeadm's options.
  const [provider, setProvider] = useState<Provider>("k3s");
  const { data: options } = useProvisionOptions();
  const [minor, setMinor] = useState("");
  const [cni, setCni] = useState("");
  const [singleNode, setSingleNode] = useState(false);
  const [cpEndpoint, setCpEndpoint] = useState("");
  const [resetOpen, setResetOpen] = useState(false);
  const [hostKey, setHostKey] = useState<HostKey | null>(null);
  const [confirmed, setConfirmed] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [events, setEvents] = useState<ProvisionEvent[]>([]);
  const [result, setResult] = useState<{
    ok: boolean;
    text: string;
  } | null>(null);
  const logRef = useRef<HTMLDivElement>(null);
  const stopRef = useRef<(() => void) | null>(null);

  useEffect(() => () => stopRef.current?.(), []);
  useEffect(() => {
    const el = logRef.current;
    if (el) el.scrollTop = el.scrollHeight;
  }, [events]);

  const valid = name.trim() !== "" && targetComplete(node);

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

  async function start() {
    if (!hostKey) return;
    setBusy(true);
    setError(null);
    try {
      const { jobId } = await startClusterSsh(mode, {
        provider,
        name: name.trim(),
        node: toSshTarget(node, hostKey.fingerprint),
        address: address.trim() || undefined,
        serverUrl: serverUrl.trim() || undefined,
        version: provider === "k3s" ? version.trim() || undefined : undefined,
        kubeadm:
          provider === "kubeadm" && mode === "create"
            ? {
                minor: minor || undefined,
                cni: cni || undefined,
                singleNode,
                controlPlaneEndpoint: cpEndpoint.trim() || undefined,
              }
            : undefined,
      });
      // Request sent: don't keep the secrets around in the page.
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
            text: ok ? `Cluster "${name.trim()}" is added.` : last.text,
          });
          setPhase("done");
          if (ok) onAdded();
        }
      });
    } catch (err) {
      setError(applyErrorMessage(err));
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="flex flex-col gap-4">
      {phase === "form" && (
        <>
          <NameField value={name} onChange={setName} />
          <label className="flex flex-col gap-1.5 text-xs text-text-muted">
            Distribution
            <select
              aria-label="Distribution"
              value={provider}
              onChange={(e) => setProvider(e.target.value as Provider)}
              className={inputClass}
            >
              <option value="k3s">k3s (lightweight Kubernetes)</option>
              <option value="kubeadm">
                Kubernetes (kubeadm, Debian/Ubuntu)
              </option>
            </select>
          </label>
          <TargetFields
            title={
              mode === "create"
                ? "Clean server"
                : provider === "kubeadm"
                  ? "Server with a kubeadm control plane"
                  : "Server with k3s"
            }
            value={node}
            onChange={setNode}
            idPrefix="cluster"
          />
          <label className="flex flex-col gap-1.5 text-xs text-text-muted">
            {mode === "create"
              ? "Address the cluster will be reached by (its certificate will cover it)"
              : "Address to connect to the cluster by (optional)"}
            <input
              aria-label="Cluster address"
              value={address}
              onChange={(e) => setAddress(e.target.value)}
              placeholder={node.host || "defaults to the SSH host"}
              className={inputClass}
            />
            <span className="text-[11px] text-text-faint">
              {provider === "kubeadm"
                ? "The API certificate is issued for this address (--apiserver-cert-extra-sans), so connecting by another address fails TLS verification."
                : "k3s issues its certificate without the machine's public address unless told to, so connecting by another address fails TLS verification."}
            </span>
          </label>
          <label className="flex flex-col gap-1.5 text-xs text-text-muted">
            Full API URL (optional — overrides the above, e.g. a forwarded port)
            <input
              aria-label="API URL"
              value={serverUrl}
              onChange={(e) => setServerUrl(e.target.value)}
              placeholder="https://10.0.0.20:6443"
              className={inputClass}
            />
          </label>
          {mode === "create" && provider === "kubeadm" && (
            <div className="flex flex-col gap-3 rounded-lg border border-border bg-surface-2 p-3">
              <div className="text-xs font-semibold">Kubernetes (kubeadm)</div>
              <div className="grid grid-cols-2 gap-3">
                <label className="flex flex-col gap-1.5 text-xs text-text-muted">
                  Kubernetes version
                  <select
                    aria-label="Kubernetes version"
                    value={minor}
                    onChange={(e) => setMinor(e.target.value)}
                    className={inputClass}
                  >
                    <option value="">
                      Default
                      {options
                        ? ` (${options.kubeadm.versions.find((v) => v.default)?.minor})`
                        : ""}
                    </option>
                    {options?.kubeadm.versions.map((v) => (
                      <option key={v.minor} value={v.minor}>
                        {v.minor}
                      </option>
                    ))}
                  </select>
                </label>
                <label className="flex flex-col gap-1.5 text-xs text-text-muted">
                  Pod network (CNI)
                  <select
                    aria-label="Pod network"
                    value={cni}
                    onChange={(e) => setCni(e.target.value)}
                    className={inputClass}
                  >
                    {options?.kubeadm.cnis.map((c) => (
                      <option key={c.name} value={c.name}>
                        {c.label} {c.version}
                        {c.default ? " (default)" : ""}
                      </option>
                    ))}
                  </select>
                </label>
              </div>
              <label className="flex items-center gap-2 text-xs text-text-muted">
                <input
                  type="checkbox"
                  checked={singleNode}
                  onChange={(e) => setSingleNode(e.target.checked)}
                />
                Single node — let workloads run on the control plane too
              </label>
              <label className="flex flex-col gap-1.5 text-xs text-text-muted">
                Control-plane endpoint (optional)
                <input
                  aria-label="Control-plane endpoint"
                  value={cpEndpoint}
                  onChange={(e) => setCpEndpoint(e.target.value)}
                  placeholder="k8s.example.com:6443"
                  className={inputClass}
                />
                <span className="text-[11px] text-warn">
                  A VIP or DNS name for the API. Set this now if you might ever
                  want high availability — adding it after the cluster exists is
                  very hard.
                </span>
              </label>
              <div className="text-[11px] text-text-faint">
                Needs a Debian or Ubuntu server with at least 2 CPUs and 2 GB
                RAM that can reach pkgs.k8s.io and registry.k8s.io. Versions and
                pod networks are pinned in this Vinaire&apos;s configuration.
              </div>
            </div>
          )}
          {mode === "create" && provider === "k3s" && (
            <label className="flex flex-col gap-1.5 text-xs text-text-muted">
              k3s version (optional)
              <input
                aria-label="k3s version"
                value={version}
                onChange={(e) => setVersion(e.target.value)}
                placeholder="latest stable"
                className={inputClass}
              />
            </label>
          )}
        </>
      )}

      {phase === "confirm" && hostKey && (
        <div className="flex flex-col gap-4">
          <div className="text-xs text-text-muted">
            Before any credentials are sent, confirm this is the server you
            meant: run{" "}
            <span className="font-mono">
              ssh-keygen -lf /etc/ssh/ssh_host_*_key.pub
            </span>{" "}
            on it and compare. If they differ, stop — someone may be
            intercepting the connection.
          </div>
          <Fingerprint
            label={mode === "create" ? "Clean server" : "Server"}
            hostKey={hostKey}
            host={node.host}
            confirmed={confirmed}
            onConfirm={setConfirmed}
          />
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
              className={`rounded-lg border bg-surface-2 p-3 text-xs ${
                result.ok ? "border-ok/30 text-ok" : "border-err/30 text-err"
              }`}
            >
              {result.text}
            </div>
          )}
          {result?.ok && mode === "create" && provider === "kubeadm" && (
            <div className="rounded-lg border border-border bg-surface-2 p-3 text-xs text-text-muted">
              <div className="font-semibold text-text">
                A kubeadm cluster starts empty — what to install next
              </div>
              <ul className="mt-1.5 list-disc pl-5">
                <li>
                  An <b>Ingress controller</b> to expose services outside the
                  cluster.
                </li>
                <li>
                  A <b>StorageClass</b> (e.g. local-path or your CSI driver), or
                  PersistentVolumeClaims stay Pending.
                </li>
                <li>
                  <b>metrics-server</b> — without it CPU/memory show as
                  &ldquo;unavailable&rdquo; in Vinaire.
                </li>
              </ul>
              <div className="mt-1.5 text-text-faint">
                Vinaire&apos;s Marketplace will install these in one click (a
                later phase); until then use Helm or kubectl.
              </div>
            </div>
          )}
        </>
      )}

      {resetOpen && (
        <ResetServerModal
          provider={provider}
          host={node.host}
          port={node.port}
          username={node.username}
          onClose={() => setResetOpen(false)}
        />
      )}

      <ErrorBox message={error} />

      <div className="flex justify-end gap-2.5">
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
        {phase === "form" && (
          <button
            type="button"
            onClick={() => void cont()}
            disabled={!valid || busy}
            className="rounded-lg bg-accent px-3.5 py-2 text-[12.5px] font-semibold text-white disabled:opacity-60"
          >
            {busy ? "Checking host…" : "Continue"}
          </button>
        )}
        {phase === "confirm" && (
          <button
            type="button"
            onClick={() => void start()}
            disabled={!confirmed || busy}
            className="rounded-lg bg-accent px-3.5 py-2 text-[12.5px] font-semibold text-white disabled:opacity-60"
          >
            {busy
              ? "Starting…"
              : mode === "create"
                ? "Create cluster"
                : "Import cluster"}
          </button>
        )}
        {phase === "done" && result && !result.ok && (
          <button
            type="button"
            onClick={() => setResetOpen(true)}
            className="rounded-lg border border-err/40 bg-surface-2 px-3.5 py-2 text-[12.5px] text-err"
          >
            Reset this server…
          </button>
        )}
        {phase === "done" && result?.ok && mode === "create" && (
          <button
            type="button"
            onClick={() => onAddNodes(name.trim())}
            className="rounded-lg border border-border bg-surface-2 px-3.5 py-2 text-[12.5px]"
          >
            Add worker nodes…
          </button>
        )}
        {phase === "done" && (
          <button
            type="button"
            onClick={onClose}
            className="rounded-lg bg-accent px-3.5 py-2 text-[12.5px] font-semibold text-white"
          >
            Close
          </button>
        )}
      </div>
    </div>
  );
}
