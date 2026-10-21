import useSWR from "swr";
import { apiFetch } from "./http";
import { useCurrentCluster } from "@/lib/clusterStore";

// Backups of an application installed from an image (Roadmap Phase 77b). Reads are as the caller;
// creating and deleting are the audited POST/DELETE below, decided by the cluster's own RBAC.
export type BackupItem = {
  kind: "job" | "cronjob" | "snapshot";
  role?: "restore";
  name: string;
  created: string;
  state: string;
  target?: string;
  schedule?: string;
  claim?: string;
  suspended?: boolean;
};

export type BackupConfig = {
  s3Targets: {
    name: string;
    endpoint: string;
    bucket: string;
    prefix?: string;
  }[];
  snapshots: { available: boolean; classes: string[] };
};

export type CreateBackup = {
  kind: "dump" | "snapshot";
  // "pvc", or the name of an S3 target from the operator's configuration
  target?: string;
  size?: string;
  credentialsSecret?: string;
  schedule?: string;
  snapshotClass?: string;
};

const base = (namespace: string, name: string) =>
  `/api/v1/instances/${encodeURIComponent(namespace)}/${encodeURIComponent(name)}/backups`;

export function useBackupConfig() {
  const cluster = useCurrentCluster();
  return useSWR(["backup-config", cluster], () =>
    apiFetch<BackupConfig>("/api/v1/backups/config"),
  );
}

export function useBackups(namespace: string, name: string) {
  const cluster = useCurrentCluster();
  return useSWR(
    ["backups", cluster, namespace, name],
    () => apiFetch<{ backups: BackupItem[] }>(base(namespace, name)),
    { refreshInterval: 5000 },
  );
}

export async function createBackup(
  namespace: string,
  name: string,
  body: CreateBackup,
): Promise<{ created: string[] }> {
  return apiFetch(base(namespace, name), {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(body),
  });
}

export async function deleteBackup(
  namespace: string,
  kind: BackupItem["kind"],
  name: string,
): Promise<void> {
  await apiFetch(
    `/api/v1/backups/${encodeURIComponent(namespace)}/${kind}/${encodeURIComponent(name)}`,
    {
      method: "DELETE",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ confirmName: name }),
    },
  );
}

// Restoring (Roadmap Phase 77c) never goes over a running instance: a dump is loaded into a NEW
// instance (the one in the URL, named again in confirmName), a snapshot becomes a NEW claim.
export async function restoreDump(
  namespace: string,
  target: string,
  body: { source: string; file?: string },
): Promise<{ created: string[] }> {
  return apiFetch(
    `/api/v1/instances/${encodeURIComponent(namespace)}/${encodeURIComponent(target)}/restore`,
    {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ ...body, confirmName: target }),
    },
  );
}

export async function restoreSnapshot(
  namespace: string,
  instance: string,
  body: { snapshot: string; claim: string; size?: string },
): Promise<{ created: string[] }> {
  return apiFetch(
    `/api/v1/instances/${encodeURIComponent(namespace)}/${encodeURIComponent(instance)}/restore-snapshot`,
    {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(body),
    },
  );
}
