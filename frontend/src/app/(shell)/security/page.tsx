"use client";

import { SettingsTabs } from "@/components/settings/SettingsTabs";
import { useState } from "react";
import {
  useSecurityChecks,
  type CheckResult,
  type CheckStatus,
} from "@/lib/api/securityChecks";
import { friendlyErrorMessage } from "@/lib/api/errors";
import { t } from "@/lib/i18n/translate";

const statusStyle: Record<CheckStatus, string> = {
  pass: "bg-ok/15 text-ok",
  fail: "bg-err/15 text-err",
  unknown: "bg-surface-2 text-text-muted",
};
const statusLabel: Record<CheckStatus, string> = {
  pass: "PASS",
  fail: "FAIL",
  unknown: "UNKNOWN",
};
const sevStyle = {
  high: "text-err",
  medium: "text-warn",
  low: "text-text-muted",
};

function Check({ r }: { r: CheckResult }) {
  const [open, setOpen] = useState(false);
  const warn = r.status === "fail" && r.severity !== "high";
  return (
    <div className="rounded-lg border border-border bg-surface">
      <button
        type="button"
        onClick={() => setOpen(!open)}
        aria-expanded={open}
        className="flex w-full items-center gap-3 px-4 py-3 text-left"
      >
        <span
          className={`rounded px-2 py-0.5 font-mono text-[11px] ${
            warn ? "bg-warn/15 text-warn" : statusStyle[r.status]
          }`}
        >
          {warn ? "WARN" : statusLabel[r.status]}
        </span>
        <span className="flex-1 text-[13px] font-medium">{r.title}</span>
        <span className={`text-[11px] uppercase ${sevStyle[r.severity]}`}>
          {r.severity}
        </span>
        {r.status === "fail" && (
          <span className="font-mono text-xs text-text-muted">{r.total}</span>
        )}
      </button>
      {open && (
        <div className="flex flex-col gap-3 border-t border-border px-4 py-3 text-xs">
          <div className="text-text-muted">{r.description}</div>
          {r.status === "unknown" && (
            <div className="text-warn">{r.reason}</div>
          )}
          {r.findings.length > 0 && (
            <div className="flex flex-col gap-1">
              {r.findings.map((f, i) => (
                <div
                  key={i}
                  className={`flex flex-wrap gap-x-2 font-mono text-[11.5px] ${
                    f.system ? "text-text-faint" : ""
                  }`}
                >
                  <span className="text-text-muted">{f.kind}</span>
                  <span>
                    {f.namespace ? `${f.namespace}/` : ""}
                    {f.name}
                  </span>
                  {f.detail && (
                    <span className="text-text-muted">{f.detail}</span>
                  )}
                  {f.system && <span>({t("system namespace")})</span>}
                </div>
              ))}
              {r.total + r.systemTotal > r.findings.length && (
                <div className="text-text-faint">
                  {t("{n} more not shown", {
                    n: r.total + r.systemTotal - r.findings.length,
                  })}
                </div>
              )}
            </div>
          )}
          <div>
            <span className="font-semibold">{t("How to fix")}: </span>
            {r.remediation}
          </div>
        </div>
      )}
    </div>
  );
}

export default function SecurityPage() {
  const { data, isLoading, error, mutate, isValidating } = useSecurityChecks();
  const categories = data
    ? Array.from(new Set(data.results.map((r) => r.category)))
    : [];

  return (
    <div className="flex flex-col gap-[18px] px-8 py-7">
      <SettingsTabs active="security" />
      <div className="flex items-start justify-between gap-4">
        <div>
          <div className="max-w-3xl text-xs text-text-muted">
            A quick review of the cluster, made only from what you are allowed
            to read — nothing is changed and nothing runs on the nodes. It does
            not replace a full benchmark such as kube-bench, which looks at the
            nodes themselves (kubelet and API-server settings, file permissions,
            etcd).
          </div>
        </div>
        <button
          type="button"
          onClick={() => mutate()}
          disabled={isValidating}
          className="rounded-md border border-border px-3 py-1.5 text-xs hover:bg-surface-2 disabled:opacity-50"
        >
          Run again
        </button>
      </div>

      {isLoading && <div className="text-xs text-text-muted">Loading…</div>}
      {error && (
        <div className="text-xs text-err">{friendlyErrorMessage(error)}</div>
      )}

      {data && (
        <>
          <div className="flex gap-6 text-xs">
            <span className="text-err">
              {t("{n} failed", { n: data.failed })}
            </span>
            <span className="text-ok">
              {t("{n} passed", { n: data.passed })}
            </span>
            <span className="text-text-muted">
              {t("{n} unknown", { n: data.unknown })}
            </span>
          </div>
          {categories.map((c) => (
            <section key={c} className="flex flex-col gap-2">
              <div className="text-[11px] font-semibold uppercase tracking-wide text-text-muted">
                {c}
              </div>
              {data.results
                .filter((r) => r.category === c)
                .map((r) => (
                  <Check key={r.id} r={r} />
                ))}
            </section>
          ))}
        </>
      )}
    </div>
  );
}
