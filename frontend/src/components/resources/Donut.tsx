"use client";

// A single-value ring: `pct` of the circle filled (capped at 100 — over-
// commitment is shown in text, not by a ring that can't hold more). The value is
// also printed in the centre, so colour is never the only carrier of meaning.
const colors = {
  ok: "var(--ok)",
  warn: "var(--warn)",
  err: "var(--err)",
  accent: "var(--accent)",
};

export function Donut({
  pct,
  level = "accent",
  size = 112,
  stroke = 12,
  label,
  sublabel,
}: {
  pct: number;
  level?: keyof typeof colors;
  size?: number;
  stroke?: number;
  label?: string;
  sublabel?: string;
}) {
  const r = (size - stroke) / 2;
  const c = 2 * Math.PI * r;
  const shown = Math.max(0, Math.min(100, pct));
  return (
    <div
      className="relative shrink-0"
      style={{ width: size, height: size }}
      role="img"
      aria-label={`${label ?? "Value"}: ${pct}%`}
    >
      <svg width={size} height={size} viewBox={`0 0 ${size} ${size}`}>
        <circle
          cx={size / 2}
          cy={size / 2}
          r={r}
          fill="none"
          stroke="var(--surface-2)"
          strokeWidth={stroke}
        />
        <circle
          cx={size / 2}
          cy={size / 2}
          r={r}
          fill="none"
          stroke={colors[level]}
          strokeWidth={stroke}
          strokeLinecap="round"
          strokeDasharray={`${(shown / 100) * c} ${c}`}
          transform={`rotate(-90 ${size / 2} ${size / 2})`}
        />
      </svg>
      <div className="absolute inset-0 flex flex-col items-center justify-center">
        <span className="font-mono text-lg font-semibold leading-none">
          {pct}%
        </span>
        {sublabel && (
          <span className="mt-1 text-[10px] text-text-faint">{sublabel}</span>
        )}
      </div>
    </div>
  );
}
