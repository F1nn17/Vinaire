"use client";

import { useNodes } from "@/lib/hooks/useWorkloads";
import type { FormFieldSchema, FormSchema } from "@/lib/api/forms";
import type { FormState, KV } from "./converters";

const inputClass =
  "w-full rounded-lg border border-border bg-surface-2 px-3 py-1.75 font-mono text-xs outline-none placeholder:text-text-faint";

// Renders a FormSchema's fields (Roadmap Phase 31). State is a flat
// key -> string | key/value rows map, owned by the parent so it can survive a
// Form <-> YAML toggle.
export function ResourceForm({
  schema,
  state,
  onChange,
}: {
  schema: FormSchema;
  state: FormState;
  onChange: (next: FormState) => void;
}) {
  const set = (key: string, value: FormState[string]) =>
    onChange({ ...state, [key]: value });

  // Fields sharing a `group` render together under one heading.
  const groups: { name: string; fields: FormFieldSchema[] }[] = [];
  for (const f of schema.fields) {
    const name = f.group ?? "";
    const last = groups[groups.length - 1];
    if (last && last.name === name && name !== "") last.fields.push(f);
    else groups.push({ name, fields: [f] });
  }

  return (
    <div className="flex max-h-[460px] flex-col gap-4 overflow-auto p-1 pr-2">
      {groups.map((g, i) => (
        <div key={i} className="flex flex-col gap-3">
          {g.name && (
            <div className="text-xs font-semibold text-text-muted">
              {g.name}
            </div>
          )}
          <div
            className={
              g.name ? "grid grid-cols-2 gap-3" : "flex flex-col gap-3"
            }
          >
            {g.fields.map((f) => (
              <Field key={f.key} field={f} state={state} set={set} />
            ))}
          </div>
        </div>
      ))}
    </div>
  );
}

function Field({
  field,
  state,
  set,
}: {
  field: FormFieldSchema;
  state: FormState;
  set: (key: string, value: FormState[string]) => void;
}) {
  const value = state[field.key];
  return (
    <div className="flex flex-col gap-1.5">
      <label className="text-xs text-text-muted" htmlFor={`f-${field.key}`}>
        {field.label}
        {field.required && <span className="text-err"> *</span>}
      </label>
      {field.type === "keyvalue" ? (
        <KeyValueEditor
          rows={Array.isArray(value) ? value : []}
          secret={field.secret}
          keyPlaceholder={field.keyPlaceholder}
          valuePlaceholder={field.valuePlaceholder}
          onChange={(rows) => set(field.key, rows)}
        />
      ) : field.type === "select" ? (
        <select
          id={`f-${field.key}`}
          value={typeof value === "string" ? value : ""}
          onChange={(e) => set(field.key, e.target.value)}
          className={inputClass}
        >
          {(field.options ?? []).map((o) => (
            <option key={o} value={o}>
              {o}
            </option>
          ))}
        </select>
      ) : field.type === "node" ? (
        <NodeSelect
          id={`f-${field.key}`}
          value={typeof value === "string" ? value : ""}
          onChange={(v) => set(field.key, v)}
        />
      ) : (
        <input
          id={`f-${field.key}`}
          type={field.type === "number" ? "number" : "text"}
          min={field.type === "number" ? 0 : undefined}
          value={typeof value === "string" ? value : ""}
          placeholder={field.placeholder}
          onChange={(e) => set(field.key, e.target.value)}
          className={inputClass}
        />
      )}
      {field.help && (
        <div className="text-[11px] text-text-faint">{field.help}</div>
      )}
    </div>
  );
}

function NodeSelect({
  id,
  value,
  onChange,
}: {
  id: string;
  value: string;
  onChange: (v: string) => void;
}) {
  const { data: nodes } = useNodes();
  const names = (nodes ?? []).map((n) => n.name);
  // Keep a value the form was given (e.g. from YAML) selectable even if the
  // node list hasn't loaded or doesn't contain it.
  if (value && !names.includes(value)) names.push(value);
  return (
    <select
      id={id}
      value={value}
      onChange={(e) => onChange(e.target.value)}
      className={inputClass}
    >
      <option value="">Any node</option>
      {names.map((n) => (
        <option key={n} value={n}>
          {n}
        </option>
      ))}
    </select>
  );
}

function KeyValueEditor({
  rows,
  secret,
  keyPlaceholder = "KEY",
  valuePlaceholder = "value",
  onChange,
}: {
  rows: KV[];
  secret?: boolean;
  keyPlaceholder?: string;
  valuePlaceholder?: string;
  onChange: (rows: KV[]) => void;
}) {
  const update = (i: number, patch: Partial<KV>) =>
    onChange(rows.map((r, j) => (j === i ? { ...r, ...patch } : r)));
  return (
    <div className="flex flex-col gap-2">
      {rows.map((r, i) => (
        <div key={i} className="flex gap-2">
          <input
            aria-label="Key"
            value={r.key}
            placeholder={keyPlaceholder}
            onChange={(e) => update(i, { key: e.target.value })}
            className={inputClass}
          />
          <input
            aria-label="Value"
            type={secret ? "password" : "text"}
            value={r.value}
            placeholder={valuePlaceholder}
            onChange={(e) => update(i, { value: e.target.value })}
            className={inputClass}
          />
          <button
            type="button"
            aria-label="Remove row"
            onClick={() => onChange(rows.filter((_, j) => j !== i))}
            className="rounded-lg border border-border bg-surface-2 px-2.5 text-xs text-text-muted"
          >
            ✕
          </button>
        </div>
      ))}
      <button
        type="button"
        onClick={() => onChange([...rows, { key: "", value: "" }])}
        className="w-fit rounded-lg border border-border bg-surface-2 px-3 py-1.5 text-xs"
      >
        + Add
      </button>
    </div>
  );
}
