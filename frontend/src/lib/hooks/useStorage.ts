import useSWR from "swr";
import { getPVC, getPVCs, getPVs, getStorageClasses } from "@/lib/api/storage";
import { useCurrentCluster } from "@/lib/clusterStore";

// `enabled` lets the Storage page (Roadmap Phase 13) call all three hooks
// unconditionally (rules of hooks) while only the active tab actually
// fetches — an SWR key of `null` skips the request entirely.
export function usePVCs(namespace = "", enabled = true) {
  const cluster = useCurrentCluster();
  return useSWR(enabled ? ["pvcs", namespace, cluster] : null, () =>
    getPVCs(namespace),
  );
}

export function usePVs(enabled = true) {
  const cluster = useCurrentCluster();
  return useSWR(enabled ? ["pvs", cluster] : null, () => getPVs());
}

export function useStorageClasses(enabled = true) {
  const cluster = useCurrentCluster();
  return useSWR(enabled ? ["storageclasses", cluster] : null, () =>
    getStorageClasses(),
  );
}

export function usePVC(namespace: string, name: string) {
  const cluster = useCurrentCluster();
  return useSWR(["pvc", namespace, name, cluster], () =>
    getPVC(namespace, name),
  );
}
