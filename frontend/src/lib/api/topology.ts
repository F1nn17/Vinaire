import { topologyMock } from "@/lib/mock/topology";
import type { TopologyData } from "@/lib/types";
import { simulatedDelay } from "./config";

// TODO(vinaire): replace with `fetch(`${API_BASE}/api/v1/topology?namespace=`)`.
export async function getTopology(): Promise<TopologyData> {
  await simulatedDelay();
  return topologyMock;
}
