"use client";

import { useState } from "react";
import { API_BASE } from "@/lib/api/config";

// A chart's icon. The image comes from Vinaire's own backend (which fetched and
// checked it), never straight from the third-party host named in the chart — the
// page's Content-Security-Policy would block that, and it would tell that host who
// is browsing. Without an icon, or if it fails to load, a letter stands in.
export function ChartIcon({
  repo,
  chart,
  hasIcon,
  size = 36,
}: {
  repo: string;
  chart: string;
  hasIcon?: boolean;
  size?: number;
}) {
  const [failed, setFailed] = useState(false);
  const box = { width: size, height: size };

  if (hasIcon && !failed) {
    return (
      // eslint-disable-next-line @next/next/no-img-element -- static export, no image optimizer
      <img
        src={`${API_BASE}/api/v1/catalog/icons/${encodeURIComponent(repo)}/${encodeURIComponent(chart)}`}
        alt=""
        loading="lazy"
        width={size}
        height={size}
        onError={() => setFailed(true)}
        className="shrink-0 rounded-md bg-surface-2 object-contain p-0.5"
        style={box}
      />
    );
  }
  return (
    <span
      aria-hidden
      className="flex shrink-0 items-center justify-center rounded-md bg-surface-2 font-semibold uppercase text-accent-text"
      style={{ ...box, fontSize: size * 0.42 }}
    >
      {chart.charAt(0) || "?"}
    </span>
  );
}
