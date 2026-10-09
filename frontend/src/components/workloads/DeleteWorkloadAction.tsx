"use client";

import { useState } from "react";
import { useRouter } from "next/navigation";
import { t } from "@/lib/i18n/translate";
import { ActionButton } from "@/components/ui/ActionBar";
import { ConfirmModal } from "@/components/ui/ConfirmModal";
import { TrashIcon } from "@/components/icons";
import { friendlyErrorMessage } from "@/lib/api/errors";
import {
  deleteDaemonSet,
  deleteJob,
  deleteStatefulSet,
} from "@/lib/api/workloadDelete";

// The Delete button of a StatefulSet, DaemonSet or Job page (the owner's decision of 2026-10-09,
// next to the multi-select in the lists): a plain delete as the caller, with its pods removed in
// the background, then back to the list.
const info = {
  statefulsets: {
    label: "StatefulSet",
    run: deleteStatefulSet,
    note: "Its pods are deleted too; the volume claims it created stay. This can't be undone.",
  },
  daemonsets: {
    label: "DaemonSet",
    run: deleteDaemonSet,
    note: "Its pods on every node are deleted too. This can't be undone.",
  },
  jobs: {
    label: "Job",
    run: deleteJob,
    note: "Its pods and their logs are deleted too. This can't be undone.",
  },
} as const;

export function DeleteWorkloadAction({
  kind,
  namespace,
  name,
}: {
  kind: keyof typeof info;
  namespace: string;
  name: string;
}) {
  const router = useRouter();
  const [open, setOpen] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const i = info[kind];

  async function confirm() {
    setBusy(true);
    setError(null);
    try {
      await i.run(namespace, name);
      router.push("/workloads");
    } catch (e) {
      setError(friendlyErrorMessage(e));
    } finally {
      setBusy(false);
    }
  }

  return (
    <>
      <ActionButton
        icon={<TrashIcon />}
        label="Delete"
        hint={`Delete the ${i.label} and its pods — can't be undone`}
        danger
        onClick={() => {
          setError(null);
          setOpen(true);
        }}
      />
      <ConfirmModal
        open={open}
        title={t("Delete {kind} {name}", { kind: i.label, name })}
        message={error ? `${t(i.note)}\n\n${error}` : t(i.note)}
        confirmLabel="Delete"
        danger
        busy={busy}
        onConfirm={() => void confirm()}
        onCancel={() => setOpen(false)}
      />
    </>
  );
}
