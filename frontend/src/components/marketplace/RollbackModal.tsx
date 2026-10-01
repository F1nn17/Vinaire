"use client";

import { useState } from "react";
import { rollbackRelease, type HelmRelease } from "@/lib/api/catalog";
import { useReleaseHistory } from "@/lib/hooks/useCatalog";
import { friendlyErrorMessage } from "@/lib/api/errors";
import { applyErrorMessage } from "@/components/editor/useYamlApply";

// Roll a release back to an earlier revision. Helm records the rollback as a
// new revision with the old one's content, so history is never rewritten.
export function RollbackModal({
  release,
  onClose,
  onDone,
}: {
  release: HelmRelease;
  onClose: () => void;
  onDone: () => void;
}) {
  const { data, error: loadError } = useReleaseHistory(
    release.namespace,
    release.name,
  );
  const [revision, setRevision] = useState<number | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [done, setDone] = useState(false);

  const earlier = (data ?? []).filter((r) => r.revision !== release.revision);

  async function submit() {
    if (revision === null) return;
    setBusy(true);
    setError(null);
    try {
      await rollbackRelease(release.namespace, release.name, revision);
      setDone(true);
      onDone();
    } catch (err) {
      setError(applyErrorMessage(err));
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 p-4">
      <div className="flex max-h-[92vh] w-full max-w-lg flex-col rounded-xl border border-border bg-surface p-5">
        <div className="text-sm font-semibold">Roll back {release.name}</div>
        <div className="mt-1 text-xs text-text-muted">
          Currently at revision {release.revision}. Pick the revision to return
          to.
        </div>

        <div className="mt-4 flex flex-col gap-1.5 overflow-auto">
          {loadError ? (
            <div className="text-xs text-err">
              Couldn&apos;t load the history: {friendlyErrorMessage(loadError)}
            </div>
          ) : !data ? (
            <div className="text-xs text-text-muted">Loading history…</div>
          ) : earlier.length === 0 ? (
            <div className="text-xs text-text-faint">
              There is no earlier revision.
            </div>
          ) : (
            earlier.map((r) => (
              <label
                key={r.revision}
                className={`flex cursor-pointer items-start gap-2.5 rounded-lg border px-3 py-2 text-xs ${
                  revision === r.revision
                    ? "border-accent bg-surface-2"
                    : "border-border"
                }`}
              >
                <input
                  type="radio"
                  name="revision"
                  checked={revision === r.revision}
                  onChange={() => setRevision(r.revision)}
                  className="mt-0.5"
                />
                <span className="flex flex-col gap-0.5">
                  <span className="font-mono">
                    #{r.revision} · {r.chart}-{r.chartVersion} · {r.status}
                  </span>
                  <span className="text-text-faint">
                    {new Date(r.updated).toLocaleString()}
                    {r.description ? ` — ${r.description}` : ""}
                  </span>
                </span>
              </label>
            ))
          )}
        </div>

        {error && (
          <div className="mt-3 whitespace-pre-wrap rounded-lg border border-err/30 bg-surface-2 p-3 text-xs text-err">
            {error}
          </div>
        )}
        {done && (
          <div className="mt-3 rounded-lg border border-ok/30 bg-surface-2 p-3 text-xs text-ok">
            Rolled back to revision {revision}.
          </div>
        )}

        <div className="mt-4 flex justify-end gap-2.5">
          <button
            type="button"
            onClick={onClose}
            disabled={busy}
            className="rounded-lg border border-border bg-surface-2 px-3.5 py-2 text-[12.5px] disabled:opacity-60"
          >
            {done ? "Close" : "Cancel"}
          </button>
          {!done && (
            <button
              type="button"
              onClick={() => void submit()}
              disabled={busy || revision === null}
              className="rounded-lg bg-accent px-3.5 py-2 text-[12.5px] font-semibold text-white disabled:opacity-60"
            >
              {busy ? "Rolling back…" : "Roll back"}
            </button>
          )}
        </div>
      </div>
    </div>
  );
}
