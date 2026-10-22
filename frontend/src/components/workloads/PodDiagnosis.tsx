"use client";

import { tm } from "@/lib/i18n/messages";
import type { PodFinding } from "@/lib/types";

const tone = {
  error: "border-err/40 text-err",
  warning: "border-warn/40 text-warn",
  info: "border-border text-accent-text",
} as const;

// Roadmap Phase 75: why the pod is not running or not ready, in plain words — what Kubernetes knows
// (conditions, container states, events), read by the backend (internal/diagnose) and written so that
// someone who has not met `CrashLoopBackOff` before can act on it. The title and the hints are fixed
// text and are translated; the detail is the cluster's own message, shown as it is.
export function PodDiagnosis({ findings }: { findings: PodFinding[] }) {
  if (findings.length === 0) return null;
  return (
    <div className="flex flex-col gap-2.5" data-testid="pod-diagnosis">
      {findings.map((f) => (
        <div
          key={`${f.code}/${f.container ?? ""}`}
          className={`rounded-[10px] border bg-surface p-4 text-xs ${tone[f.severity]}`}
        >
          <div className="flex flex-wrap items-baseline gap-2">
            <span className="text-[13px] font-semibold">{tm(f.title)}</span>
            {f.container && (
              <span className="font-mono text-[11px] text-text-faint">
                {f.container}
              </span>
            )}
          </div>
          {f.detail && (
            <div className="mt-1.5 break-words font-mono text-[11px] text-text-muted">
              {f.detail}
            </div>
          )}
          {f.hints && f.hints.length > 0 && (
            <ul className="mt-2 list-disc pl-5 text-text-muted">
              {f.hints.map((h) => (
                <li key={h}>{tm(h)}</li>
              ))}
            </ul>
          )}
        </div>
      ))}
    </div>
  );
}
