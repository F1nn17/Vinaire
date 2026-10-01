import type { ClusterType } from "@/lib/types";

export const clusterTypeLabels: Record<ClusterType, string> = {
  k3s: "k3s",
  k3d: "k3d",
  kind: "kind",
  minikube: "minikube",
  microk8s: "MicroK8s",
  kubeadm: "kubeadm",
  managed: "managed",
  unknown: "unknown",
};

export const clusterTypes = Object.keys(clusterTypeLabels) as ClusterType[];

// The detected kind of a cluster (Roadmap Phase 34), as a small pill.
export function ClusterTypeBadge({ type }: { type: ClusterType }) {
  return (
    <span className="rounded-full border border-border bg-surface-2 px-2 py-[1px] font-mono text-[10.5px] text-text-muted">
      {clusterTypeLabels[type] ?? type}
    </span>
  );
}
