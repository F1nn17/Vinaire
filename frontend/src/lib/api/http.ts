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
// through, so the backend base URL, JSON parsing, and error shape only
// live in one place.
export async function apiFetch<T>(path: string, init?: RequestInit): Promise<T> {
  const res = await fetch(`${API_BASE}${path}`, init);

  if (!res.ok) {
    const body = await res.text().catch(() => "");
    throw new ApiError(
      res.status,
      body || `Request to ${path} failed with status ${res.status}`,
    );
  }

  return res.json() as Promise<T>;
}
