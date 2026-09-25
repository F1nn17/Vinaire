import type { ClusterEvent } from "@/lib/types";
import { StatusDot } from "./StatusDot";

export function EventRow({ statusColor, title, meta, time }: ClusterEvent) {
  return (
    <div className="flex items-start gap-3 border-t border-border py-2.5">
      <span className="mt-1.5">
        <StatusDot color={statusColor} size={7} />
      </span>
      <div className="min-w-0 flex-1">
        <div className="text-[13px]">{title}</div>
        <div className="mt-0.5 font-mono text-[11px] text-text-faint">
          {meta}
        </div>
      </div>
      <span className="shrink-0 font-mono text-[11px] text-text-faint">
        {time}
      </span>
    </div>
  );
}
