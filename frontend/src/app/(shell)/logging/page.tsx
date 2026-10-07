"use client";

import Link from "next/link";
import { useRef, useState } from "react";
import useSWR from "swr";
import { friendlyErrorMessage } from "@/lib/api/errors";
import {
  cancelInstallJob,
  getInstallJob,
  startInstallJob,
  uninstallRelease,
  type InstallPod,
  type InstallJob,
} from "@/lib/api/catalog";
import {
  useLoggingPlan,
  useLoggingStatus,
  type LoggingOptions,
  type PlanStep,
} from "@/lib/api/logging";
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
      help: "There is no Loki in this cluster, so the history search on a pod's Logs tab has nothing to search. One click installs it.",
    },
    "loki-only": {
      label: "Loki found, nothing ships logs to it",
      tone: "text-warn",
      help: "A Loki is running but no collector (Alloy, Promtail…) was found, so it holds no logs. Install a collector from the Marketplace and point it at Loki.",
    },
    ready: {
      label: "Ready",
      tone: "text-ok",
      help: "Loki and a collector are running. Open a pod's Logs tab → History to search.",
    },
    configured: {
      label: "Using LOKI_URL",
      tone: "text-ok",
      help: "This Vinaire was configured with a Loki address by its operator, and uses that.",
    },
  };

export default function LoggingPage() {
  const [installing, setInstalling] = useState(false);
  // Once an install has started the installer stays on screen: the moment Loki's
  // Service exists the cluster status stops being "none", which must not tear the
  // running install (and its progress) out from under the user.
  const [session, setSession] = useState(false);
  const {
    data: status,
    error,
    isLoading,
    mutate,
  } = useLoggingStatus(installing);
  const [removing, setRemoving] = useState<string[] | null>(null);
  const [removeError, setRemoveError] = useState<string | null>(null);
  const [removeBusy, setRemoveBusy] = useState(false);

  async function remove(releases: string[]) {
    if (!status?.loki) return;
    setRemoveBusy(true);
    setRemoveError(null);
    try {
      for (const r of releases) {
        await uninstallRelease(status.loki.namespace, r).catch((e) => {
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
        <div className="text-[15px] font-semibold">Logs stack</div>
        <div className="mt-1 max-w-3xl text-xs text-text-muted">
          The live tail of a pod&apos;s logs needs nothing.{" "}
          <b>History and search</b> need a log store: Loki keeps the logs, and a
          collector (Grafana Alloy) ships every pod&apos;s logs to it.
          Installing runs as <i>you</i> — the same Helm install as the
          Marketplace, so your permissions decide and you see what&apos;s
          missing before it starts.
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
              {status.state === "ready" && (
                <span
                  className={`text-xs ${status.flowing ? "text-ok" : "text-warn"}`}
                >
                  {status.flowing ? "● logs are arriving" : "○ no pod logs yet"}
                </span>
              )}
            </div>
            <div className="mt-1.5 text-xs text-text-muted">
              {stateText[status.state].help}
            </div>
            {status.loki && (
              <div className="mt-3 flex flex-col gap-1 font-mono text-[11px] text-text-muted">
                <div>
                  Loki: {status.loki.namespace}/{status.loki.service}:
                  {status.loki.port}
                </div>
                {(status.collectors?.length ?? 0) > 0 && (
                  <div>Collectors: {status.collectors!.join(", ")}</div>
                )}
              </div>
            )}
            {status.detail && (
              <div className="mt-2 text-xs text-warn">{status.detail}</div>
            )}
            {status.managedByVinaire && (
              <div className="mt-4">
                <button
                  type="button"
                  onClick={() => {
                    setRemoveError(null);
                    setRemoving(["alloy", "loki"]);
                  }}
                  className="rounded-lg border border-border px-3 py-1.5 text-xs text-err hover:border-err"
                >
                  Remove the logs stack
                </button>
                <span className="ml-3 text-[11px] text-text-faint">
                  Uninstalls Alloy and Loki; the stored logs go with Loki&apos;s
                  volume.
                </span>
              </div>
            )}
          </div>

          {(status.state === "none" || session) && (
            <Installer
              onRunning={(v) => {
                if (v) setSession(true);
                setInstalling(v);
              }}
              onFinished={() => void mutate()}
            />
          )}
        </>
      )}

      {removing && status?.loki && (
        <TypeToConfirmModal
          title="Remove the logs stack?"
          message={`This uninstalls Alloy and Loki from "${status.loki.namespace}". Stored logs are deleted with Loki's volume, and the History search stops working until you install it again.`}
          expected="remove logs"
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

function Installer({
  onRunning,
  onFinished,
}: {
  onRunning: (v: boolean) => void;
  onFinished: () => void;
}) {
  const [opts, setOpts] = useState<LoggingOptions>({
    namespace: "logging",
    retentionDays: 7,
    storageSize: "10Gi",
    storageClass: "",
  });
  const [noVolume, setNoVolume] = useState(false);
  const effective: LoggingOptions = {
    ...opts,
    storageSize: noVolume ? "none" : opts.storageSize,
  };
  const {
    data: plan,
    error: planError,
    isLoading,
  } = useLoggingPlan(effective, true);

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
    run ? ["logging-job", run.jobId] : null,
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
          Loki and Alloy are running. Logs start arriving within a minute; then
          open a pod&apos;s{" "}
          <Link href="/workloads" className="text-accent-text underline">
            Logs tab → History
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
      <div className="text-[13px] font-semibold">Install the logs stack</div>
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
        <label className="flex flex-col gap-1.5 text-xs text-text-muted">
          Keep logs for (days)
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
        <label className="flex flex-col gap-1.5 text-xs text-text-muted">
          Volume size
          <input
            aria-label="Volume size"
            value={opts.storageSize}
            disabled={noVolume}
            onChange={(e) => setOpts({ ...opts, storageSize: e.target.value })}
            className={`${input} disabled:opacity-50`}
          />
        </label>
        <label className="flex flex-col gap-1.5 text-xs text-text-muted">
          Storage class (empty = the cluster default)
          <input
            aria-label="Storage class"
            value={opts.storageClass}
            disabled={noVolume}
            onChange={(e) => setOpts({ ...opts, storageClass: e.target.value })}
            className={`${input} disabled:opacity-50`}
          />
        </label>
      </div>
      <label className="flex items-center gap-2 text-xs text-text-muted">
        <input
          type="checkbox"
          checked={noVolume}
          onChange={(e) => setNoVolume(e.target.checked)}
        />
        No volume — logs are lost whenever Loki&apos;s pod restarts
      </label>

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
                {s.title}{" "}
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
              {w}
            </div>
          ))}
          {plan.notes.map((n) => (
            <div key={n} className="text-[11px] text-text-faint">
              {n}
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
            !plan || plan.warnings.some((w) => w.includes("already running"))
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
