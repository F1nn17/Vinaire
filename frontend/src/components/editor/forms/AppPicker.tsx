"use client";

import { useState } from "react";
import { t } from "@/lib/i18n/translate";
import {
  getResourceObject,
  useResourceTable,
  type TableRow,
} from "@/lib/api/allResources";
import { friendlyErrorMessage } from "@/lib/api/errors";
import type { FormState } from "./converters";
import {
  applyBackendFill,
  applyServiceFill,
  backendFromService,
  serviceFromWorkload,
  type BackendFill,
} from "./fromApp";

// Roadmap Phase 63: "for which application?" at the top of the Service / Ingress / HTTPRoute
// forms. Picking one reads it (as the signed-in user, through the All-resources API) and fills
// the name, namespace, selector, ports and backend; everything stays editable below, and the
// write is the usual reviewed apply — there is no endpoint of its own.

const workloadKinds = [
  { key: "apps/v1/deployments", label: "Deployment" },
  { key: "apps/v1/statefulsets", label: "StatefulSet" },
  { key: "apps/v1/daemonsets", label: "DaemonSet" },
];

const select =
  "rounded-lg border border-border bg-surface-2 px-2 py-1.5 font-mono text-xs outline-none";

const optionKey = (kind: string, r: TableRow) =>
  `${kind}|${r.namespace ?? ""}|${r.name}`;

export const hasAppPicker = (formType: string) =>
  formType === "service" || formType === "ingress" || formType === "httproute";

export function AppPicker({
  formType,
  state,
  onChange,
}: {
  formType: string;
  state: FormState;
  onChange: (next: FormState) => void;
}) {
  const [error, setError] = useState<string | null>(null);
  const [note, setNote] = useState<string | null>(null);
  // a picked Service with several ports: which one
  const [backend, setBackend] = useState<BackendFill | null>(null);

  const forService = formType === "service";
  const deployments = useResourceTable(
    forService ? workloadKinds[0].key : null,
    "",
  );
  const statefulsets = useResourceTable(
    forService ? workloadKinds[1].key : null,
    "",
  );
  const daemonsets = useResourceTable(
    forService ? workloadKinds[2].key : null,
    "",
  );
  const services = useResourceTable(forService ? null : "core/v1/services", "");
  const gateways = useResourceTable(
    formType === "httproute" ? "gateway.networking.k8s.io/v1/gateways" : null,
    "",
  );
  const classes = useResourceTable(
    formType === "ingress" ? "networking.k8s.io/v1/ingressclasses" : null,
    "",
  );

  const apps = forService
    ? [deployments.data, statefulsets.data, daemonsets.data].flatMap((tbl, i) =>
        (tbl?.rows ?? []).map((r) => ({ kind: workloadKinds[i], row: r })),
      )
    : [];

  async function pickApp(value: string) {
    if (!value) return;
    const [kindKey, namespace, name] = value.split("|");
    setError(null);
    setNote(null);
    try {
      const obj = await getResourceObject(kindKey, namespace, name);
      const fill = serviceFromWorkload(obj.yaml);
      if (!fill) throw new Error(t("Couldn't read that application."));
      onChange(applyServiceFill(state, fill));
      setNote(
        fill.noPorts
          ? t(
              "It declares no container port, so set the Service's ports by hand.",
            )
          : null,
      );
    } catch (e) {
      setError(friendlyErrorMessage(e));
    }
  }

  async function pickService(value: string) {
    if (!value) return;
    const [, namespace, name] = value.split("|");
    setError(null);
    setNote(null);
    try {
      const obj = await getResourceObject("core/v1/services", namespace, name);
      const fill = backendFromService(obj.yaml);
      if (!fill) throw new Error(t("Couldn't read that Service."));
      setBackend(fill.ports.length > 1 ? fill : null);
      onChange(applyBackendFill(state, fill));
    } catch (e) {
      setError(friendlyErrorMessage(e));
    }
  }

  function pickGateway(value: string) {
    if (!value) return;
    const [, namespace, name] = value.split("|");
    onChange({
      ...state,
      gatewayName: name,
      gatewayNamespace:
        namespace && namespace !== state.namespace ? namespace : "",
    });
  }

  const noGateways = formType === "httproute" && gateways.error;

  return (
    <div className="mb-3 flex flex-col gap-2 rounded-lg border border-border bg-surface-2 p-3 text-xs">
      <div className="font-semibold">
        {forService
          ? t("Fill from an application")
          : t("Fill from an existing Service")}
      </div>
      <div className="flex flex-wrap items-center gap-2">
        {forService ? (
          <select
            aria-label="Application"
            defaultValue=""
            onChange={(e) => void pickApp(e.target.value)}
            className={select}
          >
            <option value="">{t("Choose an application…")}</option>
            {apps.map(({ kind, row }) => (
              <option
                key={optionKey(kind.key, row)}
                value={optionKey(kind.key, row)}
              >
                {row.namespace}/{row.name} ({kind.label})
              </option>
            ))}
          </select>
        ) : (
          <select
            aria-label="Service"
            defaultValue=""
            onChange={(e) => void pickService(e.target.value)}
            className={select}
          >
            <option value="">{t("Choose a Service…")}</option>
            {(services.data?.rows ?? []).map((r) => (
              <option key={optionKey("svc", r)} value={optionKey("svc", r)}>
                {r.namespace}/{r.name}
              </option>
            ))}
          </select>
        )}
        {backend && (
          <select
            aria-label="Service port"
            value={String(state.servicePort ?? "")}
            onChange={(e) =>
              onChange(applyBackendFill(state, backend, Number(e.target.value)))
            }
            className={select}
          >
            {backend.ports.map((p) => (
              <option key={p.port} value={p.port}>
                {t("port {n}", { n: p.port })}
                {p.name ? ` (${p.name})` : ""}
              </option>
            ))}
          </select>
        )}
        {formType === "httproute" && !noGateways && (
          <select
            aria-label="Gateway"
            defaultValue=""
            onChange={(e) => pickGateway(e.target.value)}
            className={select}
          >
            <option value="">{t("Choose a Gateway…")}</option>
            {(gateways.data?.rows ?? []).map((r) => (
              <option key={optionKey("gw", r)} value={optionKey("gw", r)}>
                {r.namespace}/{r.name}
              </option>
            ))}
          </select>
        )}
        {formType === "ingress" && (classes.data?.rows.length ?? 0) > 0 && (
          <select
            aria-label="Ingress class"
            value={String(state.className ?? "")}
            onChange={(e) => onChange({ ...state, className: e.target.value })}
            className={select}
          >
            <option value="">{t("Ingress class: cluster default")}</option>
            {classes.data!.rows.map((r) => (
              <option key={r.name} value={r.name}>
                {r.name}
              </option>
            ))}
          </select>
        )}
      </div>
      {noGateways && (
        <div className="text-text-faint">
          {t(
            "Gateway API isn't installed in this cluster (or you may not list Gateways) — type the Gateway's name below.",
          )}
        </div>
      )}
      {note && <div className="text-warn">{note}</div>}
      {error && <div className="text-err">{error}</div>}
    </div>
  );
}
