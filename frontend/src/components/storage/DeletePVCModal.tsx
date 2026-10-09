"use client";

import { useState } from "react";
import { t } from "@/lib/i18n/translate";
import { deletePVC } from "@/lib/api/storage";
import { friendlyErrorMessage } from "@/lib/api/errors";
import { TypeToConfirmModal } from "@/components/ui/TypeToConfirmModal";

// Deleting one PersistentVolumeClaim (the owner's decision of 2026-10-09): irreversible — with the
// usual "Delete" reclaim policy the data goes with the claim — so the claim's name has to be typed,
// and the backend refuses while a pod still mounts it (and says which).
export function DeletePVCModal({
  namespace,
  name,
  onClose,
  onDeleted,
}: {
  namespace: string;
  name: string;
  onClose: () => void;
  onDeleted: () => void;
}) {
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  async function remove() {
    setBusy(true);
    setError(null);
    try {
      await deletePVC(namespace, name);
      onDeleted();
    } catch (e) {
      setError(friendlyErrorMessage(e));
    } finally {
      setBusy(false);
    }
  }

  return (
    <TypeToConfirmModal
      title={t("Delete volume claim {name}", { name })}
      message={t(
        "Deletes the claim {name} in namespace {ns}. With the usual “Delete” reclaim policy, the data on the volume is deleted with it. This can't be undone.",
        { name, ns: namespace },
      )}
      expected={name}
      confirmLabel="Delete"
      busy={busy}
      error={error}
      onConfirm={() => void remove()}
      onCancel={onClose}
    />
  );
}
