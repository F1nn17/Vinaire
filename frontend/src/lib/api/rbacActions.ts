import { apiFetch } from "./http";

// RBAC management (Roadmap Phase 44): bind an existing role to someone, remove a
// binding, and ask "what can X do?". Calls run as the signed-in user, so the
// cluster's own escalation check decides (see AGENTS.md's write list).

const json = { "Content-Type": "application/json" };

export type SubjectKind = "User" | "Group" | "ServiceAccount";

export type Subject = {
  kind: SubjectKind;
  name: string;
  namespace?: string; // ServiceAccount only
};

export type BindingRequest = {
  scope: "cluster" | "namespace";
  namespace?: string;
  name?: string; // generated when empty
  roleKind: "ClusterRole" | "Role";
  roleName: string;
  subjects: Subject[];
  // cluster-wide grants: the role's name, typed
  confirmName?: string;
};

export async function createBinding(req: BindingRequest): Promise<string> {
  const res = await apiFetch<{ name: string }>("/api/v1/rbac/bindings", {
    method: "POST",
    headers: json,
    body: JSON.stringify(req),
  });
  return res.name;
}

export async function deleteRoleBinding(
  namespace: string,
  name: string,
): Promise<void> {
  await apiFetch(
    `/api/v1/rbac/rolebindings/${encodeURIComponent(namespace)}/${encodeURIComponent(name)}`,
    {
      method: "DELETE",
      headers: json,
      body: JSON.stringify({ confirmName: name }),
    },
  );
}

export async function deleteClusterRoleBinding(name: string): Promise<void> {
  await apiFetch(
    `/api/v1/rbac/clusterrolebindings/${encodeURIComponent(name)}`,
    {
      method: "DELETE",
      headers: json,
      body: JSON.stringify({ confirmName: name }),
    },
  );
}

// bindings Vinaire (or the cluster's own setup) must not remove from here
export function isProtectedBinding(name: string): boolean {
  return name.startsWith("system:") || name === "cluster-admin";
}

export type AccessCheck = {
  subject: Subject;
  verb: string;
  group: string;
  resource: string;
  subresource?: string;
  namespace?: string;
  name?: string;
};

export type AccessAnswer = {
  allowed: boolean;
  denied?: boolean;
  reason?: string;
  evaluationError?: string;
};

export function checkAccess(req: AccessCheck): Promise<AccessAnswer> {
  return apiFetch("/api/v1/rbac/check", {
    method: "POST",
    headers: json,
    body: JSON.stringify(req),
  });
}

export type SubjectBinding = {
  kind: "RoleBinding" | "ClusterRoleBinding";
  name: string;
  namespace?: string;
  roleKind: string;
  roleName: string;
  via: string; // "direct" or "group <name>"
  rules: { apiGroups: string; resources: string; verbs: string }[];
};

export async function getSubjectBindings(
  s: Subject,
): Promise<SubjectBinding[]> {
  const q = new URLSearchParams({ kind: s.kind, name: s.name });
  if (s.namespace) q.set("namespace", s.namespace);
  const res = await apiFetch<{ bindings: SubjectBinding[] }>(
    `/api/v1/rbac/subject?${q.toString()}`,
  );
  return res.bindings;
}
