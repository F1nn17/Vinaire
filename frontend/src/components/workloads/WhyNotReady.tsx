"use client";

import { usePod } from "@/lib/hooks/useWorkloads";
import { t } from "@/lib/i18n/translate";
import { PodDiagnosis } from "./PodDiagnosis";

// The reason a pod is not ready, read from the pod itself (Roadmap Phase 75), for places that watch
// pods come up (the image wizard, an install's progress): what the cluster says, in plain words. When
// the diagnosis has nothing to say, the generic `fallback` (if any) stays.
export function WhyNotReady({
  namespace,
  name,
  fallback,
}: {
  namespace: string;
  name: string;
  fallback?: string;
}) {
  const { data } = usePod(namespace, name);
  const findings = data?.diagnosis ?? [];
  if (findings.length > 0) return <PodDiagnosis findings={findings} />;
  return fallback ? <span className="text-warn">{t(fallback)}</span> : null;
}
