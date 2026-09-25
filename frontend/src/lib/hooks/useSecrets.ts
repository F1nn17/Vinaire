import useSWR from "swr";
import { getSecrets } from "@/lib/api/secrets";

export function useSecrets(namespace = "") {
  return useSWR(["secrets", namespace], () => getSecrets(namespace));
}
