"use client";

import { t } from "@/lib/i18n/translate";
import type { Preflight } from "@/lib/api/catalog";

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

export function InstallPlan({ preflight }: { preflight?: Preflight }) {
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
