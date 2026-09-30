"use client";

import dynamic from "next/dynamic";

// Monaco is large and browser-only — load it on demand, never during the
// static export's prerender, so pages that don't open an editor don't pay
// for it.
const loading = () => (
  <div className="flex h-full items-center justify-center p-6 text-sm text-text-muted">
    Loading editor…
  </div>
);

export const YamlEditor = dynamic(
  () => import("./MonacoViews").then((m) => m.MonacoYamlEditor),
  { ssr: false, loading },
);

export const YamlDiff = dynamic(
  () => import("./MonacoViews").then((m) => m.MonacoYamlDiff),
  { ssr: false, loading },
);
