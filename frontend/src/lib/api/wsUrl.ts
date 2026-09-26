import { getCurrentCluster } from "@/lib/clusterStore";
import { API_BASE } from "./config";

// A WebSocket handshake is still a plain HTTP request, so the session
// cookie (Roadmap Phase 10) rides along automatically — no token query
// param needed. The selected cluster (Roadmap Phase 11) still travels as
// `?cluster=`, since that isn't something a cookie can carry.
//
// API_BASE is usually "" (same-origin — see lib/api/config.ts), so the
// WebSocket URL is built from the current page's own origin; a
// same-origin `?path?query` string wouldn't work for `new WebSocket()`,
// which needs a full ws(s):// URL.
export function buildStreamURL(path: string): string {
  const params = new URLSearchParams();
  const cluster = getCurrentCluster();
  if (cluster) {
    params.set("cluster", cluster);
  }

  const separator = path.includes("?") ? "&" : "?";
  const qs = params.toString();
  const fullPath = qs ? `${path}${separator}${qs}` : path;

  if (API_BASE) {
    return `${API_BASE.replace(/^http/, "ws")}${fullPath}`;
  }
  const wsProtocol = window.location.protocol === "https:" ? "wss:" : "ws:";
  return `${wsProtocol}//${window.location.host}${fullPath}`;
}
