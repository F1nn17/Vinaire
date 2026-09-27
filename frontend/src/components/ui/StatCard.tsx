import type { StatCardData } from "@/lib/types";
import { StatusDot } from "./StatusDot";

export function StatCard({ title, dotColor, value, caption }: StatCardData) {
  return (
    <div className="rounded-[10px] border border-border bg-surface p-[18px]">
      <div className="flex items-center justify-between">
        <span className="text-xs uppercase tracking-wide text-text-muted">
          {title}
        </span>
        <StatusDot color={dotColor} size={8} />
      </div>
      {/* Roadmap Phase 18: null ("not available yet") renders visibly
          muted and as a dash — never identical to a real 0, which renders
          bold like any other number. */}
      <div
        className={`mt-2.5 font-mono text-[32px] font-semibold ${value === null ? "text-text-faint" : ""}`}
      >
        {value === null ? "—" : value}
      </div>
      <div className="mt-1 text-xs text-text-faint">{caption}</div>
    </div>
  );
}
