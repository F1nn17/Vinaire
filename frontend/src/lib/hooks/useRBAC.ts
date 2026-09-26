import useSWR from "swr";
import {
  getClusterRoleBindings,
  getClusterRoles,
  getRBACSummary,
  getRoleBindings,
  getRoles,
  getServiceAccounts,
} from "@/lib/api/rbac";
import { useCurrentCluster } from "@/lib/clusterStore";

// `enabled` lets the Access control page (Roadmap Phase 14) call every hook
// unconditionally (rules of hooks) while only the active tab actually
// fetches — an SWR key of `null` skips the request entirely.
export function useRoles(namespace = "", enabled = true) {
  const cluster = useCurrentCluster();
  return useSWR(enabled ? ["roles", namespace, cluster] : null, () =>
    getRoles(namespace),
  );
}

export function useClusterRoles(enabled = true) {
  const cluster = useCurrentCluster();
  return useSWR(enabled ? ["clusterroles", cluster] : null, () =>
    getClusterRoles(),
  );
}

export function useRoleBindings(namespace = "", enabled = true) {
  const cluster = useCurrentCluster();
  return useSWR(enabled ? ["rolebindings", namespace, cluster] : null, () =>
    getRoleBindings(namespace),
  );
}

export function useClusterRoleBindings(enabled = true) {
  const cluster = useCurrentCluster();
  return useSWR(enabled ? ["clusterrolebindings", cluster] : null, () =>
    getClusterRoleBindings(),
  );
}

export function useServiceAccounts(namespace = "", enabled = true) {
  const cluster = useCurrentCluster();
  return useSWR(enabled ? ["serviceaccounts", namespace, cluster] : null, () =>
    getServiceAccounts(namespace),
  );
}

export function useRBACSummary(namespace = "", enabled = true) {
  const cluster = useCurrentCluster();
  return useSWR(enabled ? ["rbac-summary", namespace, cluster] : null, () =>
    getRBACSummary(namespace),
  );
}
