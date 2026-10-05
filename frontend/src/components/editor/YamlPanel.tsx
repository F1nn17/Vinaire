"use client";

import { useState } from "react";
import { FillHeight } from "@/components/ui/FillHeight";
import { Toast, type ToastState } from "@/components/ui/Toast";
import { ApplyPreviewModal } from "./ApplyPreviewModal";
import { YamlEditor } from "./YamlEditor";
import { useYamlApply } from "./useYamlApply";

// A detail page's YAML tab: an editable Monaco view of the live object with
// an Apply button that runs the dry-run → diff → confirm flow (Roadmap
// Phase 25). `yaml` is the live object; `onApplied` should refetch it.
export function YamlPanel({
  yaml,
  onApplied,
}: {
  yaml: string;
  onApplied: () => void | Promise<unknown>;
}) {
  const [base, setBase] = useState(yaml);
  const [draft, setDraft] = useState(yaml);
  const [toast, setToast] = useState<ToastState>(null);
  const dirty = draft !== base;

  // Follow live updates only while there's nothing unsaved to lose.
  if (yaml !== base && !dirty) {
    setBase(yaml);
    setDraft(yaml);
  }
  const staleWhileEditing = yaml !== base && dirty;

  const apply = useYamlApply("update", () => {
    setToast({ type: "success", message: "Applied." });
    void onApplied();
  });

  function reload() {
    setBase(yaml);
    setDraft(yaml);
  }

  return (
    <div className="flex flex-col gap-3">
      <div className="flex items-center justify-between gap-3">
        <div className="text-xs text-text-muted">
          {dirty ? "Unsaved changes" : "Edit the manifest, then Apply."}
        </div>
        <div className="flex gap-2">
          <button
            type="button"
            onClick={reload}
            disabled={!dirty && !staleWhileEditing}
            className="rounded-lg border border-border bg-surface-2 px-3 py-1.5 text-[12.5px] disabled:opacity-50"
          >
            {staleWhileEditing ? "Reload from cluster" : "Reset"}
          </button>
          <button
            type="button"
            onClick={() => void apply.start(draft)}
            disabled={!dirty || apply.busy}
            className="rounded-lg bg-accent px-3.5 py-1.5 text-[12.5px] font-semibold text-white disabled:opacity-50"
          >
            {apply.busy && !apply.preview ? "Validating…" : "Apply"}
          </button>
        </div>
      </div>

      {staleWhileEditing && (
        <div className="rounded-lg border border-warn/30 bg-surface-2 p-3 text-xs text-warn">
          This resource changed on the cluster while you were editing. The
          review step shows your version against the latest one — check it
          doesn&apos;t undo someone else&apos;s change.
        </div>
      )}

      <FillHeight className="overflow-hidden rounded-[10px] border border-border">
        <YamlEditor value={draft} onChange={setDraft} height="100%" />
      </FillHeight>

      {apply.error && !apply.preview && (
        <div className="rounded-lg border border-err/30 bg-surface-2 p-3 text-xs text-err">
          {apply.error}
        </div>
      )}

      {apply.preview && (
        <ApplyPreviewModal
          result={apply.preview.result}
          mode="update"
          busy={apply.busy}
          error={apply.error}
          onConfirm={() => void apply.confirm()}
          onCancel={apply.cancel}
        />
      )}
      <Toast toast={toast} onDismiss={() => setToast(null)} />
    </div>
  );
}
