"use client";

import { useState } from "react";
import { ApplyPreviewModal } from "./ApplyPreviewModal";
import { YamlEditor } from "./YamlEditor";
import { useYamlApply } from "./useYamlApply";
import type { ResourceTemplate } from "./templates";

// The "+ Create" flow (Roadmap Phase 25): an empty editor pre-filled with a
// starter manifest, through the same dry-run → diff → apply flow as editing.
// Mounted only while open (parents render `{open && <CreateResourceModal/>}`),
// so every open starts from a fresh template.
export function CreateResourceModal({
  templates,
  onClose,
  onCreated,
}: {
  templates: ResourceTemplate[];
  onClose: () => void;
  onCreated: () => void;
}) {
  const [templateIndex, setTemplateIndex] = useState(0);
  const [draft, setDraft] = useState(templates[0]?.yaml ?? "");

  const apply = useYamlApply("create", () => {
    onCreated();
    onClose();
  });

  function pickTemplate(index: number) {
    setTemplateIndex(index);
    setDraft(templates[index].yaml);
  }

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 p-4">
      <div className="flex w-full max-w-3xl flex-col rounded-xl border border-border bg-surface p-5">
        <div className="flex items-center justify-between gap-3">
          <div className="text-sm font-semibold">Create resource</div>
          {templates.length > 1 && (
            <label className="flex items-center gap-2 text-xs text-text-muted">
              Template
              <select
                value={templateIndex}
                onChange={(e) => pickTemplate(Number(e.target.value))}
                className="rounded-lg border border-border bg-surface-2 px-2 py-1 text-xs"
              >
                {templates.map((t, i) => (
                  <option key={t.label} value={i}>
                    {t.label}
                  </option>
                ))}
              </select>
            </label>
          )}
        </div>
        <div className="mt-1 text-xs text-text-faint">
          The cluster validates the manifest (dry-run) before anything is
          created.
        </div>

        <div className="mt-3 overflow-hidden rounded-lg border border-border">
          <YamlEditor value={draft} onChange={setDraft} height="420px" />
        </div>

        {apply.error && !apply.preview && (
          <div className="mt-3 rounded-lg border border-err/30 bg-surface-2 p-3 text-xs text-err">
            {apply.error}
          </div>
        )}

        <div className="mt-4 flex justify-end gap-2.5">
          <button
            type="button"
            onClick={onClose}
            className="rounded-lg border border-border bg-surface-2 px-3.5 py-2 text-[12.5px]"
          >
            Cancel
          </button>
          <button
            type="button"
            onClick={() => void apply.start(draft)}
            disabled={apply.busy || !draft.trim()}
            className="rounded-lg bg-accent px-3.5 py-2 text-[12.5px] font-semibold text-white disabled:opacity-60"
          >
            {apply.busy && !apply.preview ? "Validating…" : "Review & create"}
          </button>
        </div>
      </div>

      {apply.preview && (
        <ApplyPreviewModal
          result={apply.preview.result}
          mode="create"
          busy={apply.busy}
          error={apply.error}
          onConfirm={() => void apply.confirm()}
          onCancel={apply.cancel}
        />
      )}
    </div>
  );
}
