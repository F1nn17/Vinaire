import type { LogLine as LogLineType } from "@/lib/types";

const levelColor: Record<LogLineType["level"], string> = {
  INFO: "var(--ok)",
  WARN: "var(--warn)",
  ERROR: "var(--err)",
};

export function LogLine({ time, level, text }: LogLineType) {
  return (
    <div className="flex gap-2.5 py-0.5 font-mono text-xs">
      <span className="shrink-0 text-text-faint">{time}</span>
      <span className="w-11 shrink-0" style={{ color: levelColor[level] }}>
        {level}
      </span>
      <span className="text-text">{text}</span>
    </div>
  );
}
