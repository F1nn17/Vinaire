"use client";

import { useHealthz } from "@/lib/hooks/useHealthz";
import { StatusDot } from "@/components/ui/StatusDot";

// Shown only when something is wrong. A healthy backend needs no badge — the
// pages working is the signal — so this stays out of the way until the backend
// can't be reached.
export function BackendStatus() {
  const { data, error, isLoading } = useHealthz();

  if (isLoading || (!error && data?.status === "ok")) {
    return null;
  }

  return (
    <span
      role="status"
      className="flex items-center gap-1.5 rounded-full border border-err/40 bg-surface px-2.5 py-0.75 font-mono text-[11px] text-err"
    >
      <StatusDot color="err" size={6} />
      backend: unreachable
    </span>
  );
}
