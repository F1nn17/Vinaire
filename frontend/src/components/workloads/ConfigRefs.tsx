"use client";

import Link from "next/link";
import { useMemo } from "react";
import { parse } from "yaml";
import { t } from "@/lib/i18n/translate";
import { ConfigMapIcon, SecretIcon } from "@/components/icons";

type Ref = { kind: "secret" | "configmap"; name: string; via: string };

type Container = {
  env?: {
    name?: string;
    valueFrom?: {
      secretKeyRef?: { name?: string };
      configMapKeyRef?: { name?: string };
    };
  }[];
  envFrom?: {
    secretRef?: { name?: string };
    configMapRef?: { name?: string };
  }[];
};

type PodSpec = {
  containers?: Container[];
  initContainers?: Container[];
  imagePullSecrets?: { name?: string }[];
  volumes?: {
    name?: string;
    secret?: { secretName?: string };
    configMap?: { name?: string };
    projected?: {
      sources?: { secret?: { name?: string }; configMap?: { name?: string } }[];
    };
  }[];
};

// The Secrets and ConfigMaps a workload (or a pod) refers to — through environment variables,
// envFrom, volumes and imagePullSecrets — read from its own YAML. Each is a link to that object
// on its page. Nothing is read from the Secrets here; this only names them.
export function refsOf(yamlText: string): Ref[] {
  let doc: { spec?: PodSpec & { template?: { spec?: PodSpec } } } | null;
  try {
    doc = parse(yamlText);
  } catch {
    return [];
  }
  const spec: PodSpec = doc?.spec?.template?.spec ?? doc?.spec ?? {};
  const out: Ref[] = [];
  const add = (kind: Ref["kind"], name: string | undefined, via: string) => {
    if (name && !out.some((r) => r.kind === kind && r.name === name)) {
      out.push({ kind, name, via });
    }
  };
  for (const c of [
    ...(spec.containers ?? []),
    ...(spec.initContainers ?? []),
  ]) {
    for (const e of c.env ?? []) {
      add("secret", e.valueFrom?.secretKeyRef?.name, `env ${e.name ?? ""}`);
      add(
        "configmap",
        e.valueFrom?.configMapKeyRef?.name,
        `env ${e.name ?? ""}`,
      );
    }
    for (const f of c.envFrom ?? []) {
      add("secret", f.secretRef?.name, "envFrom");
      add("configmap", f.configMapRef?.name, "envFrom");
    }
  }
  for (const v of spec.volumes ?? []) {
    add("secret", v.secret?.secretName, `volume ${v.name ?? ""}`);
    add("configmap", v.configMap?.name, `volume ${v.name ?? ""}`);
    for (const s of v.projected?.sources ?? []) {
      add("secret", s.secret?.name, `volume ${v.name ?? ""}`);
      add("configmap", s.configMap?.name, `volume ${v.name ?? ""}`);
    }
  }
  for (const s of spec.imagePullSecrets ?? []) {
    add("secret", s.name, "imagePullSecrets");
  }
  return out;
}

export function ConfigRefs({
  yaml,
  namespace,
}: {
  yaml: string;
  namespace: string;
}) {
  const refs = useMemo(() => refsOf(yaml), [yaml]);
  if (refs.length === 0) return null;
  return (
    <div className="rounded-[10px] border border-border bg-surface p-5">
      <div className="mb-3 text-[13px] font-semibold">
        {t("Uses these Secrets and ConfigMaps")}
      </div>
      <div className="flex flex-col gap-2">
        {refs.map((r) => (
          <Link
            key={`${r.kind}/${r.name}`}
            href={`/${r.kind === "secret" ? "secrets" : "configmaps"}?namespace=${encodeURIComponent(namespace)}&name=${encodeURIComponent(r.name)}`}
            className="flex items-center gap-2 rounded-md border border-border bg-surface-2 px-2.5 py-1.5 text-xs hover:border-accent"
          >
            {r.kind === "secret" ? (
              <SecretIcon size={13} className="shrink-0 text-text-muted" />
            ) : (
              <ConfigMapIcon size={13} className="shrink-0 text-text-muted" />
            )}
            <span className="font-mono">{r.name}</span>
            <span className="ml-auto text-text-faint">
              {r.kind === "secret" ? "Secret" : "ConfigMap"} · {r.via}
            </span>
          </Link>
        ))}
      </div>
    </div>
  );
}
