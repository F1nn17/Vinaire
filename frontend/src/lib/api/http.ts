import { getCurrentCluster } from "@/lib/clusterStore";
import { API_BASE } from "./config";

export class ApiError extends Error {
  status: number;
  // the rest of the backend's JSON error body, when it had more than a message
  // (e.g. the install's list of missing permissions)
  data?: Record<string, unknown>;

  constructor(status: number, message: string, data?: Record<string, unknown>) {
    super(message);
    this.name = "ApiError";
    this.status = status;
    this.data = data;
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
      extractErrorMessage(body, res.status, path),
      extractErrorData(body),
    );
  }

  return res.json() as Promise<T>;
}

function extractErrorData(body: string): Record<string, unknown> | undefined {
  try {
    const parsed: unknown = JSON.parse(body);
    if (parsed && typeof parsed === "object" && !Array.isArray(parsed)) {
      return parsed as Record<string, unknown>;
    }
  } catch {
    // not JSON
  }
  return undefined;
}

// Every backend error response is `{"error": "<message>"}` (see
// backend/internal/api/respond.go's respondError) — extract that message
// instead of surfacing the raw `{"error":"..."}` JSON blob verbatim in the
// UI. Falls back to the raw text (or a generic message) if the body isn't
// JSON, e.g. a proxy/gateway error page.
function extractErrorMessage(
  body: string,
  status: number,
  path: string,
): string {
  if (body) {
    try {
      const parsed = JSON.parse(body) as { error?: unknown };
      if (typeof parsed.error === "string" && parsed.error) {
        return parsed.error;
      }
    } catch {
      // Not JSON — fall through to the raw text below.
    }
  }
  return body || `Request to ${path} failed with status ${status}`;
}
