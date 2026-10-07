import useSWR from "swr";
import { useCurrentCluster } from "@/lib/clusterStore";
import { apiFetch } from "./http";

// "All resources": any kind the cluster serves, read as the signed-in user
// (matches the backend's api.APIResourceInfo / TableResponse / ResourceObjectDetail).

export type APIResource = {
  group: string; // "" for the core group
  version: string;
  resource: string; // plural, as in the URL
  kind: string;
  namespaced: boolean;
  shortNames?: string[];
};

export type TableColumn = { name: string; type: string; description?: string };
export type TableRow = { name: string; namespace?: string; cells: string[] };
export type ResourceTable = {
  columns: TableColumn[];
  rows: TableRow[];
  namespaced: boolean;
  // the list was cut at the server's page size
  truncated?: boolean;
};

export type ResourceObject = {
  name: string;
  namespace?: string;
  kind: string;
  age: string;
  labels: Record<string, string>;
  yaml: string;
};

// "core/v1/pods", "apps/v1/deployments" — one string identifying a kind, used in
// URLs and as a key.
export function resourceKey(
  r: Pick<APIResource, "group" | "version" | "resource">,
): string {
  return `${r.group || "core"}/${r.version}/${r.resource}`;
}

export function parseResourceKey(
  key: string | null,
): { group: string; version: string; resource: string } | null {
  const parts = (key ?? "").split("/");
  if (parts.length !== 3 || parts.some((p) => p === "")) return null;
  return {
    group: parts[0] === "core" ? "" : parts[0],
    version: parts[1],
    resource: parts[2],
  };
}

function base(key: string): string {
  const [g, v, r] = key.split("/");
  return `/api/v1/api-resources/${encodeURIComponent(g)}/${encodeURIComponent(v)}/${encodeURIComponent(r)}/objects`;
}

export function useAPIResources() {
  const cluster = useCurrentCluster();
  return useSWR(
    ["api-resources", cluster],
    () => apiFetch<APIResource[]>("/api/v1/api-resources"),
    { revalidateOnFocus: false },
  );
}

export function useResourceTable(
  key: string | null,
  namespace: string,
  labelSelector = "",
) {
  const cluster = useCurrentCluster();
  return useSWR(
    key ? ["resource-table", key, namespace, labelSelector, cluster] : null,
    () => {
      const q = new URLSearchParams();
      if (namespace) q.set("namespace", namespace);
      if (labelSelector) q.set("labelSelector", labelSelector);
      const qs = q.toString();
      return apiFetch<ResourceTable>(
        `${base(key as string)}${qs ? `?${qs}` : ""}`,
      );
    },
    { refreshInterval: 15000, keepPreviousData: false },
  );
}

export function useResourceObject(
  key: string | null,
  namespace: string,
  name: string,
) {
  const cluster = useCurrentCluster();
  return useSWR(
    key && name ? ["resource-object", key, namespace, name, cluster] : null,
    () =>
      apiFetch<ResourceObject>(
        `${base(key as string)}/${encodeURIComponent(name)}${namespace ? `?namespace=${encodeURIComponent(namespace)}` : ""}`,
      ),
  );
}
