import { getSession } from "next-auth/react";
import { getCurrentCluster } from "@/lib/clusterStore";
import { API_BASE } from "./config";

// Browsers can't attach an Authorization header to a WebSocket handshake,
// so the caller's token travels as `?access_token=` instead (matches
// internal/authz.Verifier.VerifyRequest's fallback on the backend); the
// selected cluster (Roadmap Phase 11) travels as `?cluster=` the same way.
export async function buildStreamURL(path: string): Promise<string> {
  const session = await getSession();
  const params = new URLSearchParams();
  if (session?.accessToken) {
    params.set("access_token", session.accessToken);
  }
  const cluster = getCurrentCluster();
  if (cluster) {
    params.set("cluster", cluster);
  }

  const base = API_BASE.replace(/^http/, "ws");
  const separator = path.includes("?") ? "&" : "?";
  const qs = params.toString();
  return qs ? `${base}${path}${separator}${qs}` : `${base}${path}`;
}
