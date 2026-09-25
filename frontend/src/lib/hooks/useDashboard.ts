import useSWR from "swr";
import { getDashboard } from "@/lib/api/dashboard";

export function useDashboard() {
  return useSWR("dashboard", getDashboard);
}
