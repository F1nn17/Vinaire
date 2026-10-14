"use client";

import { useEffect, useRef, useState } from "react";
import { t } from "@/lib/i18n/translate";
import { readAlertIds, rememberRead } from "@/lib/readAlerts";
import { BellIcon } from "@/components/icons";
import { useAlerts } from "@/lib/hooks/useObservability";
import { StatusDot } from "@/components/ui/StatusDot";

// Roadmap Phase 16: a real dropdown sourced from GET /api/v1/alerts,
// replacing the static red-dot decoration. If Alertmanager isn't
// configured/reachable the dropdown says so explicitly instead of just
// looking empty.
export function AlertsBell() {
  const { data } = useAlerts();
  const [open, setOpen] = useState(false);
  const rootRef = useRef<HTMLDivElement>(null);
  // marked as read in this browser (a cookie); nothing is drawn from it before the alerts arrive
  const [read, setRead] = useState<string[]>(() => readAlertIds());
  useEffect(() => {
    function onClickOutside(e: MouseEvent) {
      if (rootRef.current && !rootRef.current.contains(e.target as Node)) {
        setOpen(false);
      }
    }
    document.addEventListener("mousedown", onClickOutside);
    return () => document.removeEventListener("mousedown", onClickOutside);
  }, []);

  const alerts = data?.available ? (data.alerts ?? []) : [];
  const isNew = (a: { id: string; suppressed?: boolean }) =>
    !a.suppressed && !read.includes(a.id);
  const unread = alerts.filter(isNew);
  // unread first, then the read and the suppressed ones
  const ordered = [...alerts].sort(
    (a, b) => Number(isNew(b)) - Number(isNew(a)),
  );
  const markRead = (ids: string[]) => setRead(rememberRead(ids));

  return (
    <div ref={rootRef} className="relative">
      <button
        type="button"
        aria-label={
          unread.length > 0 ? `Alerts (${unread.length} unread)` : "Alerts"
        }
        onClick={() => setOpen((v) => !v)}
        className="relative text-text-muted"
      >
        <BellIcon />
        {unread.length > 0 && (
          <span
            aria-hidden="true"
            className="absolute -right-2 -top-2 flex h-4 min-w-4 items-center justify-center rounded-full border-2 border-bg bg-err px-1 text-[9px] font-semibold leading-none text-white"
          >
            {unread.length > 99 ? "99+" : unread.length}
          </span>
        )}
      </button>

      {open && (
        <div className="absolute right-0 top-[calc(100%+10px)] z-20 max-h-[360px] w-[320px] overflow-auto rounded-lg border border-border bg-surface p-1.5 shadow-lg">
          {!data ? (
            <div className="px-2.5 py-2 text-xs text-text-muted">Loading…</div>
          ) : !data.available ? (
            <div className="px-2.5 py-2 text-xs text-text-muted">
              Alertmanager unavailable: {data.reason}
            </div>
          ) : alerts.length === 0 ? (
            <div className="px-2.5 py-2 text-xs text-text-muted">
              No active alerts.
            </div>
          ) : (
            <>
              {unread.length > 0 && (
                <button
                  type="button"
                  onClick={() => markRead(unread.map((a) => a.id))}
                  className="mb-1 w-full rounded-md px-2.5 py-1.5 text-left text-[11px] text-accent-text hover:bg-surface-2"
                >
                  {t("Mark all as read")}
                </button>
              )}
              {ordered.map((a) => (
                <div
                  key={a.id}
                  className={`flex flex-col gap-0.5 rounded-md px-2.5 py-2 ${
                    isNew(a) ? "" : "opacity-60"
                  }`}
                >
                  <span className="flex items-center gap-1.5 font-mono text-[12.5px] font-semibold">
                    <StatusDot color={a.statusColor} />
                    {a.name}
                  </span>
                  {a.summary && (
                    <span className="text-[11px] text-text-muted">
                      {a.summary}
                    </span>
                  )}
                  <span className="flex items-center gap-2 text-[10.5px] text-text-faint">
                    {a.suppressed && <span>{t("silenced")}</span>}
                    {isNew(a) && (
                      <button
                        type="button"
                        onClick={() => markRead([a.id])}
                        className="text-accent-text"
                      >
                        {t("Mark as read")}
                      </button>
                    )}
                  </span>
                </div>
              ))}
            </>
          )}
        </div>
      )}
    </div>
  );
}
