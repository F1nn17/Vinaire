"use client";

import { useState } from "react";
import { PillTabs } from "@/components/ui/Tabs";
import { useFormSchema } from "@/lib/api/forms";
import { ApplyPreviewModal } from "./ApplyPreviewModal";
import { YamlEditor } from "./YamlEditor";
import { useYamlApply } from "./useYamlApply";
import { ResourceForm } from "./forms/ResourceForm";
import { AppPicker, hasAppPicker } from "./forms/AppPicker";
import {
  defaultFormState,
  formToYaml,
  validateForm,
  yamlToForm,
  type FormState,
} from "./forms/converters";
import type { ResourceTemplate } from "./templates";

type Mode = "form" | "yaml";
const modeTabs: { key: Mode; label: string }[] = [
  { key: "form", label: "Form" },
  { key: "yaml", label: "YAML" },
];

// The "+ Create" flow (Roadmap Phase 25/31): a form — for the kinds that have
// one — or an empty editor pre-filled with a starter manifest, both feeding
// the same dry-run -> diff -> apply flow. Mounted only while open (parents
// render `{open && <CreateResourceModal/>}`), so every open starts fresh.
export function CreateResourceModal({
  templates,
  onClose,
  onCreated,
}: {
  templates: ResourceTemplate[];
  onClose: () => void;
  onCreated: () => void;
}) {
  const first = templates[0];
  const [templateIndex, setTemplateIndex] = useState(0);
  const [mode, setMode] = useState<Mode>(first?.formType ? "form" : "yaml");
  const [formState, setFormState] = useState<FormState>(() =>
    first?.formType ? defaultFormState(first.formType) : {},
  );
  const [draft, setDraft] = useState(first?.yaml ?? "");
  const [toggleError, setToggleError] = useState<string | null>(null);

  const template = templates[templateIndex];
  const formType = template?.formType;
  const { data: schema, error: schemaError } = useFormSchema(
    formType && mode === "form" ? formType : null,
  );

  const apply = useYamlApply("create", () => {
    onCreated();
    onClose();
  });

  const validation =
    mode === "form" && formType ? validateForm(formType, formState) : null;
  const currentYaml =
    mode === "form" && formType ? formToYaml(formType, formState) : draft;

  function pickTemplate(index: number) {
    const t = templates[index];
    setTemplateIndex(index);
    setDraft(t.yaml);
    setFormState(t.formType ? defaultFormState(t.formType) : {});
    setMode(t.formType ? "form" : "yaml");
    setToggleError(null);
  }

  function switchMode(next: Mode) {
    if (!formType || next === mode) return;
    setToggleError(null);
    if (next === "yaml") {
      setDraft(formToYaml(formType, formState));
      setMode("yaml");
      return;
    }
    const result = yamlToForm(formType, draft);
    if (!result.ok) {
      // Stay on YAML and leave the form's own state untouched.
      setToggleError(result.reason);
      return;
    }
    setFormState(result.state);
    setMode("form");
  }

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 p-4">
      <div className="flex w-full max-w-3xl flex-col rounded-xl border border-border bg-surface p-5">
        <div className="flex items-center justify-between gap-3">
          <div className="text-sm font-semibold">Create resource</div>
          <div className="flex items-center gap-3">
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
            {formType && (
              <PillTabs items={modeTabs} active={mode} onChange={switchMode} />
            )}
          </div>
        </div>
        <div className="mt-1 text-xs text-text-faint">
          The cluster validates the manifest (dry-run) before anything is
          created.
        </div>

        {toggleError && (
          <div className="mt-3 rounded-lg border border-warn/30 bg-surface-2 p-3 text-xs text-warn">
            {toggleError}
          </div>
        )}

        <div className="mt-3">
          {mode === "form" && formType ? (
            schemaError ? (
              <div className="text-sm text-err">
                Couldn&apos;t load the form — switch to YAML.
              </div>
            ) : !schema ? (
              <div className="p-6 text-sm text-text-muted">Loading form…</div>
            ) : (
              <>
                {hasAppPicker(formType) && (
                  <AppPicker
                    key={formType}
                    formType={formType}
                    state={formState}
                    onChange={setFormState}
                  />
                )}
                <ResourceForm
                  schema={schema}
                  state={formState}
                  onChange={setFormState}
                />
              </>
            )
          ) : (
            <div className="overflow-hidden rounded-lg border border-border">
              <YamlEditor value={draft} onChange={setDraft} height="420px" />
            </div>
          )}
        </div>

        {apply.error && !apply.preview && (
          <div className="mt-3 rounded-lg border border-err/30 bg-surface-2 p-3 text-xs text-err">
            {apply.error}
          </div>
        )}

        <div className="mt-4 flex items-center justify-between gap-2.5">
          <div className="text-xs text-text-faint">{validation}</div>
          <div className="flex gap-2.5">
            <button
              type="button"
              onClick={onClose}
              className="rounded-lg border border-border bg-surface-2 px-3.5 py-2 text-[12.5px]"
            >
              Cancel
            </button>
            <button
              type="button"
              onClick={() => void apply.start(currentYaml)}
              disabled={
                apply.busy || !currentYaml.trim() || validation !== null
              }
              className="rounded-lg bg-accent px-3.5 py-2 text-[12.5px] font-semibold text-white disabled:opacity-60"
            >
              {apply.busy && !apply.preview ? "Validating…" : "Review & create"}
            </button>
          </div>
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
