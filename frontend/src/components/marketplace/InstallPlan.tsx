"use client";

import { t } from "@/lib/i18n/translate";
import { useState } from "react";
import useSWR from "swr";
import {
  installManifest,
  type InstallRequest,
  type Preflight,
} from "@/lib/api/catalog";
import { friendlyErrorMessage } from "@/lib/api/errors";
import { YamlEditor } from "@/components/editor/YamlEditor";

// Roadmap Phase 59: before pressing Install — what this install will create, the storage it asks
// for, and what the cluster seems to lack for it. All of it comes from the same client-side render
// the permission check already makes; nothing is created by looking.

function warning(w: { code: string; detail?: string }): string {
  switch (w.code) {
    case "no-default-storage-class":
      return t(
        "The chart asks for storage, but this cluster has no default StorageClass, so its volumes will stay Pending. Set a storageClass in the values, or mark a class as the default.",
      );
    case "missing-storage-class":
      return t("The StorageClass “{c}” doesn't exist in this cluster.", {
        c: w.detail ?? "",
      });
    case "no-ingress-controller":
      return t(
        "The chart creates an Ingress, but this cluster has no IngressClass — nothing will serve it until an ingress controller (Traefik, ingress-nginx…) is installed.",
      );
    case "missing-ingress-class":
      return t(
        "An Ingress asks for the class “{c}”, which this cluster doesn't have.",
        { c: w.detail ?? "" },
      );
  }
  return w.code;
}

// The full rendered YAML, fetched only when the person opens it (rendering a big chart takes a moment).
function Manifests({ request }: { request: InstallRequest }) {
  const { data, error, isLoading } = useSWR(
    ["install-manifest", JSON.stringify(request)],
    () => installManifest(request),
    { revalidateOnFocus: false, shouldRetryOnError: false },
  );
  if (isLoading)
    return <div className="text-text-faint">{t("Rendering…")}</div>;
  if (error)
    return <div className="text-err">{friendlyErrorMessage(error)}</div>;
  return (
    <div className="flex flex-col gap-1.5">
      <div className="h-[360px] overflow-hidden rounded-lg border border-border">
        <YamlEditor value={data?.manifest ?? ""} readOnly height="100%" />
      </div>
      <div className="text-text-faint">
        {t("Secret values are hidden.")}
        {data?.truncated ? ` ${t("The preview is cut at 512 KiB.")}` : ""}
      </div>
    </div>
  );
}

export function InstallPlan({
  preflight,
  request,
}: {
  preflight?: Preflight;
  request?: InstallRequest;
}) {
  const [show, setShow] = useState(false);
  if (!preflight || (preflight.created ?? []).length === 0) return null;
  return (
    <div className="flex flex-col gap-2 rounded-lg border border-border bg-surface p-3 text-xs">
      <div className="font-semibold">{t("This install will create")}</div>
      <div className="flex flex-wrap gap-1.5">
        {preflight.created.map((k) => (
          <span
            key={k.kind}
            className="rounded-md border border-border bg-surface-2 px-2 py-1 font-mono text-[11px]"
          >
            {k.count} × {k.kind}
          </span>
        ))}
      </div>
      {preflight.volumes.length > 0 && (
        <div className="text-text-muted">
          {t("Storage")}:{" "}
          {preflight.volumes
            .map(
              (v) =>
                `${v.name}${v.size ? ` ${v.size}` : ""}${
                  v.storageClass ? ` (${v.storageClass})` : ""
                }`,
            )
            .join(" · ")}
        </div>
      )}
      {request && (
        <div className="flex flex-col gap-1.5">
          <button
            type="button"
            onClick={() => setShow(!show)}
            aria-expanded={show}
            className="w-fit text-accent-text underline"
          >
            {show ? t("Hide the manifests") : t("Show the manifests")}
          </button>
          {show && <Manifests request={request} />}
        </div>
      )}
      {(preflight.warnings ?? []).map((w, i) => (
        <div
          key={i}
          className="rounded-md border border-warn/40 bg-surface-2 p-2 text-warn"
        >
          ⚠ {warning(w)}
        </div>
      ))}
    </div>
  );
}
