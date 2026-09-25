import { getSession } from "next-auth/react";
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
// place. getSession() reads next-auth's client-side session cache (kept
// warm by the SessionProvider in components/providers/Providers.tsx)
// rather than always hitting /api/auth/session.
export async function apiFetch<T>(
  path: string,
  init?: RequestInit,
): Promise<T> {
  const session = await getSession();
  const headers = new Headers(init?.headers);
  if (session?.accessToken) {
    headers.set("Authorization", `Bearer ${session.accessToken}`);
  }
  const cluster = getCurrentCluster();
  if (cluster) {
    headers.set("X-Cluster", cluster);
  }

  const res = await fetch(`${API_BASE}${path}`, { ...init, headers });

  if (!res.ok) {
    const body = await res.text().catch(() => "");
    throw new ApiError(
      res.status,
      body || `Request to ${path} failed with status ${res.status}`,
    );
  }

  return res.json() as Promise<T>;
}
