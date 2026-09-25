import useSWR from "swr";
import { getSecrets } from "@/lib/api/secrets";

export function useSecrets() {
  return useSWR("secrets", getSecrets);
}
