import useSWR from "swr";
import { useCurrentCluster } from "@/lib/clusterStore";
import { apiFetch } from "./http";

// Security checks (Roadmap Phase 54, light version): a fixed list of read-only checks over
// what the signed-in user can already read. Nothing is written and nothing runs on a node.

export type CheckStatus = "pass" | "fail" | "unknown";
export type CheckSeverity = "high" | "medium" | "low";

export type CheckFinding = {
  kind: string;
  namespace?: string;
  name: string;
  detail?: string;
  system?: boolean;
};

export type CheckResult = {
  id: string;
  category: string;
  severity: CheckSeverity;
  title: string;
  description: string;
  remediation: string;
  status: CheckStatus;
  reason?: string;
  total: number;
  systemTotal: number;
  findings: CheckFinding[];
};

export type SecurityChecks = {
  generatedAt: string;
  results: CheckResult[];
  failed: number;
  passed: number;
  unknown: number;
};

export function useSecurityChecks() {
  const cluster = useCurrentCluster();
  return useSWR(
    ["security-checks", cluster],
    () => apiFetch<SecurityChecks>("/api/v1/security/checks"),
    { revalidateOnFocus: false },
  );
}
