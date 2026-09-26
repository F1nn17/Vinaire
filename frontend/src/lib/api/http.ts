import { getCurrentCluster } from "@/lib/clusterStore";
import { API_BASE } from "./config";

export class ApiError extends Error {
  status: number;

  constructor(status: number, message: string) {
    super(message);
    this.name = "ApiError";
    this.status = status;
  }
}

// Thin fetch wrapper every real (non-mock) lib/api/*.ts function should go
// through, so the backend base URL, JSON parsing, error shape, auth
// (Roadmap Phase 10), and cluster selection (Phase 11) only live in one
// place.
//
// Auth here is a plain HttpOnly session cookie, not a token this code ever
// sees — `credentials: "include"` is what makes the browser attach it.
// A 401 means the session is missing or expired (Roadmap Phase 10's DoD:
// once a pasted token's TTL runs out, requests start getting 401) — send
// the browser to /login rather than surfacing a confusing error in place.
export async function apiFetch<T>(
  path: string,
  init?: RequestInit,
): Promise<T> {
  const headers = new Headers(init?.headers);
  const cluster = getCurrentCluster();
  if (cluster) {
    headers.set("X-Cluster", cluster);
  }

  const res = await fetch(`${API_BASE}${path}`, {
    ...init,
    headers,
    credentials: "include",
  });

  if (res.status === 401) {
    if (
      typeof window !== "undefined" &&
      window.location.pathname !== "/login"
    ) {
      window.location.href = "/login";
    }
    throw new ApiError(401, "unauthorized");
  }

  if (!res.ok) {
    const body = await res.text().catch(() => "");
    throw new ApiError(
      res.status,
      body || `Request to ${path} failed with status ${res.status}`,
    );
  }

  return res.json() as Promise<T>;
}
