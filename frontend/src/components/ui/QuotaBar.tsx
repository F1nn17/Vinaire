import type { QuotaUsageRow } from "@/lib/types";

const colorVar: Record<string, string> = {
  ok: "var(--ok)",
  warn: "var(--warn)",
  err: "var(--err)",
};

// One ResourceQuota resource as a usage bar (Roadmap Phase 27) — color comes
// from the backend (ok/warn/err by how close to the hard limit).
export function QuotaBar({
  row,
  wide = false,
}: {
  row: QuotaUsageRow;
  wide?: boolean;
}) {
  return (
    <div className="flex flex-col gap-1">
      <span className="font-mono text-[11px] text-text-muted">
        {row.resource} {row.used} / {row.hard}
      </span>
      <div
        className={`h-1.5 overflow-hidden rounded bg-surface-2 ${wide ? "w-56" : "w-28"}`}
      >
        <div
          className="h-full"
          style={{ width: `${row.pct}%`, background: colorVar[row.color] }}
        />
      </div>
    </div>
  );
}
