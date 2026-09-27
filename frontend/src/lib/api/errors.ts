import { ApiError } from "./http";

// A shared, consistent error-state message — the error-display equivalent
// of Roadmap Phase 17's shared loading/empty-state components
// (components/ui/Skeleton.tsx) — so every list/detail page shows the same
// kind of short, human copy instead of a raw backend error string (which,
// for a 403, is a full Kubernetes RBAC sentence — still included, just
// under a clear headline rather than as the whole message).
export function friendlyErrorMessage(error: unknown): string {
  if (error instanceof ApiError) {
    switch (error.status) {
      case 403:
        return `You don't have permission to view this — ${error.message}`;
      case 404:
        return "Not found.";
      case 502:
      case 503:
        return "The backend is unavailable right now.";
      default:
        return error.message;
    }
  }
  if (error instanceof Error) {
    return error.message;
  }
  return "Something went wrong.";
}
