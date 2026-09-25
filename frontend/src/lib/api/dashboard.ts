import { dashboardMock } from "@/lib/mock/dashboard";
import type { DashboardData } from "@/lib/types";
import { simulatedDelay } from "./config";

// TODO(vinaire): replace with `fetch(`${API_BASE}/api/v1/dashboard`)` once
// the backend exposes an aggregate dashboard endpoint (read-only).
export async function getDashboard(): Promise<DashboardData> {
  await simulatedDelay();
  return dashboardMock;
}
