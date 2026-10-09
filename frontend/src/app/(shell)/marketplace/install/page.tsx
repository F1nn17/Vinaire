"use client";

import Link from "next/link";
import { useSearchParams } from "next/navigation";
import { Suspense, useEffect, useMemo, useState } from "react";
import useSWR from "swr";
import {
  cancelInstallJob,
  getInstallJob,
  preflightInstall,
  startInstallJob,
  type InstallRequest,
} from "@/lib/api/catalog";
import { friendlyErrorMessage } from "@/lib/api/errors";
import { t } from "@/lib/i18n/translate";
import { useChart } from "@/lib/hooks/useCatalog";
import { useNamespaces } from "@/lib/hooks/useNamespaces";
import { useNodes } from "@/lib/hooks/useWorkloads";
import { applyErrorMessage } from "@/components/editor/useYamlApply";
import { YamlEditor } from "@/components/editor/YamlEditor";
import { ChartIcon } from "@/components/marketplace/ChartIcon";
import { InstallProgress } from "@/components/marketplace/InstallProgress";
import { PermissionsCheck } from "@/components/marketplace/PermissionsCheck";
import { RollbackNote } from "@/components/marketplace/RollbackNote";
import { RecipePanel } from "@/components/marketplace/RecipePanel";
import { InstallPlan } from "@/components/marketplace/InstallPlan";
import { ExternalDbHint } from "@/components/marketplace/ExternalDbHint";
import { ValuesForm } from "@/components/marketplace/ValuesForm";
import { buildSections } from "@/lib/values/schemaForm";
import { PillTabs } from "@/components/ui/Tabs";

const inputClass =
  "rounded-lg border border-border bg-surface-2 px-3 py-1.75 text-xs outline-none";

const valuesHint =
  '# Only what you want to change — everything else keeps the chart\'s default.\n# The "Chart defaults" tab lists every available value.\n';

function useDebounced<T>(value: T, ms: number): T {
  const [v, setV] = useState(value);
  useEffect(() => {
    const t = setTimeout(() => setV(value), ms);
    return () => clearTimeout(t);
  }, [value, ms]);
  return v;
}

const ended = ["done", "failed", "cancelled"];

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
    mutate: reloadChart,
  } = useChart(repo, chartName, version || undefined);
  const { data: namespaces } = useNamespaces();
  const { data: nodes } = useNodes();

  const [release, setRelease] = useState(chartName);
  const [namespace, setNamespace] = useState(chartName);
  const [nodeName, setNodeName] = useState("");
  const [values, setValues] = useState(valuesHint);
  const [valuesTab, setValuesTab] = useState<"mine" | "defaults" | "form">(
    "mine",
  );
  const [wait, setWait] = useState(false);
  const [allowDeprecated, setAllowDeprecated] = useState(false);
  const [starting, setStarting] = useState(false);
  const [startError, setStartError] = useState<string | null>(null);
  const [jobId, setJobId] = useState<string | null>(null);

  // the form is offered only when the chart ships a schema that yields settings
  const schemaText = chart?.schema;
  const hasForm = useMemo(
    () => !!schemaText && buildSections(schemaText).length > 0,
    [schemaText],
  );

  const existing = new Set(namespaces?.map((n) => n.name));
  const newNamespace = !!namespaces && !!namespace && !existing.has(namespace);

  const request: InstallRequest = {
    repo,
    chart: chartName,
    version: version || undefined,
    namespace: namespace.trim(),
    release: release.trim(),
    values,
    nodeName: nodeName || undefined,
    createNamespace: newNamespace,
    wait,
    allowDeprecated,
  };

  // Ask, as the user, whether the cluster lets them install this — before they
  // press Install, so a missing permission shows up as a list rather than as a
  // failure halfway through. Debounced: the backend renders the chart each time.
  const checked = useDebounced(JSON.stringify(request), 900);
  const formReady = !!chart && !!release.trim() && !!namespace.trim();
  const { data: preflight, isLoading: checking } = useSWR(
    formReady && !jobId ? ["install-preflight", checked] : null,
    () => preflightInstall(JSON.parse(checked) as InstallRequest),
    {
      revalidateOnFocus: false,
      shouldRetryOnError: false,
      keepPreviousData: true,
    },
  );
  const missing = preflight?.missing ?? [];

  // Once started the install runs in the background; poll how far it is.
  const { data: progress, mutate: refreshJob } = useSWR(
    jobId ? ["install-job", jobId] : null,
    () => getInstallJob(jobId as string),
    {
      refreshInterval: (d) => (d && ended.includes(d.job.stage) ? 0 : 700),
      revalidateOnFocus: false,
    },
  );
  const job = progress?.job;

  async function install() {
    setStarting(true);
    setStartError(null);
    try {
      setJobId(await startInstallJob(request));
    } catch (err) {
      setStartError(applyErrorMessage(err));
    } finally {
      setStarting(false);
    }
  }

  async function cancel() {
    if (!jobId) return;
    try {
      await cancelInstallJob(jobId);
      // a cancel mid-install (or a rollback of a failed one) starts more work: look again now,
      // which also restarts the polling that had stopped on "failed"
      await refreshJob();
    } catch (err) {
      setStartError(applyErrorMessage(err));
    }
  }

  function backToForm() {
    setJobId(null);
    setStartError(null);
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
        <div className="flex flex-col items-start gap-3 text-sm">
          <div className="text-err">
            Couldn&apos;t load the chart: {friendlyErrorMessage(error)}
          </div>
          <button
            type="button"
            onClick={() => void reloadChart()}
            className="rounded-lg border border-border px-3 py-1.5 text-xs hover:bg-surface-2"
          >
            Try again
          </button>
        </div>
      ) : isLoading && !chart ? (
        <div className="text-sm text-text-muted">Downloading the chart…</div>
      ) : chart ? (
        <div className="grid gap-6 lg:grid-cols-[1fr_1.3fr]">
          <div className="flex flex-col gap-4">
            <div className="flex items-start gap-3">
              <ChartIcon
                repo={chart.repo}
                chart={chart.name}
                hasIcon={chart.hasIcon}
                size={48}
              />
              <div className="min-w-0">
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
            </div>

            <label className="flex flex-col gap-1.5 text-xs text-text-muted">
              Chart version
              <select
                aria-label="Chart version"
                value={version}
                onChange={(e) => setVersion(e.target.value)}
                disabled={!!jobId}
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
                disabled={!!jobId}
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
                disabled={!!jobId}
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
                  disabled={!!jobId}
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

            {!chart.recipe && !jobId && (
              <ExternalDbHint
                key={`${repo}/${chartName}`}
                defaults={chart.values}
                values={values}
                onValues={setValues}
              />
            )}

            {chart.recipe && !jobId && (
              <RecipePanel
                key={`${repo}/${chartName}`}
                recipe={chart.recipe}
                installNamespace={namespace.trim()}
                release={release.trim()}
                values={values}
                onValues={setValues}
              />
            )}

            <label className="flex items-center gap-2 text-xs text-text-muted">
              <input
                type="checkbox"
                checked={wait}
                onChange={(e) => setWait(e.target.checked)}
                disabled={!!jobId}
              />
              Wait until the resources are ready (up to 5 minutes)
            </label>

            {chart.deprecated && (
              <label className="flex flex-col gap-1.5 rounded-lg border border-warn/40 bg-surface-2 p-3 text-xs text-warn">
                <span className="font-semibold">This chart is deprecated.</span>
                <span className="text-text-muted">
                  Its maintainers no longer update it, so it can carry known
                  vulnerabilities. Prefer a maintained alternative.
                </span>
                <span className="flex items-center gap-2 text-text-muted">
                  <input
                    type="checkbox"
                    checked={allowDeprecated}
                    onChange={(e) => setAllowDeprecated(e.target.checked)}
                    disabled={!!jobId}
                  />
                  Install it anyway
                </span>
              </label>
            )}

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
                ...(hasForm
                  ? [{ key: "form" as const, label: "Settings form" }]
                  : []),
                { key: "mine" as const, label: "Your values (YAML)" },
                {
                  key: "defaults" as const,
                  label: "Chart defaults (read-only)",
                },
              ]}
              active={valuesTab === "form" && !hasForm ? "mine" : valuesTab}
              onChange={setValuesTab}
            />
            {valuesTab === "mine" && !jobId && (
              <div className="flex flex-wrap items-center gap-2 text-xs">
                <button
                  type="button"
                  onClick={() => setValues(chart.values)}
                  className="rounded-lg border border-border px-3 py-1.5 hover:bg-surface-2"
                >
                  Start from the documented defaults
                </button>
                <label className="cursor-pointer rounded-lg border border-border px-3 py-1.5 hover:bg-surface-2">
                  Load a values file…
                  <input
                    type="file"
                    accept=".yaml,.yml,.txt"
                    className="sr-only"
                    onChange={(e) => {
                      const file = e.target.files?.[0];
                      if (!file || file.size > 1024 * 1024) return;
                      void file.text().then(setValues);
                      e.target.value = "";
                    }}
                  />
                </label>
                <span className="text-text-faint">
                  The defaults come with the chart&apos;s own comments — keep
                  what you change, delete the rest.
                </span>
              </div>
            )}
            <div className="h-[420px] overflow-hidden rounded-lg border border-border">
              {valuesTab === "form" && hasForm && chart ? (
                <div className="h-full p-3">
                  <ValuesForm
                    schemaText={chart.schema as string}
                    defaultsText={chart.values}
                    valuesText={values}
                    onChange={setValues}
                  />
                </div>
              ) : valuesTab === "defaults" ? (
                <YamlEditor value={chart!.values} readOnly height="100%" />
              ) : (
                <YamlEditor value={values} onChange={setValues} height="100%" />
              )}
            </div>

            {startError && (
              <div className="whitespace-pre-wrap rounded-lg border border-err/30 bg-surface-2 p-3 text-xs text-err">
                {startError}
              </div>
            )}

            {job ? (
              <>
                <InstallProgress
                  job={job}
                  pods={progress?.pods}
                  onCancel={() => void cancel()}
                />
                {job.stage === "failed" && (
                  <div className="flex flex-col gap-3">
                    {job.missing && job.missing.length > 0 ? (
                      <PermissionsCheck
                        checking={false}
                        missing={job.missing}
                        unchecked={job.unchecked}
                      />
                    ) : (
                      <div className="whitespace-pre-wrap rounded-lg border border-err/30 bg-surface-2 p-3 text-xs text-err">
                        {job.error}
                      </div>
                    )}
                    {/deadline exceeded|timed out/i.test(job.error ?? "") && (
                      <div className="rounded-lg border border-border bg-surface p-3 text-xs text-text-muted">
                        {t(
                          "The install timed out waiting for the pods to become ready. The pod's Events and Logs say why — most often a wrong database user, password or name, or an image that is still downloading.",
                        )}{" "}
                        <Link
                          href={`/workloads?namespace=${encodeURIComponent(job.namespace)}&tab=pods`}
                          className="text-accent-text underline"
                        >
                          {t("Open the pods in {ns}", { ns: job.namespace })}
                        </Link>
                      </div>
                    )}
                    <RollbackNote job={job} />
                    <div className="flex gap-2">
                      {job.canRollBack && (
                        <button
                          type="button"
                          onClick={() => void cancel()}
                          className="rounded-lg border border-err/50 px-3 py-1.5 text-xs text-err hover:border-err"
                        >
                          Roll back what was created
                        </button>
                      )}
                      <button
                        type="button"
                        onClick={backToForm}
                        className="rounded-lg border border-border px-3 py-1.5 text-xs hover:bg-surface-2"
                      >
                        Back to the form
                      </button>
                    </div>
                  </div>
                )}
                {job.stage === "cancelled" && (
                  <div className="flex flex-col gap-3 text-xs text-text-muted">
                    <div>
                      {job.failedStage === "installing"
                        ? "Cancelled while installing."
                        : "Cancelled — nothing was installed."}
                    </div>
                    <RollbackNote job={job} />
                    <div>
                      <button
                        type="button"
                        onClick={backToForm}
                        className="rounded-lg border border-border px-3 py-1.5 hover:bg-surface-2"
                      >
                        Back to the form
                      </button>
                    </div>
                  </div>
                )}
                {job.stage === "done" && job.result && (
                  <div className="rounded-lg border border-ok/30 bg-surface-2 p-3 text-xs">
                    <div className="text-ok">
                      Installed {job.result.release.name} (
                      {job.result.release.chart}-
                      {job.result.release.chartVersion}) in{" "}
                      {job.result.release.namespace} —{" "}
                      {job.result.release.status}.
                    </div>
                    {job.result.notes && (
                      <pre className="mt-2 max-h-56 overflow-auto whitespace-pre-wrap font-mono text-[11px] text-text-muted">
                        {job.result.notes}
                      </pre>
                    )}
                    {chart.recipe?.after && (
                      <div className="mt-2 flex flex-col gap-1.5 text-text-muted">
                        {chart.recipe.after.map((line, i) => (
                          <p key={i}>
                            {t(line)
                              .replaceAll("{release}", job.result!.release.name)
                              .replaceAll(
                                "{namespace}",
                                job.result!.release.namespace,
                              )}
                          </p>
                        ))}
                      </div>
                    )}
                    <Link
                      href="/marketplace"
                      className="mt-2 inline-block text-accent-text underline"
                    >
                      Back to the Marketplace
                    </Link>
                  </div>
                )}
              </>
            ) : (
              <>
                <PermissionsCheck
                  checking={checking}
                  missing={missing}
                  unchecked={preflight?.unchecked}
                />
                <InstallPlan preflight={preflight} request={request} />
                <div className="flex justify-end">
                  <button
                    type="button"
                    onClick={() => void install()}
                    disabled={
                      starting ||
                      (chart.deprecated && !allowDeprecated) ||
                      !release.trim() ||
                      !namespace.trim() ||
                      missing.length > 0
                    }
                    className="rounded-lg bg-accent px-4 py-2 text-[12.5px] font-semibold text-white disabled:opacity-60"
                  >
                    {starting ? "Starting…" : "Install"}
                  </button>
                </div>
              </>
            )}
          </div>
        </div>
      ) : null}
    </div>
  );
}
