import useSWR from "swr";
import { useCurrentCluster } from "@/lib/clusterStore";
import { apiFetch } from "./http";

// The audit viewer (Roadmap Phase 42): the records Vinaire writes for every
// mutating action. Matches the backend's audit.Entry / AuditLog response.

export type AuditEntry = {
  time: string;
  action: string;
  namespace: string;
  name: string;
  identity: string;
  remoteAddr: string;
  detail?: Record<string, unknown>;
};

export type AuditResponse = {
  // false when this install keeps no audit file (AUDIT_LOG_PATH unset)
  available: boolean;
  entries: AuditEntry[];
  truncated?: boolean;
  actions: string[];
  scanned?: number;
  oldest?: string;
};

export type AuditFilter = {
  action: string;
  identity: string;
  namespace: string;
  name: string;
  q: string;
  since: string; // "1h" | "24h" | "7d" | "30d" | ""
};

export function useAudit(f: AuditFilter, refreshMs: number) {
  const cluster = useCurrentCluster();
  return useSWR(
    ["audit", cluster, f],
    () => {
      const p = new URLSearchParams();
      for (const [k, v] of Object.entries(f)) if (v) p.set(k, v);
      p.set("limit", "500");
      return apiFetch<AuditResponse>(`/api/v1/audit?${p.toString()}`);
    },
    {
      refreshInterval: refreshMs,
      keepPreviousData: true,
      shouldRetryOnError: false,
    },
  );
}
