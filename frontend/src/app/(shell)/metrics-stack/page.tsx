"use client";

import Link from "next/link";
import { useRef, useState } from "react";
import useSWR from "swr";
import { apiFetch } from "@/lib/api/http";
import { friendlyErrorMessage } from "@/lib/api/errors";
import { tm } from "@/lib/i18n/messages";
import {
  cancelInstallJob,
  getReleaseValues,
  upgradeRelease,
  getInstallJob,
  startInstallJob,
  uninstallRelease,
  type InstallPod,
  type InstallJob,
} from "@/lib/api/catalog";
import {
  applySettings,
  readSettings,
  validSelector,
  volumeChangeError,
  type StackProfile,
  type StackSettings,
} from "@/lib/metricsSettings";
import {
  useMetricsPlan,
  useMetricsStatus,
  type MetricsOptions,
  type MetricsProfile,
  type PlanStep,
} from "@/lib/api/metricsStack";
import { InstallProgress } from "@/components/marketplace/InstallProgress";
import { PermissionsCheck } from "@/components/marketplace/PermissionsCheck";
import { ListSkeleton } from "@/components/ui/Skeleton";
import { TypeToConfirmModal } from "@/components/ui/TypeToConfirmModal";

const input =
  "rounded-lg border border-border bg-surface px-3 py-1.75 font-mono text-xs outline-none placeholder:text-text-faint";

const stateText: Record<string, { label: string; tone: string; help: string }> =
  {
    none: {
      label: "Not installed",
      tone: "text-warn",
      help: "There is no metrics system in this cluster, so the CPU / memory / network charts have nothing to draw. One click installs Prometheus.",
    },
    "metrics-server-only": {
      label: "metrics-server only",
      tone: "text-warn",
      help: "metrics-server gives the current CPU and memory values, but there is no history and no network or disk charts. Install Prometheus for those.",
    },
    partial: {
      label: "Prometheus found, exporters missing",
      tone: "text-warn",
      help: "A Prometheus is running, but an exporter the charts need is missing. Install it from the Marketplace (kube-state-metrics, node-exporter).",
    },
    "prometheus-ready": {
      label: "Ready",
      tone: "text-ok",
      help: "Prometheus with its exporters is running. Open a pod, deployment or node → Metrics.",
    },
    configured: {
      label: "Using PROMETHEUS_URL",
      tone: "text-ok",
      help: "This Vinaire was configured with a Prometheus address by its operator, and uses that.",
    },
  };

export default function MetricsStackPage() {
  const [installing, setInstalling] = useState(false);
  // Once an install has started the installer stays on screen: the moment Prometheus's
  // Service exists the cluster status stops being "none", which must not tear the
  // running install (and its progress) out from under the user.
  const [session, setSession] = useState(false);
  const {
    data: status,
    error,
    isLoading,
    mutate,
  } = useMetricsStatus(installing);
  const [removing, setRemoving] = useState<string[] | null>(null);
  const [removeError, setRemoveError] = useState<string | null>(null);
  const [removeBusy, setRemoveBusy] = useState(false);

  async function remove(releases: string[]) {
    if (!status?.prometheus) return;
    setRemoveBusy(true);
    setRemoveError(null);
    try {
      for (const r of releases) {
        await uninstallRelease(status.prometheus.namespace, r).catch((e) => {
          // a release that's already gone isn't a failure
          if (!/not found/i.test(String(e?.message))) throw e;
        });
      }
      setRemoving(null);
      await mutate();
    } catch (err) {
      setRemoveError(friendlyErrorMessage(err));
    } finally {
      setRemoveBusy(false);
    }
  }

  return (
    <div className="flex flex-col gap-[18px] px-8 py-7">
      <div>
        <div className="text-[15px] font-semibold">Metrics stack</div>
        <div className="mt-1 max-w-3xl text-xs text-text-muted">
          Current CPU and memory values come from metrics-server (built into
          k3s). <b>History and charts</b> need Prometheus, which scrapes the
          nodes and pods and keeps the numbers. Installing runs as <i>you</i> —
          the same Helm install as the Marketplace, so your permissions decide
          and you see what&apos;s missing before it starts.
        </div>
      </div>

      {error ? (
        <div className="text-sm text-err">
          Failed to check the cluster: {friendlyErrorMessage(error)}
        </div>
      ) : isLoading || !status ? (
        <ListSkeleton rows={3} />
      ) : (
        <>
          <div className="max-w-3xl rounded-[10px] border border-border bg-surface p-5">
            <div className="flex items-center justify-between gap-3">
              <div
                className={`text-[13px] font-semibold ${stateText[status.state].tone}`}
              >
                {stateText[status.state].label}
              </div>
              {(status.state === "prometheus-ready" ||
                status.state === "partial") && (
                <span
                  className={`text-xs ${status.flowing ? "text-ok" : "text-warn"}`}
                >
                  {status.flowing
                    ? "● metrics are arriving"
                    : "○ no metrics yet"}
                </span>
              )}
            </div>
            <div className="mt-1.5 text-xs text-text-muted">
              {stateText[status.state].help}
            </div>
            {status.prometheus && (
              <div className="mt-3 flex flex-col gap-1 font-mono text-[11px] text-text-muted">
                <div>
                  Prometheus: {status.prometheus.namespace}/
                  {status.prometheus.service}:{status.prometheus.port}
                </div>
                <div>
                  metrics-server: {status.metricsServer ? "yes" : "no"} ·
                  kube-state-metrics: {status.kubeStateMetrics ? "yes" : "no"} ·
                  node-exporter: {status.nodeExporter ? "yes" : "no"}
                </div>
              </div>
            )}
            {status.state === "none" && status.operatorFound && (
              <div className="mt-2 text-xs text-warn">
                prometheus-operator&apos;s CRDs are installed here — a
                Prometheus may be managed by the operator without being found by
                name.
              </div>
            )}
            {status.detail && (
              <div className="mt-2 text-xs text-warn">{tm(status.detail)}</div>
            )}
            {status.managedByVinaire && (
              <div className="mt-4">
                <button
                  type="button"
                  onClick={() => {
                    setRemoveError(null);
                    setRemoving([status.release ?? "prometheus"]);
                  }}
                  className="rounded-lg border border-border px-3 py-1.5 text-xs text-err hover:border-err"
                >
                  Remove the metrics stack
                </button>
                <span className="ml-3 text-[11px] text-text-faint">
                  Uninstalls Prometheus and its exporters; the stored metrics go
                  with its volume.
                </span>
              </div>
            )}
          </div>

          {status.managedByVinaire &&
            status.prometheus &&
            status.release &&
            status.profile && (
              <StackSettingsPanel
                namespace={status.prometheus.namespace}
                release={status.release}
                profile={status.profile}
                hasAlertmanager={!!status.alertmanager}
                volume={status.volume}
                onSaved={() => void mutate()}
              />
            )}

          {(status.state === "none" ||
            status.state === "metrics-server-only" ||
            session) && (
            <Installer
              metricsServer={status.metricsServer}
              onRunning={(v) => {
                if (v) setSession(true);
                setInstalling(v);
              }}
              onFinished={() => void mutate()}
            />
          )}
        </>
      )}

      {removing && status?.prometheus && (
        <TypeToConfirmModal
          title="Remove the metrics stack?"
          message={`This uninstalls Prometheus, kube-state-metrics and node-exporter from "${status.prometheus.namespace}". Stored metrics are deleted with the volume, and the charts stop working until you install it again.`}
          expected="remove metrics"
          confirmLabel="Remove"
          busy={removeBusy}
          error={removeError}
          onConfirm={() => void remove(removing)}
          onCancel={() => setRemoving(null)}
        />
      )}
    </div>
  );
}

// ---- the install ----------------------------------------------------------------

type Run = { step: PlanStep; jobId: string };

const profiles: {
  id: MetricsProfile;
  title: string;
  help: string;
  needs: string;
}[] = [
  {
    id: "standard",
    title: "Standard",
    help: "Prometheus with kube-state-metrics and node-exporter: current values, history, and the network and disk charts. The sensible default.",
    needs: "about 0.2 CPU and 0.5–1.5 GiB of memory, a 10 Gi volume",
  },
  {
    id: "full",
    title: "Full",
    help: "kube-prometheus-stack: the operator, Prometheus, Alertmanager, the exporters and the default alert rules. For clusters that already work with Prometheus rules.",
    needs:
      "about 0.5 CPU and 1–3 GiB of memory, a volume, and cluster-wide CRDs",
  },
  {
    id: "lite",
    title: "Lite",
    help: "Only metrics-server: the current CPU and memory of nodes and pods. No history, no network or disk charts.",
    needs: "about 0.05 CPU and 64–200 MiB of memory, no volume",
  },
];

function Installer({
  metricsServer,
  onRunning,
  onFinished,
}: {
  metricsServer: boolean;
  onRunning: (v: boolean) => void;
  onFinished: () => void;
}) {
  const [opts, setOpts] = useState<MetricsOptions>({
    profile: "standard",
    nodeSelector: "",
    alertmanager: false,
    namespace: "monitoring",
    retentionDays: 7,
    storageSize: "10Gi",
    storageClass: "",
  });
  const [noVolume, setNoVolume] = useState(false);
  const effective: MetricsOptions = {
    ...opts,
    storageSize: noVolume ? "none" : opts.storageSize,
  };
  const {
    data: plan,
    error: planError,
    isLoading,
  } = useMetricsPlan(effective, true);

  const [run, setRun] = useState<Run | null>(null);
  const [doneSteps, setDoneSteps] = useState<string[]>([]);
  const [startError, setStartError] = useState<string | null>(null);
  const [failed, setFailed] = useState<InstallJob | null>(null);
  const [finished, setFinished] = useState(false);

  async function begin(step: PlanStep) {
    setStartError(null);
    setFailed(null);
    try {
      const id = await startInstallJob({
        repo: step.repo,
        chart: step.chart,
        version: step.version,
        namespace: step.namespace,
        release: step.release,
        values: step.values,
        createNamespace: step.createNamespace,
        wait: step.wait,
      });
      setRun({ step, jobId: id });
      onRunning(true);
    } catch (err) {
      setStartError(friendlyErrorMessage(err));
      onRunning(false);
    }
  }

  // Advance when a step ends. Done in the poll's callback (not an effect): it runs
  // when new data arrives, once per job.
  const handled = useRef("");
  function onPoll(d: { job: InstallJob }) {
    if (!run || !plan || handled.current === run.jobId) return;
    const stage = d.job.stage;
    if (stage === "done") {
      handled.current = run.jobId;
      const next =
        plan.steps[
          plan.steps.findIndex((s) => s.release === run.step.release) + 1
        ];
      setDoneSteps((list) =>
        list.includes(run.step.release) ? list : [...list, run.step.release],
      );
      if (next) {
        void begin(next);
      } else {
        setRun(null);
        setFinished(true);
        onRunning(false);
        onFinished();
      }
    } else if (stage === "failed" || stage === "cancelled") {
      handled.current = run.jobId;
      setFailed(d.job);
      setRun(null);
      onRunning(false);
    }
  }

  const { data: progress } = useSWR(
    run ? ["metrics-job", run.jobId] : null,
    () => getInstallJob(run!.jobId),
    {
      refreshInterval: (d) =>
        d && ["done", "failed", "cancelled"].includes(d.job.stage) ? 0 : 700,
      revalidateOnFocus: false,
      onSuccess: onPoll,
    },
  );
  const job = progress?.job;

  if (finished) {
    return (
      <div className="max-w-3xl rounded-[10px] border border-ok/40 bg-surface p-5 text-sm">
        <div className="font-semibold text-ok">Installed.</div>
        <div className="mt-1 text-xs text-text-muted">
          Prometheus, kube-state-metrics and node-exporter are running. Charts
          appear after the first scrape — a minute or two. Then open a pod,
          deployment or node →{" "}
          <Link href="/workloads" className="text-accent-text underline">
            Metrics
          </Link>
          .
        </div>
      </div>
    );
  }

  if (run) {
    return (
      <div className="flex max-w-3xl flex-col gap-3">
        <div className="text-xs text-text-muted">
          Step {doneSteps.length + 1} of {plan?.steps.length ?? 2}:{" "}
          <b>{run.step.title}</b>
        </div>
        {job ? (
          <InstallProgress
            job={job}
            pods={progress?.pods as InstallPod[] | undefined}
            onCancel={() => void cancelInstallJob(run.jobId)}
          />
        ) : (
          <div className="text-xs text-text-faint">Starting…</div>
        )}
      </div>
    );
  }

  return (
    <div className="flex max-w-3xl flex-col gap-4 rounded-[10px] border border-border bg-surface p-5">
      <div className="text-[13px] font-semibold">Install the metrics stack</div>
      <div
        className="grid grid-cols-3 gap-3"
        role="radiogroup"
        aria-label="Profile"
      >
        {profiles
          .filter((p) => p.id !== "lite" || !metricsServer)
          .map((p) => (
            <button
              key={p.id}
              type="button"
              role="radio"
              aria-checked={opts.profile === p.id}
              onClick={() =>
                setOpts({
                  ...opts,
                  profile: p.id,
                  namespace: p.id === "lite" ? "kube-system" : "monitoring",
                })
              }
              className={`flex flex-col gap-1 rounded-lg border p-3 text-left ${
                opts.profile === p.id
                  ? "border-accent bg-surface-2"
                  : "border-border hover:border-text-faint"
              }`}
            >
              <span className="text-[12.5px] font-semibold">{p.title}</span>
              <span className="text-[11px] text-text-muted">{p.help}</span>
              <span className="text-[11px] text-text-faint">
                Needs {p.needs}
              </span>
            </button>
          ))}
      </div>
      <div className="grid grid-cols-2 gap-3">
        <label className="flex flex-col gap-1.5 text-xs text-text-muted">
          Namespace
          <input
            aria-label="Namespace"
            value={opts.namespace}
            onChange={(e) => setOpts({ ...opts, namespace: e.target.value })}
            className={input}
          />
        </label>
        {opts.profile !== "lite" && (
          <label className="flex flex-col gap-1.5 text-xs text-text-muted">
            Keep metrics for (days)
            <input
              aria-label="Retention days"
              type="number"
              min={1}
              max={365}
              value={opts.retentionDays}
              onChange={(e) =>
                setOpts({ ...opts, retentionDays: Number(e.target.value) || 7 })
              }
              className={input}
            />
          </label>
        )}
        <label className="flex flex-col gap-1.5 text-xs text-text-muted">
          Run only on nodes labelled (key=value, optional)
          <input
            aria-label="Node selector"
            placeholder="disktype=ssd"
            value={opts.nodeSelector}
            onChange={(e) => setOpts({ ...opts, nodeSelector: e.target.value })}
            className={input}
          />
        </label>
        {opts.profile !== "lite" && (
          <label className="flex flex-col gap-1.5 text-xs text-text-muted">
            Volume size
            <input
              aria-label="Volume size"
              value={opts.storageSize}
              disabled={noVolume}
              onChange={(e) =>
                setOpts({ ...opts, storageSize: e.target.value })
              }
              className={`${input} disabled:opacity-50`}
            />
          </label>
        )}
        {opts.profile !== "lite" && (
          <label className="flex flex-col gap-1.5 text-xs text-text-muted">
            Storage class (empty = the cluster default)
            <input
              aria-label="Storage class"
              value={opts.storageClass}
              disabled={noVolume}
              onChange={(e) =>
                setOpts({ ...opts, storageClass: e.target.value })
              }
              className={`${input} disabled:opacity-50`}
            />
          </label>
        )}
      </div>
      {opts.profile === "standard" && (
        <label className="flex items-center gap-2 text-xs text-text-muted">
          <input
            type="checkbox"
            checked={opts.alertmanager}
            onChange={(e) =>
              setOpts({ ...opts, alertmanager: e.target.checked })
            }
          />
          Also install Alertmanager — its alerts feed the bell in the top bar
        </label>
      )}
      {opts.profile !== "lite" && (
        <label className="flex items-center gap-2 text-xs text-text-muted">
          <input
            type="checkbox"
            checked={noVolume}
            onChange={(e) => setNoVolume(e.target.checked)}
          />
          No volume — metrics history is lost whenever Prometheus&apos;s pod
          restarts
        </label>
      )}

      {planError ? (
        <div className="text-xs text-err">
          {friendlyErrorMessage(planError)}
        </div>
      ) : isLoading && !plan ? (
        <div className="text-xs text-text-faint">Checking the cluster…</div>
      ) : plan ? (
        <>
          <ul className="list-disc pl-5 text-xs text-text-muted">
            {plan.steps.map((s) => (
              <li key={s.release}>
                {tm(s.title)}{" "}
                <span className="font-mono text-text-faint">
                  ({s.chart} {s.version})
                </span>
              </li>
            ))}
          </ul>
          {plan.warnings.map((w) => (
            <div
              key={w}
              className="rounded-lg border border-warn/40 bg-surface-2 p-3 text-xs text-warn"
            >
              {tm(w)}
            </div>
          ))}
          {plan.notes.map((n) => (
            <div key={n} className="text-[11px] text-text-faint">
              {tm(n)}
            </div>
          ))}
        </>
      ) : null}

      {startError && <div className="text-xs text-err">{startError}</div>}
      {failed && (
        <div className="flex flex-col gap-2">
          {failed.missing && failed.missing.length > 0 ? (
            <PermissionsCheck
              checking={false}
              missing={failed.missing}
              unchecked={failed.unchecked}
            />
          ) : (
            <div className="whitespace-pre-wrap rounded-lg border border-err/30 bg-surface-2 p-3 text-xs text-err">
              {failed.stage === "cancelled" ? "Cancelled." : failed.error}
            </div>
          )}
        </div>
      )}
      <div className="flex justify-end">
        <button
          type="button"
          disabled={
            !plan ||
            !validSelector(opts.nodeSelector) ||
            plan.warnings.some(
              (w) =>
                w.includes("already running") || w.includes("don't install it"),
            )
          }
          onClick={() => plan && void begin(plan.steps[0])}
          className="rounded-lg bg-accent px-4 py-2 text-[12.5px] font-semibold text-white disabled:opacity-50"
        >
          {failed ? "Try again" : "Install"}
        </button>
      </div>
    </div>
  );
}

// ---- settings of an installed stack ----------------------------------------------

// A Helm upgrade of the release Vinaire installed, with its own values edited in
// place — so what the user changed by hand and the volume are kept.
function StackSettingsPanel({
  namespace,
  release,
  profile,
  hasAlertmanager,
  volume,
  onSaved,
}: {
  namespace: string;
  release: string;
  profile: StackProfile;
  hasAlertmanager: boolean;
  volume?: {
    name: string;
    size: string;
    storageClass?: string;
    expandable: boolean;
  };
  onSaved: () => void;
}) {
  const [open, setOpen] = useState(false);
  const [values, setValues] = useState<string | null>(null);
  const [form, setForm] = useState<StackSettings | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [done, setDone] = useState(false);

  async function begin() {
    setOpen(true);
    setError(null);
    setDone(false);
    try {
      const v = await getReleaseValues(namespace, release);
      setValues(v);
      setForm(readSettings(v, profile));
    } catch (err) {
      setError(friendlyErrorMessage(err));
    }
  }

  async function save() {
    if (!form || values === null) return;
    setBusy(true);
    setError(null);
    try {
      const sizeProblem = volume
        ? volumeChangeError(volume.size, form.volumeSize)
        : null;
      if (sizeProblem) throw new Error(sizeProblem);
      const next = applySettings(values, profile, form);
      // the chart and its pinned version come from the plan, not from the browser
      const q = new URLSearchParams({ profile, namespace });
      const plan = await apiFetch<{ steps: PlanStep[] }>(
        `/api/v1/metrics/plan?${q.toString()}`,
      );
      const step = plan.steps[0];
      await upgradeRelease(namespace, release, {
        repo: step.repo,
        chart: step.chart,
        version: step.version,
        values: next,
        wait: true,
      });
      setDone(true);
      setOpen(false);
      onSaved();
    } catch (err) {
      setError(err instanceof Error ? err.message : friendlyErrorMessage(err));
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="max-w-3xl rounded-[10px] border border-border bg-surface p-5">
      <div className="flex items-center justify-between">
        <div className="text-[13px] font-semibold">Settings</div>
        {!open && (
          <button
            type="button"
            onClick={() => void begin()}
            className="rounded-lg border border-border px-3 py-1.5 text-xs hover:border-accent"
          >
            Change…
          </button>
        )}
      </div>
      {done && (
        <div className="mt-2 text-xs text-ok">
          Saved — the stack is being updated.
        </div>
      )}
      {open && (
        <div className="mt-3 flex flex-col gap-3">
          {form ? (
            <>
              <div className="grid grid-cols-2 gap-3">
                <label className="flex flex-col gap-1.5 text-xs text-text-muted">
                  Keep metrics for (days)
                  <input
                    aria-label="Retention days"
                    type="number"
                    min={1}
                    max={365}
                    value={form.retentionDays}
                    onChange={(e) =>
                      setForm({
                        ...form,
                        retentionDays: Number(e.target.value) || 7,
                      })
                    }
                    className={input}
                  />
                </label>
                <label className="flex flex-col gap-1.5 text-xs text-text-muted">
                  Run only on nodes labelled (key=value, optional)
                  <input
                    aria-label="Node selector"
                    value={form.nodeSelector}
                    onChange={(e) =>
                      setForm({ ...form, nodeSelector: e.target.value })
                    }
                    className={input}
                  />
                </label>
              </div>
              {volume && form.volumeSize !== "" && (
                <div className="flex flex-col gap-1.5">
                  <label className="flex flex-col gap-1.5 text-xs text-text-muted">
                    Volume size (now {volume.size}
                    {volume.storageClass ? `, ${volume.storageClass}` : ""}) —
                    it can only grow
                    <input
                      aria-label="Volume size"
                      value={form.volumeSize}
                      disabled={!volume.expandable}
                      onChange={(e) =>
                        setForm({ ...form, volumeSize: e.target.value.trim() })
                      }
                      className={`${input} disabled:opacity-50`}
                    />
                  </label>
                  {!volume.expandable && (
                    <div className="text-[11px] text-warn">
                      This volume&apos;s storage class doesn&apos;t allow
                      growing a claim in place, so the size can&apos;t be
                      changed here.
                    </div>
                  )}
                </div>
              )}
              {profile === "standard" && (
                <label className="flex items-center gap-2 text-xs text-text-muted">
                  <input
                    type="checkbox"
                    checked={form.alertmanager}
                    onChange={(e) =>
                      setForm({ ...form, alertmanager: e.target.checked })
                    }
                  />
                  Alertmanager{hasAlertmanager ? "" : " (not installed yet)"}
                </label>
              )}
              <div className="text-[11px] text-text-faint">
                This upgrades the Helm release with your own settings kept; the
                volume isn&apos;t touched. It runs as you.
              </div>
            </>
          ) : (
            !error && <div className="text-xs text-text-faint">Loading…</div>
          )}
          {error && <div className="text-xs text-err">{error}</div>}
          <div className="flex justify-end gap-2">
            <button
              type="button"
              onClick={() => setOpen(false)}
              className="rounded-lg border border-border px-3 py-1.5 text-xs"
            >
              Cancel
            </button>
            <button
              type="button"
              disabled={!form || busy}
              onClick={() => void save()}
              className="rounded-lg bg-accent px-4 py-1.5 text-[12.5px] font-semibold text-white disabled:opacity-50"
            >
              {busy ? "Saving…" : "Save"}
            </button>
          </div>
        </div>
      )}
    </div>
  );
}
