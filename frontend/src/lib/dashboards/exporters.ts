import { stringify } from "yaml";
import type { AppInstance } from "@/lib/api/apps";

// Roadmap Phase 74b: the exporter that makes an installed PostgreSQL or Redis visible to Prometheus,
// installed from the Dashboards page. This only *describes* the install — the Helm values of the
// Marketplace's own exporter charts, for an instance Vinaire found. The install itself is the
// Marketplace's (the Helm SDK, as the caller, audited); a password is never read or typed here: the
// exporter is given the name of the Secret that already holds it.

export type ExporterKind = "postgresql" | "redis";

export const exporterCharts: Record<
  ExporterKind,
  { repo: string; chart: string; port: number; short: string }
> = {
  postgresql: {
    repo: "prometheus-community",
    chart: "prometheus-postgres-exporter",
    port: 9187,
    short: "pg-exporter",
  },
  redis: {
    repo: "prometheus-community",
    chart: "prometheus-redis-exporter",
    port: 9121,
    short: "redis-exporter",
  },
};

export function exporterKindOf(appId: string): ExporterKind | null {
  return appId === "postgresql" || appId === "redis" ? appId : null;
}

// How the Prometheus in the cluster finds a Service to read: the plain Prometheus chart (the Metrics
// stack page's "standard" profile) reads Services marked with annotations; with prometheus-operator
// (kube-prometheus-stack) a ServiceMonitor is needed, and unless the operator was set to look at all
// of them it must carry the stack's `release` label.
export type ScrapeFlavor =
  { kind: "annotations" } | { kind: "servicemonitor"; release: string };

export function scrapeFlavorOf(status?: {
  prometheus?: { service: string };
  operatorFound: boolean;
  release?: string;
}): ScrapeFlavor {
  const service = status?.prometheus?.service ?? "";
  const operator =
    !!status?.operatorFound && !/^prometheus-server$/.test(service);
  return operator
    ? {
        kind: "servicemonitor",
        release: status?.release || "kube-prometheus-stack",
      }
    : { kind: "annotations" };
}

const dns1123 = /^[a-z0-9]([-a-z0-9]{0,251}[a-z0-9])?$/;
const word = /^[A-Za-z0-9_][A-Za-z0-9_.-]{0,62}$/;
const keyRe = /^[-._a-zA-Z0-9]{1,253}$/;

export type ExporterInput = {
  kind: ExporterKind;
  instance: AppInstance;
  service: string;
  port: string;
  // PostgreSQL
  user: string;
  database: string;
  // the Secret that holds the password (Redis: empty when the instance has none)
  secret: string;
  secretKey: string;
  // Redis only: an ACL user, when the instance uses one
  redisUser: string;
};

// What is wrong with the input, by field — empty when it can be installed.
export function validateExporter(i: ExporterInput): Record<string, string> {
  const e: Record<string, string> = {};
  if (!dns1123.test(i.service)) e.service = "Pick the instance's Service.";
  if (!/^[0-9]{1,5}$/.test(i.port) || Number(i.port) > 65535)
    e.port = "A port number.";
  if (i.kind === "postgresql") {
    if (!word.test(i.user)) e.user = "A user name.";
    if (!word.test(i.database)) e.database = "A database name.";
  }
  const needsSecret = i.kind === "postgresql" || i.secret !== "";
  if (needsSecret) {
    if (!dns1123.test(i.secret))
      e.secret = "Pick the Secret with the password.";
    if (!keyRe.test(i.secretKey)) e.secretKey = "Pick the key in that Secret.";
  }
  if (i.kind === "redis" && i.redisUser && !word.test(i.redisUser))
    e.redisUser = "A user name.";
  return e;
}

// The release's name: next to the instance it watches, short enough for Helm.
export function exporterRelease(kind: ExporterKind, instance: string): string {
  const base = `${instance}-${exporterCharts[kind].short}`
    .toLowerCase()
    .replace(/[^a-z0-9-]+/g, "-")
    .replace(/^-+|-+$/g, "");
  return base.slice(0, 53).replace(/-+$/, "");
}

function scrape(kind: ExporterKind, flavor: ScrapeFlavor) {
  return flavor.kind === "annotations"
    ? {
        service: {
          annotations: {
            "prometheus.io/scrape": "true",
            "prometheus.io/port": String(exporterCharts[kind].port),
          },
        },
      }
    : {
        serviceMonitor: { enabled: true, labels: { release: flavor.release } },
      };
}

// The Helm values for the exporter chart. The host is the Service's full name, so the exporter
// reaches the instance from wherever it runs.
export function exporterValues(i: ExporterInput, flavor: ScrapeFlavor): string {
  const host = `${i.service}.${i.instance.namespace}.svc.cluster.local`;
  if (i.kind === "postgresql") {
    return stringify({
      config: {
        datasource: {
          host,
          port: i.port,
          database: i.database,
          user: i.user,
          passwordSecret: { name: i.secret, key: i.secretKey },
        },
      },
      ...scrape("postgresql", flavor),
    });
  }
  return stringify({
    redisAddress: `redis://${host}:${i.port}`,
    ...(i.secret
      ? {
          auth: {
            enabled: true,
            secret: { name: i.secret, key: i.secretKey },
            ...(i.redisUser ? { redisUser: i.redisUser } : {}),
          },
        }
      : {}),
    ...scrape("redis", flavor),
  });
}

// ---- guessing, so that the form is filled in before the person touches it -----------------------

// The instance's Service: the one named like it; else one that starts with its name (CloudNativePG's
// `<name>-rw`); else the first one.
export function guessService(instance: string, services: string[]): string {
  return (
    services.find((s) => s === instance) ??
    services.find((s) => s === `${instance}-rw`) ??
    services.find((s) => s.startsWith(instance) && !s.endsWith("-hl")) ??
    services[0] ??
    ""
  );
}

export type SecretInfo = { name: string; keys: string[] };

// The Secret with the password: the one named like the instance (what "Install from an image" makes),
// CloudNativePG's `<name>-app`, else the first Secret that has a key called password.
export function guessSecret(
  instance: string,
  secrets: SecretInfo[],
  strict = false,
): SecretInfo | undefined {
  const usable = secrets.filter((s) => !s.name.startsWith("sh.helm.release"));
  return (
    usable.find((s) => s.name === instance && s.keys.includes("password")) ??
    usable.find(
      (s) => s.name === `${instance}-app` && s.keys.includes("password"),
    ) ??
    // any Secret with a password in it is a fair guess for a database; for Redis it is not — Redis
    // often has no password, and a wrong one would break the exporter
    (strict ? undefined : usable.find((s) => s.keys.includes("password")))
  );
}
