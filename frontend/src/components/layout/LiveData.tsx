"use client";

import type { ReactNode } from "react";
import { SWRConfig, type Middleware } from "swr";

// Resource data (lists and detail pages) refreshes itself, so what a scale, a
// restart or a delete does to the cluster shows up without reloading the page:
// the action's own refetch usually runs before the cluster has caught up.
// Only keys named here poll; forms, install checks and searches do not.
const LIVE = new Set([
  "workloads",
  "deployment",
  "statefulset",
  "daemonset",
  "job",
  "pod",
  "nodes",
  "node",
  "namespaces",
  "namespace",
  "configmaps",
  "secrets",
  "serviceaccounts",
  "pvcs",
  "pvc",
  "pvs",
  "storageclasses",
  "ingresses",
  "gateway-resources",
  "helm-releases",
  "helm-history",
  "dashboard",
  "capacity",
  "crd-instances",
  "crd-instance",
  "roles",
  "rolebindings",
  "clusterroles",
  "clusterrolebindings",
]);

const POLL_MS = 5000;

const polling: Middleware = (useSWRNext) => (key, fetcher, config) => {
  const name = Array.isArray(key) ? key[0] : key;
  const live = typeof name === "string" && LIVE.has(name);
  return useSWRNext(key, fetcher, {
    ...config,
    refreshInterval: config.refreshInterval || (live ? POLL_MS : 0),
  });
};

export function LiveData({ children }: { children: ReactNode }) {
  return <SWRConfig value={{ use: [polling] }}>{children}</SWRConfig>;
}
