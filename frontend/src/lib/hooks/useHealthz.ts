import useSWR from "swr";
import { getHealthz } from "@/lib/api/health";

export function useHealthz() {
  return useSWR("healthz", getHealthz, {
    refreshInterval: 5000,
    shouldRetryOnError: true,
    errorRetryInterval: 5000,
  });
}
