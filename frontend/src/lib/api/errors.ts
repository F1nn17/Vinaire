import { tr } from "@/lib/i18n/translate";
import { ApiError } from "./http";

// A shared, consistent error-state message — the error-display equivalent
// of Roadmap Phase 17's shared loading/empty-state components
// (components/ui/Skeleton.tsx) — so every list/detail page shows the same
// kind of short, human copy instead of a raw backend error string (which,
// for a 403, is a full Kubernetes RBAC sentence — still included, just
// under a clear headline rather than as the whole message).
// A proxy's HTML error page or a very long dump isn't worth showing.
function isReadable(message: string): boolean {
  return (
    message.length > 0 &&
    message.length <= 400 &&
    !message.trimStart().startsWith("<")
  );
}

export function friendlyErrorMessage(error: unknown): string {
  if (error instanceof ApiError) {
    switch (error.status) {
      case 403:
        return `${tr("You don't have permission for this —")} ${error.message}`;
      case 404:
        return tr("Not found.");
      case 502:
        // The backend is up but couldn't get what it needed from somewhere else
        // (a chart repository, the cluster). Its message says what — show it
        // rather than a claim that the backend itself is down.
        return isReadable(error.message)
          ? `${tr("The backend couldn't complete this:")} ${error.message}`
          : tr("The backend couldn't complete this request.");
      case 503:
        return tr("The backend is unavailable right now.");
      default:
        return error.message;
    }
  }
  if (error instanceof Error) {
    return error.message;
  }
  return tr("Something went wrong.");
}
