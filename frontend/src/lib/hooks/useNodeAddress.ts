import useSWR from "swr";
import { parse } from "yaml";
import { getResourceObject, useResourceTable } from "@/lib/api/allResources";

// The address a node answers on from outside — its external IP when it has one, else its internal
// one — which is what a NodePort is reached at. Read as the signed-in user; "" until known or when
// the nodes can't be read. (A NodePort is open on every node; the first one is as good as any.)
export function useNodeAddress(enabled: boolean): string {
  const nodes = useResourceTable(enabled ? "core/v1/nodes" : null, "");
  const first = nodes.data?.rows[0]?.name ?? "";
  const address = useSWR(
    first ? ["node-address", first] : null,
    async () => {
      const obj = await getResourceObject("core/v1/nodes", "", first);
      const doc = parse(obj.yaml) as {
        status?: { addresses?: { type?: string; address?: string }[] };
      } | null;
      const list = doc?.status?.addresses ?? [];
      return (
        list.find((a) => a.type === "ExternalIP")?.address ??
        list.find((a) => a.type === "InternalIP")?.address ??
        ""
      );
    },
    { revalidateOnFocus: false },
  );
  return address.data ?? "";
}
