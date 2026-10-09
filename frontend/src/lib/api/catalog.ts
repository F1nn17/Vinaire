import { apiFetch } from "./http";
import type { Recipe } from "@/lib/values/recipe";

// Real endpoints (Roadmap Phase 28): the Helm application catalog. Browsing is
// read-only; install/upgrade/rollback/uninstall run as the signed-in user's own
// Kubernetes identity on the backend, so the cluster's RBAC decides.

export type CatalogRepo = {
  name: string;
  url: string;
  description?: string;
  category?: string;
  // an OCI registry rather than a classic Helm repository
  oci?: boolean;
};

export type ChartSummary = {
  repo: string;
  name: string;
  version: string;
  appVersion?: string;
  description?: string;
  keywords?: string[];
  deprecated?: boolean;
  category?: string;
  // an icon the backend serves (see components/marketplace/ChartIcon)
  hasIcon?: boolean;
};

export type CatalogSearch = {
  charts: ChartSummary[];
  // matches before the list was cut
  total: number;
  // repositories still loading their index; a later search will include them
  pending?: string[];
  // repositories that couldn't be reached — one broken repo doesn't blank the page
  errors?: Record<string, string>;
  // matches left out because the chart is deprecated (unmaintained, maybe unsafe)
  hiddenDeprecated?: number;
};

export type ChartVersionInfo = {
  version: string;
  appVersion?: string;
  created?: string;
  deprecated?: boolean;
};

export type ChartDetails = ChartSummary & {
  home?: string;
  versions: ChartVersionInfo[];
  readme?: string;
  values: string; // the chart's default values.yaml
  hasSchema: boolean;
  // the chart's values.schema.json, for the settings form (absent if none / too big)
  schema?: string;
  // has a top-level `nodeSelector` value, so "run on node" can work
  supportsNodeSelector: boolean;
  // Roadmap Phase 59: install help for charts the catalog knows well
  recipe?: Recipe;
};

export type HelmRelease = {
  name: string;
  namespace: string;
  revision: number;
  status: string;
  chart: string;
  chartVersion: string;
  appVersion?: string;
  updated: string;
  description?: string;
  // set when the release was installed through Vinaire's catalog
  repo?: string;
};

export type InstallResult = { release: HelmRelease; notes?: string };

export function getCatalogRepos(): Promise<{ repos: CatalogRepo[] }> {
  return apiFetch("/api/v1/catalog/repos");
}

export function searchCatalog(
  search: string,
  repo: string,
  category = "",
  includeDeprecated = false,
): Promise<CatalogSearch> {
  const p = new URLSearchParams();
  if (includeDeprecated) p.set("deprecated", "true");
  if (search) p.set("search", search);
  if (repo) p.set("repo", repo);
  if (category) p.set("category", category);
  return apiFetch(`/api/v1/catalog/charts?${p.toString()}`);
}

export function getChart(
  repo: string,
  chart: string,
  version?: string,
): Promise<ChartDetails> {
  const q = version ? `?version=${encodeURIComponent(version)}` : "";
  return apiFetch(
    `/api/v1/catalog/charts/${encodeURIComponent(repo)}/${encodeURIComponent(chart)}${q}`,
  );
}

export async function getReleases(namespace?: string): Promise<HelmRelease[]> {
  const q = namespace ? `?namespace=${encodeURIComponent(namespace)}` : "";
  const res = await apiFetch<{ releases: HelmRelease[] }>(
    `/api/v1/catalog/releases${q}`,
  );
  return res.releases;
}

export async function getReleaseHistory(
  namespace: string,
  name: string,
): Promise<HelmRelease[]> {
  const res = await apiFetch<{ revisions: HelmRelease[] }>(
    `/api/v1/catalog/releases/${encodeURIComponent(namespace)}/${encodeURIComponent(name)}/history`,
  );
  return res.revisions;
}

// The values a release was installed with. They can hold passwords, so the
// backend audits this call like a Secret reveal — only call it on demand.
export async function getReleaseValues(
  namespace: string,
  name: string,
): Promise<string> {
  const res = await apiFetch<{ values: string }>(
    `/api/v1/catalog/releases/${encodeURIComponent(namespace)}/${encodeURIComponent(name)}/values`,
  );
  return res.values;
}

const json = { "Content-Type": "application/json" };

export type InstallRequest = {
  repo: string;
  chart: string;
  version?: string;
  namespace: string;
  release: string;
  values: string; // YAML
  nodeName?: string;
  createNamespace?: boolean;
  wait?: boolean;
  // the explicit "install this deprecated chart anyway"
  allowDeprecated?: boolean;
};

export function installChart(req: InstallRequest): Promise<InstallResult> {
  return apiFetch("/api/v1/catalog/install", {
    method: "POST",
    headers: json,
    body: JSON.stringify(req),
  });
}

export type UpgradeRequest = {
  repo: string;
  chart: string;
  version?: string;
  values: string;
  nodeName?: string;
  wait?: boolean;
};

export function upgradeRelease(
  namespace: string,
  name: string,
  req: UpgradeRequest,
): Promise<InstallResult> {
  return apiFetch(
    `/api/v1/catalog/releases/${encodeURIComponent(namespace)}/${encodeURIComponent(name)}/upgrade`,
    { method: "POST", headers: json, body: JSON.stringify(req) },
  );
}

export async function rollbackRelease(
  namespace: string,
  name: string,
  revision: number,
): Promise<void> {
  await apiFetch(
    `/api/v1/catalog/releases/${encodeURIComponent(namespace)}/${encodeURIComponent(name)}/rollback`,
    { method: "POST", headers: json, body: JSON.stringify({ revision }) },
  );
}

// `confirmName` must equal `name` — the backend re-checks it.
export async function uninstallRelease(
  namespace: string,
  name: string,
): Promise<{ leftovers?: string[] }> {
  return apiFetch(
    `/api/v1/catalog/releases/${encodeURIComponent(namespace)}/${encodeURIComponent(name)}`,
    {
      method: "DELETE",
      headers: json,
      body: JSON.stringify({ confirmName: name }),
    },
  );
}

// What an install would need that the caller lacks (nothing is created by asking).
export type MissingPermission = {
  group: string;
  resource: string;
  namespace?: string;
  verbs: string[];
};

export type Preflight = {
  objects: number;
  missing: MissingPermission[];
  // kinds that couldn't be checked (typically CRDs the chart installs itself)
  unchecked?: string[];
  // Roadmap Phase 59: what the install creates, the storage it asks for, what the cluster lacks
  created: { kind: string; count: number }[];
  volumes: { name: string; size?: string; storageClass?: string }[];
  warnings: { code: string; detail?: string }[];
};

export function preflightInstall(req: InstallRequest): Promise<Preflight> {
  return apiFetch("/api/v1/catalog/install/preflight", {
    method: "POST",
    headers: json,
    body: JSON.stringify(req),
  });
}

// "get/create networkpolicies.networking.k8s.io in db" — one line per gap.
export function describeMissing(m: MissingPermission): string {
  const res = m.group ? `${m.resource}.${m.group}` : m.resource;
  return `${m.verbs.join("/")} ${res} ${m.namespace ? `in namespace ${m.namespace}` : "(cluster-wide)"}`;
}

// Missing permissions carried by a refused install (HTTP 403), if any.
export function missingFromError(err: unknown): MissingPermission[] {
  const data = (err as { data?: { missing?: unknown } } | null)?.data;
  return Array.isArray(data?.missing)
    ? (data.missing as MissingPermission[])
    : [];
}

export type InstallJobStage =
  | "downloading"
  | "checking"
  | "installing"
  | "rollingback"
  | "done"
  | "failed"
  | "cancelled";

export type InstallJob = {
  id: string;
  stage: InstallJobStage;
  // the stage that was running when the job failed or was cancelled
  failedStage?: InstallJobStage;
  chart: string;
  release: string;
  namespace: string;
  bytesDone: number;
  bytesTotal: number; // -1 = the server didn't say
  bytesPerSec: number;
  elapsedSec: number;
  error?: string;
  forbidden?: boolean;
  missing?: MissingPermission[];
  unchecked?: string[];
  result?: InstallResult;
  // Roadmap Phase 60: what a cancel or a rollback did
  rolledBack?: boolean;
  rollbackError?: string;
  namespaceCreated?: boolean;
  leftovers?: string[];
  // the install failed after Helm began and its release can be uninstalled
  canRollBack?: boolean;
};

export type InstallPod = {
  name: string;
  phase: string;
  ready: number;
  total: number;
  reason?: string;
};

export async function startInstallJob(req: InstallRequest): Promise<string> {
  const res = await apiFetch<{ id: string }>("/api/v1/catalog/install-jobs", {
    method: "POST",
    headers: json,
    body: JSON.stringify(req),
  });
  return res.id;
}

export function getInstallJob(
  id: string,
): Promise<{ job: InstallJob; pods?: InstallPod[] }> {
  return apiFetch(`/api/v1/catalog/install-jobs/${encodeURIComponent(id)}`);
}

export async function cancelInstallJob(id: string): Promise<void> {
  await apiFetch(`/api/v1/catalog/install-jobs/${encodeURIComponent(id)}`, {
    method: "DELETE",
    headers: json,
  });
}
