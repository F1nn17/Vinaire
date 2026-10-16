"use client";

import Link from "next/link";
import { t } from "@/lib/i18n/translate";
import type { InstallJob, InstallPod } from "@/lib/api/catalog";

// Live progress of a Marketplace install (an install job): downloading the chart
// (bytes, speed, time left), checking the caller's rights, installing, and — once
// objects exist — the release's pods coming up.

export function formatBytes(n: number): string {
  if (n < 1024) return `${n} B`;
  if (n < 1024 * 1024) return `${(n / 1024).toFixed(1)} KB`;
  return `${(n / (1024 * 1024)).toFixed(1)} MB`;
}

function formatSeconds(s: number): string {
  if (!isFinite(s) || s < 0) return "—";
  if (s < 1) return t("<1 s");
  if (s < 60) return t("{n} s", { n: Math.round(s) });
  return t("{m} min {s} s", {
    m: Math.floor(s / 60),
    s: Math.round(s % 60),
  });
}

type Step = "downloading" | "checking" | "installing";
const steps: { key: Step; label: string }[] = [
  { key: "downloading", label: "Download the chart" },
  { key: "checking", label: "Check your permissions" },
  { key: "installing", label: "Install" },
];

function stepState(
  job: InstallJob,
  step: Step,
): "todo" | "active" | "done" | "failed" {
  if (job.stage === "done") return "done";
  const idx = steps.findIndex((s) => s.key === step);
  if (
    job.stage === "failed" ||
    job.stage === "cancelled" ||
    job.stage === "rollingback"
  ) {
    const at = steps.findIndex((s) => s.key === job.failedStage);
    if (idx < at) return "done";
    return idx === at ? "failed" : "todo";
  }
  const cur = steps.findIndex((s) => s.key === job.stage);
  if (idx < cur) return "done";
  return idx === cur ? "active" : "todo";
}

function Mark({ state }: { state: ReturnType<typeof stepState> }) {
  if (state === "active") {
    return (
      <span
        aria-hidden
        className="inline-block h-3.5 w-3.5 animate-spin rounded-full border-2 border-accent border-t-transparent"
      />
    );
  }
  const glyph = { done: "✓", failed: "✕", todo: "○" }[state];
  const color = {
    done: "text-ok",
    failed: "text-err",
    todo: "text-text-faint",
  }[state];
  return (
    <span aria-hidden className={`w-3.5 text-center ${color}`}>
      {glyph}
    </span>
  );
}

export function InstallProgress({
  job,
  pods,
  onCancel,
}: {
  job: InstallJob;
  pods?: InstallPod[];
  onCancel?: () => void;
}) {
  const known = job.bytesTotal > 0;
  const pct = known
    ? Math.min(100, Math.round((job.bytesDone / job.bytesTotal) * 100))
    : null;
  const left =
    known && job.bytesPerSec > 0
      ? (job.bytesTotal - job.bytesDone) / job.bytesPerSec
      : NaN;
  const canCancel =
    (job.stage === "downloading" ||
      job.stage === "checking" ||
      job.stage === "installing") &&
    !!onCancel;
  // Phase 60: the step that takes back what a cancelled or failed install made
  const rollback: "active" | "done" | "failed" | null =
    job.stage === "rollingback"
      ? "active"
      : job.rolledBack
        ? "done"
        : job.rollbackError
          ? "failed"
          : null;

  return (
    <div
      className="flex flex-col gap-3 rounded-lg border border-border bg-surface p-4 text-xs"
      role="status"
      aria-live="polite"
    >
      {steps.map((s) => {
        const state = stepState(job, s.key);
        return (
          <div key={s.key} className="flex flex-col gap-1.5">
            <div className="flex items-center gap-2">
              <Mark state={state} />
              <span className={state === "todo" ? "text-text-faint" : ""}>
                {s.label}
              </span>
            </div>
            {s.key === "downloading" && state === "active" && (
              <div className="ml-5.5 flex flex-col gap-1">
                <div
                  className="h-2 overflow-hidden rounded bg-surface-2"
                  role="progressbar"
                  aria-valuemin={0}
                  aria-valuemax={100}
                  aria-valuenow={pct ?? undefined}
                >
                  <div
                    className={`h-full bg-accent ${pct === null ? "w-1/3 animate-pulse" : ""}`}
                    style={pct === null ? undefined : { width: `${pct}%` }}
                  />
                </div>
                <div className="font-mono text-[11px] text-text-muted">
                  {formatBytes(job.bytesDone)}
                  {known ? ` of ${formatBytes(job.bytesTotal)} (${pct}%)` : ""}
                  {job.bytesPerSec > 0
                    ? ` · ${formatBytes(job.bytesPerSec)}/s`
                    : ""}
                  {known
                    ? t(" · {b} left", {
                        b: formatBytes(job.bytesTotal - job.bytesDone),
                      })
                    : ""}
                  {isFinite(left) ? ` · ~${formatSeconds(left)}` : ""}
                </div>
              </div>
            )}
            {s.key === "installing" && state === "active" && (
              <div className="ml-5.5 text-[11px] text-text-faint">
                Helm is creating the release&apos;s objects —{" "}
                {formatSeconds(job.elapsedSec)} so far. You can stop it: what it
                has created is then taken back.
              </div>
            )}
          </div>
        );
      })}

      {rollback && (
        <div className="flex items-center gap-2">
          <Mark state={rollback} />
          <span>Roll back what was created</span>
        </div>
      )}

      {pods && pods.length > 0 && (
        <div className="flex flex-col gap-1 border-t border-border pt-3">
          <div className="text-text-muted">
            Pods (
            {pods.filter((p) => p.total > 0 && p.ready === p.total).length}/
            {pods.length} ready)
          </div>
          {pods.map((p) => (
            <div
              key={p.name}
              className="flex items-center gap-2 font-mono text-[11px]"
            >
              <span
                className={
                  p.total > 0 && p.ready === p.total
                    ? "text-ok"
                    : p.reason?.includes("BackOff") || p.phase === "Failed"
                      ? "text-err"
                      : "text-warn"
                }
              >
                ●
              </span>
              <span className="truncate">{p.name}</span>
              <span className="text-text-faint">
                {p.ready}/{p.total} · {p.reason || p.phase}
              </span>
              {!(p.total > 0 && p.ready === p.total) && (
                <Link
                  href={`/workloads/pods/detail?namespace=${encodeURIComponent(job.namespace)}&name=${encodeURIComponent(p.name)}`}
                  className="font-sans text-accent-text underline"
                >
                  {t("Why?")}
                </Link>
              )}
            </div>
          ))}
        </div>
      )}

      {canCancel && (
        <div>
          <button
            type="button"
            onClick={onCancel}
            className="rounded-lg border border-border px-3 py-1.5 text-[11.5px] text-text-muted hover:bg-surface-2"
          >
            {job.stage === "installing" ? "Stop and roll back" : "Cancel"}
          </button>
        </div>
      )}
    </div>
  );
}
