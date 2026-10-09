"use client";

import { useState } from "react";
import { t } from "@/lib/i18n/translate";
import type { InstallJob } from "@/lib/api/catalog";
import { deleteNamespace } from "@/lib/api/namespaces";
import { friendlyErrorMessage } from "@/lib/api/errors";
import { TypeToConfirmModal } from "@/components/ui/TypeToConfirmModal";
import { DeletePVCModal } from "@/components/storage/DeletePVCModal";

// Roadmap Phase 60: what a cancelled (or rolled-back) install did and what it left. The
// release is uninstalled by the backend; the namespace the install created, and any volume
// claims Helm doesn't remove, are shown here rather than deleted behind the person's back —
// the namespace can be removed with the existing, audited namespace delete (the name typed).

export function RollbackNote({ job }: { job: InstallJob }) {
  const [confirm, setConfirm] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [removed, setRemoved] = useState(false);
  // volume claims already deleted from here, and the one being deleted
  const [gone, setGone] = useState<string[]>([]);
  const [deletingClaim, setDeletingClaim] = useState<string | null>(null);

  if (!job.rolledBack && !job.rollbackError) return null;

  async function removeNamespace() {
    setBusy(true);
    setError(null);
    try {
      await deleteNamespace(job.namespace);
      setRemoved(true);
      setConfirm(false);
    } catch (e) {
      setError(friendlyErrorMessage(e));
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="flex flex-col gap-2 rounded-lg border border-border bg-surface p-3 text-xs">
      {job.rolledBack ? (
        <div className="text-ok">
          {t("The release {r} was rolled back.", { r: job.release })}
        </div>
      ) : (
        <div className="whitespace-pre-wrap text-err">
          {t("The rollback did not finish:")} {job.rollbackError}
        </div>
      )}
      {job.leftovers &&
        job.leftovers.filter((c) => !gone.includes(c)).length > 0 && (
          <div className="flex flex-col gap-1.5 text-text-muted">
            <div>
              {t(
                "Helm leaves these volume claims (your data) in place — delete the ones you don't need:",
              )}
            </div>
            <ul className="flex flex-col gap-1 font-mono">
              {job.leftovers
                .filter((c) => !gone.includes(c))
                .map((c) => (
                  <li key={c} className="flex items-center gap-3">
                    <span>{c}</span>
                    <button
                      type="button"
                      onClick={() => setDeletingClaim(c)}
                      className="rounded-md border border-err/50 px-2 py-0.5 text-[11px] text-err hover:border-err"
                    >
                      Delete
                    </button>
                  </li>
                ))}
            </ul>
          </div>
        )}
      {deletingClaim && (
        <DeletePVCModal
          namespace={job.namespace}
          name={deletingClaim}
          onClose={() => setDeletingClaim(null)}
          onDeleted={() => {
            setGone([...gone, deletingClaim]);
            setDeletingClaim(null);
          }}
        />
      )}
      {job.namespaceCreated && job.rolledBack && !removed && (
        <div className="flex flex-wrap items-center gap-3 text-text-muted">
          <span>
            {t("This install created the namespace {ns}.", {
              ns: job.namespace,
            })}
          </span>
          <button
            type="button"
            onClick={() => setConfirm(true)}
            className="rounded-lg border border-err/50 px-3 py-1.5 text-err hover:border-err"
          >
            Delete the namespace
          </button>
        </div>
      )}
      {removed && (
        <div className="text-ok">
          {t("Namespace {ns} deleted.", { ns: job.namespace })}
        </div>
      )}
      {confirm && (
        <TypeToConfirmModal
          title={t("Delete namespace {ns}", { ns: job.namespace })}
          message={t(
            "Everything inside it goes with it, including volume claims. This can't be undone.",
          )}
          expected={job.namespace}
          confirmLabel="Delete"
          busy={busy}
          error={error}
          onConfirm={() => void removeNamespace()}
          onCancel={() => setConfirm(false)}
        />
      )}
    </div>
  );
}
