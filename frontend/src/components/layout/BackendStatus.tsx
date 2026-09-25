"use client";

import { useHealthz } from "@/lib/hooks/useHealthz";
import { StatusDot } from "@/components/ui/StatusDot";
import type { StatusColor } from "@/lib/types";

export function BackendStatus() {
  const { data, error, isLoading } = useHealthz();

  const connected = !error && data?.status === "ok";
  const color: StatusColor = isLoading ? "warn" : connected ? "ok" : "err";
  const label = isLoading
    ? "backend: connecting…"
    : connected
      ? "backend: connected"
      : "backend: unreachable";

  return (
    <span className="flex items-center gap-1.5 rounded-full border border-border bg-surface px-2.5 py-[3px] font-mono text-[11px] text-text-muted">
      <StatusDot color={color} size={6} />
      {label}
    </span>
  );
}
