"use client";

import Link from "next/link";
import { useSearchParams } from "next/navigation";
import { Suspense, useState } from "react";
import { installChart, type InstallResult } from "@/lib/api/catalog";
import { friendlyErrorMessage } from "@/lib/api/errors";
import { useChart } from "@/lib/hooks/useCatalog";
import { useNamespaces } from "@/lib/hooks/useNamespaces";
import { useNodes } from "@/lib/hooks/useWorkloads";
import { applyErrorMessage } from "@/components/editor/useYamlApply";
import { YamlEditor } from "@/components/editor/YamlEditor";
import { PillTabs } from "@/components/ui/Tabs";

const inputClass =
  "rounded-lg border border-border bg-surface-2 px-3 py-1.75 text-xs outline-none";

const valuesHint =
  '# Only what you want to change — everything else keeps the chart\'s default.\n# The "Chart defaults" tab lists every available value.\n';

export default function InstallPage() {
  return (
    <Suspense
      fallback={<div className="p-8 text-sm text-text-muted">Loading…</div>}
    >
      <InstallInner />
    </Suspense>
  );
}

function InstallInner() {
  const params = useSearchParams();
  const repo = params.get("repo") ?? "";
  const chartName = params.get("chart") ?? "";

  const [version, setVersion] = useState("");
  const {
    data: chart,
    error,
    isLoading,
  } = useChart(repo, chartName, version || undefined);
  const { data: namespaces } = useNamespaces();
  const { data: nodes } = useNodes();

  const [release, setRelease] = useState(chartName);
  const [namespace, setNamespace] = useState(chartName);
  const [nodeName, setNodeName] = useState("");
  const [values, setValues] = useState(valuesHint);
  const [valuesTab, setValuesTab] = useState<"mine" | "defaults">("mine");
  const [wait, setWait] = useState(false);
  const [busy, setBusy] = useState(false);
  const [installError, setInstallError] = useState<string | null>(null);
  const [result, setResult] = useState<InstallResult | null>(null);

  const existing = new Set(namespaces?.map((n) => n.name));
  const newNamespace = !!namespaces && !!namespace && !existing.has(namespace);

  async function install() {
    setBusy(true);
    setInstallError(null);
    try {
      setResult(
        await installChart({
          repo,
          chart: chartName,
          version: version || undefined,
          namespace: namespace.trim(),
          release: release.trim(),
          values,
          nodeName: nodeName || undefined,
          createNamespace: newNamespace,
          wait,
        }),
      );
    } catch (err) {
      setInstallError(applyErrorMessage(err));
    } finally {
      setBusy(false);
    }
  }

  if (!repo || !chartName) {
    return (
      <div className="px-8 py-7 text-sm text-text-muted">
        Pick a chart from the{" "}
        <Link href="/marketplace" className="text-accent-text underline">
          catalog
        </Link>
        .
      </div>
    );
  }

  return (
    <div className="flex flex-col gap-[18px] px-8 py-7">
      <div className="flex items-center gap-2 text-xs text-text-muted">
        <Link href="/marketplace" className="hover:text-text">
          Marketplace
        </Link>
        <span>/</span>
        <span className="font-mono text-text">
          {repo}/{chartName}
        </span>
      </div>

      {error ? (
        <div className="text-sm text-err">
          Couldn&apos;t load the chart: {friendlyErrorMessage(error)}
        </div>
      ) : isLoading && !chart ? (
        <div className="text-sm text-text-muted">Downloading the chart…</div>
      ) : chart ? (
        <div className="grid gap-6 lg:grid-cols-[1fr_1.3fr]">
          <div className="flex flex-col gap-4">
            <div>
              <div className="text-[15px] font-semibold">{chart.name}</div>
              <div className="mt-1 text-xs text-text-muted">
                {chart.description}
              </div>
              <div className="mt-1 text-[11px] text-text-faint">
                {chart.version}
                {chart.appVersion ? ` · app ${chart.appVersion}` : ""}
                {chart.hasSchema
                  ? " · values are validated against a schema"
                  : ""}
              </div>
              {chart.home && (
                <a
                  href={chart.home}
                  target="_blank"
                  rel="noreferrer noopener"
                  className="mt-1 inline-block text-[11px] text-accent-text underline"
                >
                  Project home
                </a>
              )}
            </div>

            <label className="flex flex-col gap-1.5 text-xs text-text-muted">
              Chart version
              <select
                aria-label="Chart version"
                value={version}
                onChange={(e) => setVersion(e.target.value)}
                className={inputClass}
              >
                <option value="">Latest ({chart.versions[0]?.version})</option>
                {chart.versions.map((v) => (
                  <option key={v.version} value={v.version}>
                    {v.version}
                    {v.deprecated ? " — deprecated" : ""}
                  </option>
                ))}
              </select>
            </label>

            <label className="flex flex-col gap-1.5 text-xs text-text-muted">
              Release name
              <input
                aria-label="Release name"
                value={release}
                onChange={(e) => setRelease(e.target.value)}
                className={`${inputClass} font-mono`}
              />
            </label>

            <label className="flex flex-col gap-1.5 text-xs text-text-muted">
              Namespace
              <input
                aria-label="Namespace"
                list="marketplace-namespaces"
                value={namespace}
                onChange={(e) => setNamespace(e.target.value)}
                className={`${inputClass} font-mono`}
              />
              <datalist id="marketplace-namespaces">
                {namespaces?.map((n) => (
                  <option key={n.name} value={n.name} />
                ))}
              </datalist>
              {newNamespace && (
                <span className="text-[11px] text-warn">
                  This namespace doesn&apos;t exist and will be created.
                </span>
              )}
            </label>

            {chart.supportsNodeSelector ? (
              <label className="flex flex-col gap-1.5 text-xs text-text-muted">
                Run on node (optional)
                <select
                  aria-label="Run on node"
                  value={nodeName}
                  onChange={(e) => setNodeName(e.target.value)}
                  className={inputClass}
                >
                  <option value="">Any node</option>
                  {nodes?.map((n) => (
                    <option key={n.name} value={n.name}>
                      {n.name}
                    </option>
                  ))}
                </select>
                <span className="text-[11px] text-text-faint">
                  Adds{" "}
                  <span className="font-mono">
                    nodeSelector: kubernetes.io/hostname
                  </span>{" "}
                  to the values.
                </span>
              </label>
            ) : (
              <div className="rounded-lg border border-border bg-surface-2 p-3 text-[11px] text-text-faint">
                This chart has no top-level{" "}
                <span className="font-mono">nodeSelector</span> value, so
                Vinaire can&apos;t pin it to a node for you. To choose a node,
                set the chart&apos;s own scheduling value in the values (see the
                defaults tab).
              </div>
            )}

            <label className="flex items-center gap-2 text-xs text-text-muted">
              <input
                type="checkbox"
                checked={wait}
                onChange={(e) => setWait(e.target.checked)}
              />
              Wait until the resources are ready (up to 5 minutes)
            </label>

            {chart.readme && (
              <details className="rounded-lg border border-border bg-surface p-3 text-xs">
                <summary className="cursor-pointer text-text-muted">
                  README
                </summary>
                <pre className="mt-2 max-h-80 overflow-auto whitespace-pre-wrap font-mono text-[11px] text-text-muted">
                  {chart.readme}
                </pre>
              </details>
            )}
          </div>

          <div className="flex flex-col gap-3">
            <PillTabs
              items={[
                { key: "mine", label: "Your values" },
                { key: "defaults", label: "Chart defaults (read-only)" },
              ]}
              active={valuesTab}
              onChange={setValuesTab}
            />
            <div className="h-[420px] overflow-hidden rounded-lg border border-border">
              {valuesTab === "mine" ? (
                <YamlEditor value={values} onChange={setValues} height="100%" />
              ) : (
                <YamlEditor value={chart.values} readOnly height="100%" />
              )}
            </div>

            {installError && (
              <div className="whitespace-pre-wrap rounded-lg border border-err/30 bg-surface-2 p-3 text-xs text-err">
                {installError}
              </div>
            )}

            {result ? (
              <div className="rounded-lg border border-ok/30 bg-surface-2 p-3 text-xs">
                <div className="text-ok">
                  Installed {result.release.name} ({result.release.chart}-
                  {result.release.chartVersion}) in {result.release.namespace} —{" "}
                  {result.release.status}.
                </div>
                {result.notes && (
                  <pre className="mt-2 max-h-56 overflow-auto whitespace-pre-wrap font-mono text-[11px] text-text-muted">
                    {result.notes}
                  </pre>
                )}
                <Link
                  href="/marketplace"
                  className="mt-2 inline-block text-accent-text underline"
                >
                  Back to the Marketplace
                </Link>
              </div>
            ) : (
              <div className="flex justify-end">
                <button
                  type="button"
                  onClick={() => void install()}
                  disabled={busy || !release.trim() || !namespace.trim()}
                  className="rounded-lg bg-accent px-4 py-2 text-[12.5px] font-semibold text-white disabled:opacity-60"
                >
                  {busy ? "Installing…" : "Install"}
                </button>
              </div>
            )}
          </div>
        </div>
      ) : null}
    </div>
  );
}
