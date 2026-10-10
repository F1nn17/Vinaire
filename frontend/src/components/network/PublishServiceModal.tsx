"use client";

import { useEffect, useMemo, useState } from "react";
import { parse, stringify } from "yaml";
import { t } from "@/lib/i18n/translate";
import { getResourceObject, type TableRow } from "@/lib/api/allResources";
import { createResource } from "@/lib/api/resources";
import { applyErrorMessage } from "@/components/editor/useYamlApply";
import { CodeBlock } from "@/components/ui/CodeBlock";

type SvcPort = {
  name?: string;
  port: number;
  targetPort?: number | string;
  protocol?: string;
};
type Source = {
  selector: Record<string, string>;
  ports: SvcPort[];
};

const inputClass =
  "w-full rounded-lg border border-border bg-surface-2 px-3 py-1.75 text-xs outline-none";
const dnsLabel = /^[a-z0-9]([-a-z0-9]{0,61}[a-z0-9])?$/;

// Publish something that already runs — a database installed long ago — outside the cluster, without
// touching it: a NEW Service of type NodePort or LoadBalancer is created that points at the same
// pods (the same selector) and the same ports as an existing Service. The existing Service and
// whatever uses it stay exactly as they were; deleting the new one closes the door again. Created
// through the generic YAML create (Phase 25), as the signed-in user — no write path of its own.
export function PublishServiceModal({
  services,
  initial,
  onClose,
  onCreated,
}: {
  services: TableRow[];
  // a Service to start from (the one ticked in the list)
  initial?: { namespace: string; name: string };
  onClose: () => void;
  onCreated: () => void;
}) {
  const candidates = useMemo(
    () =>
      services.filter(
        (s) =>
          s.namespace &&
          !(s.namespace === "default" && s.name === "kubernetes"),
      ),
    [services],
  );
  const [pick, setPick] = useState(
    initial ? `${initial.namespace}|${initial.name}` : "",
  );
  const [type, setType] = useState<"NodePort" | "LoadBalancer">("NodePort");
  const [name, setName] = useState("");
  const [nodePort, setNodePort] = useState("");
  const [source, setSource] = useState<Source | null>(null);
  const [chosen, setChosen] = useState<Set<number>>(new Set());
  const [loadError, setLoadError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const [ns, svc] = pick.split("|");

  // read the chosen Service: its selector and ports are what the new one copies
  useEffect(() => {
    if (!ns || !svc) return;
    let cancelled = false;
    getResourceObject("core/v1/services", ns, svc)
      .then((obj) => {
        if (cancelled) return;
        const doc = parse(obj.yaml) as {
          spec?: { selector?: Record<string, string>; ports?: SvcPort[] };
        } | null;
        const ports = doc?.spec?.ports ?? [];
        setSource({ selector: doc?.spec?.selector ?? {}, ports });
        setChosen(new Set(ports.map((_, i) => i)));
        setLoadError(null);
      })
      .catch((e) => {
        if (!cancelled) {
          setSource(null);
          setLoadError(applyErrorMessage(e));
        }
      });
    return () => {
      cancelled = true;
    };
  }, [ns, svc]);

  const effectiveName =
    name || (svc ? `${svc}-${type === "NodePort" ? "nodeport" : "lb"}` : "");
  const noSelector = !!source && Object.keys(source.selector).length === 0;
  const portsPicked = (source?.ports ?? []).filter((_, i) => chosen.has(i));

  const problem = !ns
    ? t("Choose a Service.")
    : !dnsLabel.test(effectiveName)
      ? t("Lower-case letters, digits and dashes.")
      : noSelector
        ? t(
            "This Service has no selector (it points at fixed addresses), so there are no pods to publish.",
          )
        : portsPicked.length === 0
          ? t("Choose at least one port.")
          : type === "NodePort" &&
              nodePort &&
              !(
                /^[0-9]{5}$/.test(nodePort) &&
                +nodePort >= 30000 &&
                +nodePort <= 32767
              )
            ? t("A port from 30000 to 32767, or empty to let the cluster pick.")
            : null;

  const yaml = useMemo(() => {
    if (problem || !source) return "";
    return stringify(
      {
        apiVersion: "v1",
        kind: "Service",
        metadata: {
          name: effectiveName,
          namespace: ns,
          labels: {
            "app.kubernetes.io/managed-by": "vinaire",
            "vinaire.io/published-from": svc,
          },
        },
        spec: {
          type,
          selector: source.selector,
          ports: portsPicked.map((p, i) => ({
            ...(p.name ? { name: p.name } : {}),
            port: p.port,
            targetPort: p.targetPort ?? p.port,
            protocol: p.protocol ?? "TCP",
            ...(type === "NodePort" && nodePort && i === 0
              ? { nodePort: Number(nodePort) }
              : {}),
          })),
        },
      },
      { version: "1.1" },
    );
  }, [problem, source, effectiveName, ns, svc, type, portsPicked, nodePort]);

  async function create() {
    setBusy(true);
    setError(null);
    try {
      await createResource(yaml);
      onCreated();
      onClose();
    } catch (e) {
      setError(applyErrorMessage(e));
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 p-4">
      <div className="flex max-h-[90vh] w-full max-w-2xl flex-col gap-3 overflow-auto rounded-xl border border-border bg-surface p-5 text-sm">
        <div className="font-semibold">
          {t("Publish a Service outside the cluster")}
        </div>
        <div className="text-xs text-text-muted">
          {t(
            "Creates one more Service for the same pods, reachable from outside. The existing Service and everything that uses it stay as they are; delete the new one to close it again.",
          )}
        </div>

        <label className="flex flex-col gap-1 text-xs">
          <span className="font-semibold">{t("Publish this Service")}</span>
          <select
            className={inputClass}
            value={pick}
            onChange={(e) => {
              setPick(e.target.value);
              setName("");
            }}
          >
            <option value="">{t("Choose a Service…")}</option>
            {candidates.map((s) => (
              <option
                key={`${s.namespace}/${s.name}`}
                value={`${s.namespace}|${s.name}`}
              >
                {s.namespace}/{s.name}
              </option>
            ))}
          </select>
        </label>
        {loadError && <div className="text-xs text-err">{loadError}</div>}

        <div className="grid grid-cols-2 gap-3 text-xs">
          <label className="flex flex-col gap-1">
            <span className="font-semibold">{t("How")}</span>
            <select
              className={inputClass}
              value={type}
              onChange={(e) => setType(e.target.value as typeof type)}
            >
              <option value="NodePort">
                {t("A port on every node (NodePort)")}
              </option>
              <option value="LoadBalancer">
                {t("A load balancer (LoadBalancer)")}
              </option>
            </select>
          </label>
          <label className="flex flex-col gap-1">
            <span className="font-semibold">
              {t("Name of the new Service")}
            </span>
            <input
              className={inputClass}
              value={name}
              placeholder={effectiveName}
              onChange={(e) => setName(e.target.value)}
            />
          </label>
        </div>

        {type === "NodePort" && (
          <label className="flex flex-col gap-1 text-xs">
            <span className="font-semibold">{t("Node port")}</span>
            <input
              className={inputClass}
              value={nodePort}
              onChange={(e) => setNodePort(e.target.value)}
              placeholder="30000–32767"
            />
            <span className="text-text-muted">
              {t("For the first port below. Empty: the cluster picks one.")}
            </span>
          </label>
        )}

        {source && source.ports.length > 0 && (
          <div className="flex flex-col gap-1 text-xs">
            <span className="font-semibold">{t("Ports to publish")}</span>
            {source.ports.map((p, i) => (
              <label key={i} className="flex items-center gap-2">
                <input
                  type="checkbox"
                  checked={chosen.has(i)}
                  onChange={(e) => {
                    const next = new Set(chosen);
                    if (e.target.checked) next.add(i);
                    else next.delete(i);
                    setChosen(next);
                  }}
                />
                <span className="font-mono">
                  {p.name ? `${p.name} · ` : ""}
                  {p.port}/{p.protocol ?? "TCP"}
                </span>
              </label>
            ))}
          </div>
        )}

        <div className="rounded-lg border border-warn/40 bg-surface-2 p-2.5 text-[11px] text-warn">
          {t(
            "Anyone who can reach your nodes (or the load balancer) can try to connect. For a database, close the port with a firewall to the addresses that need it.",
          )}
        </div>

        {problem && ns && <div className="text-xs text-err">{problem}</div>}
        {yaml && <CodeBlock code={yaml} />}
        {error && <div className="text-xs text-err">{error}</div>}

        <div className="flex justify-end gap-2">
          <button
            type="button"
            onClick={onClose}
            className="rounded-lg border border-border px-4 py-2 text-xs"
          >
            {t("Cancel")}
          </button>
          <button
            type="button"
            disabled={!!problem || busy}
            onClick={() => void create()}
            className="rounded-lg bg-accent px-4 py-2 text-xs font-semibold text-white disabled:opacity-50"
          >
            {busy ? t("Creating…") : t("Publish")}
          </button>
        </div>
      </div>
    </div>
  );
}
