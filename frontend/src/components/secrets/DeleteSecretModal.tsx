"use client";

import { useState } from "react";
import { t } from "@/lib/i18n/translate";
import { deleteSecret } from "@/lib/api/secrets";
import { friendlyErrorMessage } from "@/lib/api/errors";
import { TypeToConfirmModal } from "@/components/ui/TypeToConfirmModal";

// Deleting one Secret (the owner's decision of 2026-10-09): what an uninstalled chart leaves behind,
// such as a database's password. The name has to be typed; a Helm release record is refused.
export function DeleteSecretModal({
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
      await deleteSecret(namespace, name);
      onDeleted();
    } catch (e) {
      setError(friendlyErrorMessage(e));
    } finally {
      setBusy(false);
    }
  }

  return (
    <TypeToConfirmModal
      title={t("Delete Secret {name}", { name })}
      message={t(
        "Deletes the Secret {name} in namespace {ns}. Anything that still reads it (a database, an application) will stop working. This can't be undone.",
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
