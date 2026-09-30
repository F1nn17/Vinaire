"use client";

import { useState } from "react";
import { ApiError } from "@/lib/api/http";
import {
  createResource,
  dryRunResource,
  updateResource,
  type ApplyMode,
  type DryRunResult,
} from "@/lib/api/resources";

export function applyErrorMessage(error: unknown): string {
  if (error instanceof ApiError) {
    if (error.status === 403) return `Not allowed — ${error.message}`;
    if (error.status === 409) {
      return `${error.message} Reload the latest version from the cluster and re-apply your change.`;
    }
    return error.message;
  }
  return error instanceof Error ? error.message : "Something went wrong.";
}

type Preview = { yaml: string; result: DryRunResult };

// The two-step Apply flow: `start` dry-runs the manifest through the API
// server and opens a diff preview; `confirm` performs the real write. Nothing
// is persisted until `confirm`.
export function useYamlApply(mode: ApplyMode, onApplied: () => void) {
  const [preview, setPreview] = useState<Preview | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  async function start(yaml: string) {
    setBusy(true);
    setError(null);
    try {
      setPreview({ yaml, result: await dryRunResource(yaml, mode) });
    } catch (err) {
      setError(applyErrorMessage(err));
    } finally {
      setBusy(false);
    }
  }

  async function confirm() {
    if (!preview) return;
    setBusy(true);
    setError(null);
    try {
      if (mode === "create") await createResource(preview.yaml);
      else
        await updateResource(
          preview.yaml,
          preview.result.resourceVersion ?? "",
        );
      setPreview(null);
      onApplied();
    } catch (err) {
      setError(applyErrorMessage(err));
    } finally {
      setBusy(false);
    }
  }

  function cancel() {
    setPreview(null);
    setError(null);
  }

  return { preview, busy, error, start, confirm, cancel };
}
