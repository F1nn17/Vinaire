"use client";

import Link from "next/link";
import { useEffect, useMemo, useState } from "react";
import useSWR, { useSWRConfig } from "swr";
import { friendlyErrorMessage } from "@/lib/api/errors";
import { useResourceTable } from "@/lib/api/allResources";
import type { DetectedApp } from "@/lib/api/apps";
import { getInstallJob, startInstallJob } from "@/lib/api/catalog";
import { useMetricsStatus } from "@/lib/api/metricsStack";
import { useSecrets } from "@/lib/hooks/useSecrets";
import { t } from "@/lib/i18n/translate";
import {
  exporterCharts,
  exporterKindOf,
  exporterRelease,
  exporterValues,
  guessSecret,
  guessService,
  scrapeFlavorOf,
  validateExporter,
  type ExporterInput,
} from "@/lib/dashboards/exporters";

const input =
  "rounded-lg border border-border bg-surface-2 px-3 py-1.75 font-mono text-xs outline-none placeholder:text-text-faint";

// Roadmap Phase 74b: "PostgreSQL runs here, but nothing reports its metrics" — with the way out on the
// same page. The form is filled in from what is in the cluster (the instance's Service, the Secret
// with its password); one press installs the Marketplace's exporter next to the instance, as the
// signed-in user, through the same audited install as everywhere. The password is never read or
// typed: the exporter is given the Secret's name and key.
export function ExporterInstall({ app }: { app: DetectedApp }) {
  const kind = exporterKindOf(app.id);
  if (!kind) return null;
  return <Form key={app.id} app={app} kind={kind} />;
}

function Form({
  app,
  kind,
}: {
  app: DetectedApp;
  kind: NonNullable<ReturnType<typeof exporterKindOf>>;
}) {
  const [pick, setPick] = useState(0);
  const instance = app.instances[Math.min(pick, app.instances.length - 1)];
  return (
    <div
      className="flex flex-col gap-3 rounded-[10px] border border-warn/40 bg-surface p-4 text-xs"
      data-testid="exporter-install"
    >
      <div className="text-warn">
        {t("{app} runs here, but nothing reports its metrics to Prometheus.", {
          app: app.title,
        })}
      </div>
      {app.instances.length > 1 && (
        <label className="flex items-center gap-2 text-text-muted">
          Instance
          <select
            aria-label="Instance"
            value={pick}
            onChange={(e) => setPick(Number(e.target.value))}
            className={input}
          >
            {app.instances.map((i, n) => (
              <option key={`${i.namespace}/${i.name}`} value={n}>
                {i.name} ({i.namespace})
              </option>
            ))}
          </select>
        </label>
      )}
      {/* a fresh form for each instance: what was guessed for one is wrong for another */}
      <InstanceForm
        key={`${instance.namespace}/${instance.name}`}
        kind={kind}
        instance={instance}
      />
    </div>
  );
}

function InstanceForm({
  kind,
  instance,
}: {
  kind: NonNullable<ReturnType<typeof exporterKindOf>>;
  instance: DetectedApp["instances"][number];
}) {
  const ns = instance.namespace;
  const services = useResourceTable("core/v1/services", ns);
  const secrets = useSecrets(ns);
  const status = useMetricsStatus();
  const { mutate } = useSWRConfig();

  const serviceNames = useMemo(
    () => (services.data?.rows ?? []).map((r) => r.name),
    [services.data],
  );
  const secretInfos = useMemo(
    () =>
      (secrets.data ?? []).map((s) => ({
        name: s.name,
        keys: s.keys.map((k) => k.label),
      })),
    [secrets.data],
  );
  const guessedSecret = guessSecret(
    instance.name,
    secretInfos,
    kind === "redis",
  );
  // a Secret that Vinaire's wizard made carries the user's name beside the password
  const wizardMade = !!guessedSecret?.keys.includes("username");

  const defaults =
    kind === "postgresql"
      ? {
          user: wizardMade ? "app" : "postgres",
          database: wizardMade ? "app" : "postgres",
        }
      : { user: "", database: "" };
  // what the person typed over the guesses; the guesses themselves are derived, so they follow the
  // lists as they arrive
  const [typed, setTyped] = useState<Partial<ExporterInput>>({});
  const form: ExporterInput = {
    kind,
    instance,
    service: typed.service ?? guessService(instance.name, serviceNames),
    port: typed.port ?? (kind === "postgresql" ? "5432" : "6379"),
    user: typed.user ?? defaults.user,
    database: typed.database ?? defaults.database,
    secret: typed.secret ?? guessedSecret?.name ?? "",
    secretKey:
      typed.secretKey ??
      (secretInfos
        .find((s) => s.name === (typed.secret ?? guessedSecret?.name))
        ?.keys.includes("password")
        ? "password"
        : (secretInfos.find(
            (s) => s.name === (typed.secret ?? guessedSecret?.name),
          )?.keys[0] ?? "password")),
    redisUser: typed.redisUser ?? (kind === "redis" && wizardMade ? "app" : ""),
  };
  const set = (patch: Partial<ExporterInput>) =>
    setTyped((x) => ({ ...x, ...patch }));
  // Redis often has no password at all: with no Secret named after it, assume none (a guess the
  // person can undo with the box below)
  const [noPass, setNoPass] = useState<boolean | undefined>(undefined);
  const noPassword = kind === "redis" && (noPass ?? !guessedSecret);
  const effective: ExporterInput = noPassword
    ? { ...form, secret: "", secretKey: "" }
    : form;
  const problems = validateExporter(effective);
  // the guesses come from the Services and Secrets of the namespace: wait for them
  const looking = services.isLoading || secrets.isLoading;
  const valid =
    Object.keys(problems).length === 0 && !!serviceNames.length && !looking;

  const flavor = scrapeFlavorOf(status.data);
  const chart = exporterCharts[kind];
  const release = exporterRelease(kind, instance.name);

  const [jobId, setJobId] = useState<string | null>(null);
  const [starting, setStarting] = useState(false);
  const [error, setError] = useState<string | null>(null);

  async function install() {
    setStarting(true);
    setError(null);
    try {
      setJobId(
        await startInstallJob({
          repo: chart.repo,
          chart: chart.chart,
          namespace: ns,
          release,
          values: exporterValues(effective, flavor),
        }),
      );
    } catch (e) {
      setError(friendlyErrorMessage(e));
    } finally {
      setStarting(false);
    }
  }

  const job = useSWR(
    jobId ? ["exporter-job", jobId] : null,
    () => getInstallJob(jobId as string),
    {
      refreshInterval: (d) =>
        d && ["done", "failed", "cancelled"].includes(d.job.stage) ? 0 : 1000,
      revalidateOnFocus: false,
    },
  );
  const stage = job.data?.job.stage;

  // once it is installed, look for the metrics every 15 s (Prometheus reads a new target within a
  // minute) until the dashboard's own check finds them and this card goes away
  useEffect(() => {
    if (stage !== "done") return;
    const id = setInterval(
      () =>
        void mutate(
          (k) =>
            Array.isArray(k) &&
            (k[0] === "dash-req" || k[0] === "dash-availability"),
        ),
      15_000,
    );
    return () => clearInterval(id);
  }, [stage, mutate]);

  if (stage === "done") {
    return (
      <div className="text-ok">
        {t(
          "The exporter {release} is installed in {ns}. Prometheus reads a new target within a minute — this page fills in by itself.",
          { release, ns },
        )}{" "}
        <Link href="/marketplace" className="text-accent-text underline">
          Installed releases →
        </Link>
      </div>
    );
  }
  if (jobId && stage && stage !== "failed" && stage !== "cancelled") {
    return (
      <div className="text-text-muted">
        {t("Installing the exporter {release}…", { release })} ({stage})
      </div>
    );
  }

  return (
    <div className="flex flex-col gap-3">
      <div className="text-text-muted">
        {t(
          "Vinaire can install the Marketplace's {chart} next to it, in {ns}, pointed at this instance. Check what was filled in.",
          { chart: chart.chart, ns },
        )}
      </div>
      {looking && (
        <div className="text-text-faint">
          {t("Looking at the instance's Services and Secrets…")}
        </div>
      )}
      <div className="grid gap-3 md:grid-cols-3">
        <Field label="Service" error={problems.service}>
          <select
            aria-label="Service"
            value={form.service}
            onChange={(e) => set({ service: e.target.value })}
            className={input}
          >
            {serviceNames.map((n) => (
              <option key={n} value={n}>
                {n}
              </option>
            ))}
          </select>
        </Field>
        <Field label="Port" error={problems.port}>
          <input
            aria-label="Port"
            value={form.port}
            onChange={(e) => set({ port: e.target.value.trim() })}
            className={input}
          />
        </Field>
        {kind === "postgresql" && (
          <>
            <Field label="User" error={problems.user}>
              <input
                aria-label="User"
                value={form.user}
                onChange={(e) => set({ user: e.target.value.trim() })}
                className={input}
              />
            </Field>
            <Field label="Database" error={problems.database}>
              <input
                aria-label="Database"
                value={form.database}
                onChange={(e) => set({ database: e.target.value.trim() })}
                className={input}
              />
            </Field>
          </>
        )}
        {kind === "redis" && (
          <Field
            label="ACL user (empty: the default user)"
            error={problems.redisUser}
          >
            <input
              aria-label="ACL user"
              value={form.redisUser}
              onChange={(e) => set({ redisUser: e.target.value.trim() })}
              className={input}
            />
          </Field>
        )}
        {!noPassword && (
          <>
            <Field label="Secret with the password" error={problems.secret}>
              <select
                aria-label="Secret with the password"
                value={form.secret}
                onChange={(e) =>
                  set({ secret: e.target.value, secretKey: undefined })
                }
                className={input}
              >
                {form.secret === "" && <option value="">—</option>}
                {secretInfos
                  .filter((s) => !s.name.startsWith("sh.helm.release"))
                  .map((s) => (
                    <option key={s.name} value={s.name}>
                      {s.name}
                    </option>
                  ))}
              </select>
            </Field>
            <Field label="Key in that Secret" error={problems.secretKey}>
              <select
                aria-label="Key in the Secret"
                value={form.secretKey}
                onChange={(e) => set({ secretKey: e.target.value })}
                className={input}
              >
                {(
                  secretInfos.find((s) => s.name === form.secret)?.keys ?? [
                    form.secretKey,
                  ]
                ).map((k) => (
                  <option key={k} value={k}>
                    {k}
                  </option>
                ))}
              </select>
            </Field>
          </>
        )}
      </div>
      {kind === "redis" && (
        <label className="flex items-center gap-2 text-text-muted">
          <input
            type="checkbox"
            checked={noPassword}
            onChange={(e) => setNoPass(e.target.checked)}
          />
          This Redis has no password
        </label>
      )}
      {kind === "postgresql" && (
        <div className="text-[11px] text-text-faint">
          {wizardMade
            ? t(
                "This looks like an instance made with “Install from an image”: user and database are app unless you chose others there.",
              )
            : t(
                "The exporter signs in with this user; a read-only monitoring user is enough.",
              )}
        </div>
      )}
      <div className="text-[11px] text-text-faint">
        {flavor.kind === "annotations"
          ? t(
              "Prometheus finds it through annotations on the exporter's Service.",
            )
          : t(
              "Prometheus finds it through a ServiceMonitor labelled release={release}.",
              { release: flavor.release },
            )}{" "}
        {t(
          "The password stays in its Secret: the exporter is only given the Secret's name and key.",
        )}
      </div>
      {serviceNames.length === 0 && !services.isLoading && (
        <div className="text-err">{t("No Service found in {ns}.", { ns })}</div>
      )}
      {error && <div className="text-err">{error}</div>}
      {stage === "failed" && (
        <div className="whitespace-pre-wrap text-err">
          {job.data?.job.error}
        </div>
      )}
      <div>
        <button
          type="button"
          disabled={!valid || starting}
          onClick={() => void install()}
          className="rounded-lg bg-accent px-4 py-2 text-[12.5px] font-semibold text-white disabled:opacity-50"
        >
          {starting ? "Starting…" : "Install the exporter"}
        </button>
      </div>
    </div>
  );
}

function Field({
  label,
  error,
  children,
}: {
  label: string;
  error?: string;
  children: React.ReactNode;
}) {
  return (
    <label className="flex flex-col gap-1 text-text-muted">
      {label}
      {children}
      {error && <span className="text-[11px] text-err">{t(error)}</span>}
    </label>
  );
}
