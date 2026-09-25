import { gatewayMock, ingressMock } from "@/lib/mock/network";
import type { GatewayRow, IngressRow } from "@/lib/types";
import { simulatedDelay } from "./config";

// TODO(vinaire): replace with `fetch(`${API_BASE}/api/v1/ingresses`)`.
export async function getIngresses(): Promise<IngressRow[]> {
  await simulatedDelay();
  return ingressMock;
}

// TODO(vinaire): replace with `fetch(`${API_BASE}/api/v1/gateway-routes`)`.
export async function getGatewayResources(): Promise<GatewayRow[]> {
  await simulatedDelay();
  return gatewayMock;
}
