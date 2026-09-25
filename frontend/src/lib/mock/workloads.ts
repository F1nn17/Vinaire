import type { WorkloadKind } from "@/lib/types";

export const workloadTabs: { key: WorkloadKind; label: string }[] = [
  { key: "deployments", label: "Deployments" },
  { key: "pods", label: "Pods" },
  { key: "statefulsets", label: "StatefulSets" },
  { key: "daemonsets", label: "DaemonSets" },
  { key: "jobs", label: "Jobs" },
];
