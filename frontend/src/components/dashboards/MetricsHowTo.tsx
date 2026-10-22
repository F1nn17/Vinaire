"use client";

import Link from "next/link";
import type { DetectedApp } from "@/lib/api/apps";
import { t } from "@/lib/i18n/translate";
import { CodeBlock } from "@/components/ui/CodeBlock";

// Roadmap Phase 74c: MinIO and Keycloak serve their own metrics — there is no exporter to install.
// What is missing is switching the metrics on in the running application and telling Prometheus where
// they are. Vinaire doesn't change an application that already runs (that is the owner's decision,
// not a default), so this says exactly what to run, with the names filled in from what was found.

const k8sKind = (kind: string) =>
  kind === "StatefulSet"
    ? "statefulset"
    : kind === "Deployment"
      ? "deployment"
      : "";

export function MetricsHowTo({ app }: { app: DetectedApp }) {
  if (app.id !== "minio" && app.id !== "keycloak") return null;
  const i = app.instances[0];
  const kind = k8sKind(i.kind);
  const target = kind ? `${kind}/${i.name}` : `<the workload of ${i.name}>`;
  const common = `-n ${i.namespace}`;
  const commands =
    app.id === "minio"
      ? [
          "# 1. let Prometheus read MinIO's cluster metrics without a token",
          `kubectl ${common} set env ${target} MINIO_PROMETHEUS_AUTH_TYPE=public`,
          "",
          "# 2. tell Prometheus where they are (the Service of the instance)",
          `kubectl ${common} annotate service ${i.name} --overwrite \\`,
          "  prometheus.io/scrape=true prometheus.io/port=9000 \\",
          "  prometheus.io/path=/minio/v2/metrics/cluster",
        ]
      : [
          "# 1. switch Keycloak's metrics on (they appear on its management port, 9000)",
          `kubectl ${common} set env ${target} KC_METRICS_ENABLED=true`,
          "",
          "# 2. tell Prometheus where they are (the Service of the instance)",
          `kubectl ${common} annotate service ${i.name} --overwrite \\`,
          "  prometheus.io/scrape=true prometheus.io/port=9000 \\",
          "  prometheus.io/path=/metrics",
        ];
  return (
    <div
      className="flex flex-col gap-3 rounded-[10px] border border-warn/40 bg-surface p-4 text-xs"
      data-testid="metrics-howto"
    >
      <div className="text-warn">
        {t("{app} runs here, but nothing reports its metrics to Prometheus.", {
          app: app.title,
        })}
      </div>
      <div className="text-text-muted">
        {app.id === "minio"
          ? t(
              "MinIO serves Prometheus metrics itself, so there is nothing to install — it only has to be told to, and Prometheus told where to look.",
            )
          : t(
              "Keycloak serves Prometheus metrics itself, so there is nothing to install — it only has to be told to, and Prometheus told where to look.",
            )}{" "}
        {t(
          "Vinaire does not change an application that is already running; these are the two changes to make yourself (for the instance found: {name} in {ns}):",
          { name: i.name, ns: i.namespace },
        )}
      </div>
      <CodeBlock code={commands.join("\n")} />
      <div className="text-[11px] text-text-faint">
        {kind
          ? t("The first command restarts the pods; their data is not touched.")
          : t(
              "This instance is run by an operator or a chart: set the same variable where its settings live (the chart's values, the object store's spec) instead of on the workload, or the next sync will undo it.",
            )}{" "}
        {t(
          "The annotations work with the plain Prometheus chart (the Metrics stack page's standard profile). With kube-prometheus-stack use a ServiceMonitor for the same address and path.",
        )}{" "}
        <Link href="/metrics-stack" className="text-accent-text underline">
          Metrics stack →
        </Link>
      </div>
    </div>
  );
}
