import { configMapsMock } from "@/lib/mock/configmaps";
import type { ConfigMapDetail } from "@/lib/types";
import { simulatedDelay } from "./config";

// TODO(vinaire): replace with `fetch(`${API_BASE}/api/v1/configmaps`)`.
export async function getConfigMaps(): Promise<ConfigMapDetail[]> {
  await simulatedDelay();
  return configMapsMock;
}
