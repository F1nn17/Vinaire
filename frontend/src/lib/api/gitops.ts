import { apiFetch } from "./http";

// Real endpoints (Roadmap Phase 29): per-cluster GitOps. "native" is Vinaire's
// own small poll-and-apply loop (it applies as a ServiceAccount whose token the
// source was created with); "argocd" shows and drives an Argo CD that is already
// in the cluster; "off" hides the section.

export type GitOpsMode = "off" | "native" | "argocd";

export type Phase = "Pending" | "Syncing" | "Synced" | "OutOfSync" | "Error";

export type ObjectState = "Synced" | "OutOfSync" | "Missing" | "Error";

export type ObjectRef = {
  apiVersion: string;
  kind: string;
  namespace?: string;
  name: string;
};

export type ObjectStatus = ObjectRef & { state: ObjectState; message?: string };

export type SourceStatus = {
  phase: Phase;
  message?: string;
  revision?: string;
  appliedRevision?: string;
  checkedAt?: string;
  syncedAt?: string;
  objects: ObjectStatus[];
  // applied earlier, no longer in git — native mode never deletes
  orphans?: ObjectRef[];
};

export type GitSource = {
  id: string;
  name: string;
  cluster: string;
  repoURL: string;
  revision: string;
  path: string;
  recurse: boolean;
  namespace: string;
  intervalSeconds: number;
  autoSync: boolean;
  force: boolean;
  hasGitToken: boolean;
  applyAs: string;
  createdBy?: string;
  createdAt: string;
  status: SourceStatus;
  warnings?: string[];
};

export type GitOpsOverview = {
  cluster: string;
  available: boolean;
  mode: GitOpsMode;
  argoDetected: boolean;
  warning?: string;
  sources: GitSource[];
};

export type ArgoApplication = {
  namespace: string;
  name: string;
  project?: string;
  repoURL?: string;
  path?: string;
  targetRevision?: string;
  syncStatus?: string;
  health?: string;
  revision?: string;
  operation?: string;
  message?: string;
  syncing: boolean;
};

const json = { "Content-Type": "application/json" };

export function getGitOps(): Promise<GitOpsOverview> {
  return apiFetch("/api/v1/gitops");
}

export function setGitOpsMode(mode: GitOpsMode): Promise<GitOpsOverview> {
  return apiFetch("/api/v1/gitops/mode", {
    method: "PUT",
    headers: json,
    body: JSON.stringify({ mode }),
  });
}

export type NewSource = {
  name: string;
  repoURL: string;
  revision?: string;
  path?: string;
  recurse?: boolean;
  namespace?: string;
  intervalSeconds?: number;
  autoSync: boolean;
  force: boolean;
  gitUsername?: string;
  gitToken?: string; // optional, read-only; sealed at rest, never returned
  applyToken: string; // the ServiceAccount the manifests are applied as
};

export function addGitSource(body: NewSource): Promise<GitSource> {
  return apiFetch("/api/v1/gitops/sources", {
    method: "POST",
    headers: json,
    body: JSON.stringify(body),
  });
}

export function getGitSource(id: string): Promise<GitSource> {
  return apiFetch(`/api/v1/gitops/sources/${encodeURIComponent(id)}`);
}

export async function removeGitSource(id: string): Promise<void> {
  await apiFetch(`/api/v1/gitops/sources/${encodeURIComponent(id)}`, {
    method: "DELETE",
  });
}

export async function syncGitSource(id: string): Promise<void> {
  await apiFetch(`/api/v1/gitops/sources/${encodeURIComponent(id)}/sync`, {
    method: "POST",
  });
}

export function getArgoApplications(): Promise<{
  detected: boolean;
  applications: ArgoApplication[];
}> {
  return apiFetch("/api/v1/gitops/argocd/applications");
}

export async function syncArgoApplication(
  namespace: string,
  name: string,
): Promise<void> {
  await apiFetch(
    `/api/v1/gitops/argocd/applications/${encodeURIComponent(namespace)}/${encodeURIComponent(name)}/sync`,
    { method: "POST" },
  );
}
