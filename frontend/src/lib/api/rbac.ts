import type {
  ClusterRoleBindingRow,
  ClusterRoleRow,
  RBACBindingSummary,
  RoleBindingRow,
  RoleRow,
  ServiceAccountRow,
} from "@/lib/types";
import { apiFetch } from "./http";

// Real endpoints (Roadmap Phase 14).
export function getRoles(namespace = ""): Promise<RoleRow[]> {
  const query = namespace ? `?namespace=${encodeURIComponent(namespace)}` : "";
  return apiFetch<RoleRow[]>(`/api/v1/roles${query}`);
}

// ClusterRoles are cluster-scoped — no namespace parameter.
export function getClusterRoles(): Promise<ClusterRoleRow[]> {
  return apiFetch<ClusterRoleRow[]>("/api/v1/clusterroles");
}

export function getRoleBindings(namespace = ""): Promise<RoleBindingRow[]> {
  const query = namespace ? `?namespace=${encodeURIComponent(namespace)}` : "";
  return apiFetch<RoleBindingRow[]>(`/api/v1/rolebindings${query}`);
}

// ClusterRoleBindings are cluster-scoped — no namespace parameter.
export function getClusterRoleBindings(): Promise<ClusterRoleBindingRow[]> {
  return apiFetch<ClusterRoleBindingRow[]>("/api/v1/clusterrolebindings");
}

export function getServiceAccounts(
  namespace = "",
): Promise<ServiceAccountRow[]> {
  const query = namespace ? `?namespace=${encodeURIComponent(namespace)}` : "";
  return apiFetch<ServiceAccountRow[]>(`/api/v1/serviceaccounts${query}`);
}

// A static "what's configured" rollup — never a live SubjectAccessReview/
// "can I do X" check (see AGENTS.md / ROADMAP.md Phase 14's scope note).
export function getRBACSummary(namespace = ""): Promise<RBACBindingSummary[]> {
  const query = namespace ? `?namespace=${encodeURIComponent(namespace)}` : "";
  return apiFetch<RBACBindingSummary[]>(`/api/v1/rbac/summary${query}`);
}
