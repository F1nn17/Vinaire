import { apiFetch } from "./http";

// Real endpoints (Roadmap Phase 28): the Helm application catalog. Browsing is
// read-only; install/upgrade/rollback/uninstall run as the signed-in user's own
// Kubernetes identity on the backend, so the cluster's RBAC decides.

export type CatalogRepo = { name: string; url: string; description?: string };

export type ChartSummary = {
  repo: string;
  name: string;
  version: string;
  appVersion?: string;
  description?: string;
  keywords?: string[];
  deprecated?: boolean;
};

export type CatalogSearch = {
  charts: ChartSummary[];
  // repositories that couldn't be reached — one broken repo doesn't blank the page
  errors?: Record<string, string>;
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
  // has a top-level `nodeSelector` value, so "run on node" can work
  supportsNodeSelector: boolean;
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
): Promise<CatalogSearch> {
  const p = new URLSearchParams();
  if (search) p.set("search", search);
  if (repo) p.set("repo", repo);
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
): Promise<void> {
  await apiFetch(
    `/api/v1/catalog/releases/${encodeURIComponent(namespace)}/${encodeURIComponent(name)}`,
    {
      method: "DELETE",
      headers: json,
      body: JSON.stringify({ confirmName: name }),
    },
  );
}
