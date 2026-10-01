import { API_BASE } from "@/lib/api/config";

// Roadmap Phase 26: where the backend's authenticated reverse proxy serves a
// pod's port. The cluster is part of the path (not the X-Cluster header the
// API uses) because the browser navigates to this URL directly and every
// sub-request of the proxied page must resolve to the same cluster.
export function buildProxyUrl(opts: {
  cluster: string;
  namespace: string;
  pod: string;
  port: number;
  https: boolean;
}): string {
  const target = `${opts.https ? "https:" : ""}${opts.port}`;
  return `${API_BASE}/proxy/${encodeURIComponent(opts.cluster)}/${opts.namespace}/${opts.pod}/${target}/`;
}
