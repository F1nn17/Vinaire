"use client";

import { useEffect, useRef, useState } from "react";
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

  useEffect(() => {
    function onClickOutside(e: MouseEvent) {
      if (rootRef.current && !rootRef.current.contains(e.target as Node)) {
        setOpen(false);
      }
    }
    document.addEventListener("mousedown", onClickOutside);
    return () => document.removeEventListener("mousedown", onClickOutside);
  }, []);

  const alerts = data?.available ? data.alerts : [];

  return (
    <div ref={rootRef} className="relative">
      <button
        type="button"
        aria-label="Alerts"
        onClick={() => setOpen((v) => !v)}
        className="relative text-text-muted"
      >
        <BellIcon />
        {alerts.length > 0 && (
          <span
            aria-hidden="true"
            className="absolute -right-0.5 -top-0.5 h-2 w-2 rounded-full border-2 border-bg bg-err"
          />
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
            alerts.map((a, i) => (
              <div
                key={i}
                className="flex flex-col gap-0.5 rounded-md px-2.5 py-2"
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
              </div>
            ))
          )}
        </div>
      )}
    </div>
  );
}
