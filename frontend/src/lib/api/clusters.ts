import type { Cluster, ClusterType } from "@/lib/types";
import { apiFetch } from "./http";
import type { HostKey, SshTarget } from "./provision";

// Real endpoint (Roadmap Phase 11) — every kubeconfig context the backend
// is configured to talk to, plus any cluster added dynamically through the
// cluster-management UI (the Clusters page).
export function getClusters(): Promise<Cluster[]> {
  return apiFetch<Cluster[]>("/api/v1/clusters");
}

export type AddClusterRequest = {
  name: string;
  server: string;
  token: string;
  caData?: string;
};

// Validates the submitted credentials against a real API call server-side
// before persisting anything — a bad token/server fails this call directly,
// not silently later on every page that tries to use the new cluster.
export function addCluster(req: AddClusterRequest): Promise<Cluster> {
  return apiFetch<Cluster>("/api/v1/clusters", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(req),
  });
}

// --- Roadmap Phase 34: more ways to add a cluster, discovery, types ---

export type ClusterManagement = {
  available: boolean;
  store?: "secret" | "file";
  encrypted: boolean;
  reason?: string;
};

export function getClusterManagement(): Promise<ClusterManagement> {
  return apiFetch<ClusterManagement>("/api/v1/clusters/management");
}

export type DiscoveredCluster = {
  id: string;
  name: string;
  source: string;
  context: string;
  server: string;
  type: ClusterType;
  reachable: boolean;
  version?: string;
  credential: "client-certificate" | "token" | "none";
  clusterAdmin: boolean | null;
  alreadyAdded: boolean;
  problem?: string;
};

export type DiscoverResult = {
  available: boolean;
  reason?: string;
  candidates: DiscoveredCluster[];
  unreadable: { path: string; hint: string }[];
  listeningApis: { address: string; note: string }[];
  searchedPaths: string[];
};

export function discoverClusters(): Promise<DiscoverResult> {
  return apiFetch<DiscoverResult>("/api/v1/clusters/discover");
}

const json = { "Content-Type": "application/json" };

export function addDiscoveredCluster(req: {
  id: string;
  name?: string;
  type?: ClusterType;
}): Promise<Cluster> {
  return apiFetch<Cluster>("/api/v1/clusters/add-discovered", {
    method: "POST",
    headers: json,
    body: JSON.stringify(req),
  });
}

export type KubeconfigPreview = {
  contexts: string[];
  current: string;
  selected: string;
  server: string;
  credentialKind: "client-certificate" | "token" | "none";
  insecureTls: boolean;
  problem?: string;
};

export function previewKubeconfig(
  kubeconfig: string,
  context?: string,
): Promise<KubeconfigPreview> {
  return apiFetch<KubeconfigPreview>(
    "/api/v1/clusters/import-kubeconfig/preview",
    {
      method: "POST",
      headers: json,
      body: JSON.stringify({ kubeconfig, context }),
    },
  );
}

export function importKubeconfig(req: {
  name: string;
  kubeconfig: string;
  context?: string;
  type?: ClusterType;
}): Promise<Cluster> {
  return apiFetch<Cluster>("/api/v1/clusters/import-kubeconfig", {
    method: "POST",
    headers: json,
    body: JSON.stringify(req),
  });
}

export function setClusterType(
  name: string,
  type: ClusterType,
): Promise<{ status: string; persisted: boolean }> {
  return apiFetch(`/api/v1/clusters/${encodeURIComponent(name)}/type`, {
    method: "PUT",
    headers: json,
    body: JSON.stringify({ type }),
  });
}

// SSH modes: same host-key / credential rules as Phase 30's Add node (HTTPS
// only, credentials in the body only, confirm the fingerprint first).
export type ClusterSshRequest = {
  provider: "k3s";
  name: string;
  node: SshTarget;
  address?: string;
  serverUrl?: string;
  version?: string;
  type?: ClusterType;
};

export function fetchClusterHostKey(
  host: string,
  port: number,
): Promise<HostKey> {
  return apiFetch<HostKey>("/api/v1/clusters/ssh/hostkey", {
    method: "POST",
    headers: json,
    body: JSON.stringify({ host, port }),
  });
}

export function startClusterSsh(
  mode: "import" | "create",
  req: ClusterSshRequest,
): Promise<{ jobId: string }> {
  return apiFetch<{ jobId: string }>(`/api/v1/clusters/${mode}-ssh`, {
    method: "POST",
    headers: json,
    body: JSON.stringify(req),
  });
}

export function removeCluster(name: string): Promise<void> {
  return apiFetch<void>(`/api/v1/clusters/${encodeURIComponent(name)}`, {
    method: "DELETE",
  });
}
