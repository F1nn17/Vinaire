import useSWR from "swr";
import { getCRDInstance, getCRDInstances, getCRDs } from "@/lib/api/crds";
import { useCurrentCluster } from "@/lib/clusterStore";

export function useCRDs() {
  const cluster = useCurrentCluster();
  return useSWR(["crds", cluster], () => getCRDs());
}

export function useCRDInstances(
  crdName: string,
  namespace = "",
  enabled = true,
) {
  const cluster = useCurrentCluster();
  return useSWR(
    enabled && crdName ? ["crd-instances", crdName, namespace, cluster] : null,
    () => getCRDInstances(crdName, namespace),
  );
}

export function useCRDInstance(crdName: string, name: string, namespace = "") {
  const cluster = useCurrentCluster();
  return useSWR(
    crdName && name
      ? ["crd-instance", crdName, name, namespace, cluster]
      : null,
    () => getCRDInstance(crdName, name, namespace),
  );
}
