"use client";

import { t } from "@/lib/i18n/translate";
import { useMemo, useState } from "react";
import { parse } from "yaml";
import {
  applyValue,
  buildSections,
  defaultOf,
  fromInput,
  overrideOf,
  parseValuesText,
  toInput,
  type Field,
} from "@/lib/values/schemaForm";

const inputClass =
  "w-full rounded-lg border border-border bg-surface-2 px-3 py-1.5 font-mono text-xs outline-none placeholder:text-text-faint";

// A form over the chart's values.schema.json. It edits the same YAML the "Your
// values" tab shows (so the two views never disagree), writes only fields the user
// changed, and shows the chart's default as the placeholder.
export function ValuesForm({
  schemaText,
  defaultsText,
  valuesText,
  onChange,
}: {
  schemaText: string;
  defaultsText: string;
  valuesText: string;
  onChange: (next: string) => void;
}) {
  const sections = useMemo(() => buildSections(schemaText), [schemaText]);
  const defaults = useMemo(() => {
    try {
      return parse(defaultsText) as unknown;
    } catch {
      return {};
    }
  }, [defaultsText]);
  const [find, setFind] = useState("");
  const { doc, error } = useMemo(
    () => parseValuesText(valuesText),
    [valuesText],
  );

  const total = sections.reduce((n, s) => n + s.fields.length, 0);
  const q = find.trim().toLowerCase();
  const shown = sections
    .map((s) => ({
      ...s,
      fields: q
        ? s.fields.filter(
            (f) =>
              f.path.join(".").toLowerCase().includes(q) ||
              (f.description ?? "").toLowerCase().includes(q),
          )
        : s.fields,
    }))
    .filter((s) => s.fields.length > 0);
  const changed = sections.reduce(
    (n, s) =>
      n + s.fields.filter((f) => overrideOf(doc, f) !== undefined).length,
    0,
  );

  if (error) {
    return (
      <div className="rounded-lg border border-warn/30 bg-surface-2 p-4 text-xs text-warn">
        The values YAML has an error, so the form can&apos;t read it: {error}{" "}
        Fix it on the &ldquo;Your values&rdquo; tab first.
      </div>
    );
  }

  return (
    <div className="flex h-full flex-col gap-3">
      <div className="flex items-center gap-3">
        <input
          type="text"
          aria-label="Find a setting"
          value={find}
          onChange={(e) => setFind(e.target.value)}
          placeholder={t("Find among {n} settings…", { n: total })}
          className="w-72 rounded-lg border border-border bg-surface px-3 py-1.5 text-xs outline-none placeholder:text-text-faint"
        />
        <span className="text-xs text-text-faint">
          {changed} changed · only changed settings are written to your values
        </span>
      </div>
      <div className="min-h-0 flex-1 overflow-auto pr-1">
        {shown.length === 0 ? (
          <div className="text-xs text-text-faint">No setting matches.</div>
        ) : (
          shown.map((s, i) => (
            <details
              key={s.name}
              open={q !== "" || i === 0}
              className="mb-2 rounded-lg border border-border bg-surface"
            >
              <summary className="cursor-pointer select-none px-3 py-2 text-[12.5px] font-semibold">
                {s.name}{" "}
                <span className="font-normal text-text-faint">
                  ({s.fields.length})
                </span>
              </summary>
              <div className="flex flex-col gap-3.5 border-t border-border p-3">
                {s.fields.map((f) => (
                  <FieldRow
                    key={f.path.join(".")}
                    field={f}
                    override={overrideOf(doc, f)}
                    fallback={defaultOf(defaults, f)}
                    onSet={(v) => onChange(applyValue(valuesText, f, v))}
                  />
                ))}
              </div>
            </details>
          ))
        )}
      </div>
    </div>
  );
}

function FieldRow({
  field,
  override,
  fallback,
  onSet,
}: {
  field: Field;
  override: unknown;
  fallback: unknown;
  onSet: (v: unknown) => void;
}) {
  // what's typed stays as typed while the box has focus ("1," in a list is not yet
  // a value), and only valid input is applied
  const [draft, setDraft] = useState<string | null>(null);
  const [err, setErr] = useState<string | null>(null);
  const isSet = override !== undefined;
  const shownText = draft ?? toInput(field, override);
  const hint =
    fallback === undefined || fallback === null || fallback === ""
      ? ""
      : `default: ${toInput(field, fallback)}`;

  function edit(text: string) {
    setDraft(text);
    const v = fromInput(field, text);
    if (v instanceof Error) {
      setErr(`Needs ${v.message}`);
      return;
    }
    setErr(null);
    onSet(v);
  }
  const done = () => {
    setDraft(null);
    setErr(null);
  };

  let control: React.ReactNode;
  if (field.kind === "boolean") {
    control = (
      <select
        id={`vf-${field.path.join(".")}`}
        value={isSet ? String(override) : ""}
        onChange={(e) =>
          onSet(e.target.value === "" ? undefined : e.target.value === "true")
        }
        className={inputClass}
      >
        <option value="">{hint ? `(${hint})` : "(not set)"}</option>
        <option value="true">true</option>
        <option value="false">false</option>
      </select>
    );
  } else if (field.kind === "enum") {
    control = (
      <select
        id={`vf-${field.path.join(".")}`}
        value={isSet ? String(override) : ""}
        onChange={(e) => {
          const picked = field.enumValues?.find(
            (x) => String(x) === e.target.value,
          );
          onSet(e.target.value === "" ? undefined : picked);
        }}
        className={inputClass}
      >
        <option value="">{hint ? `(${hint})` : "(not set)"}</option>
        {field.enumValues?.map((x) => (
          <option key={String(x)} value={String(x)}>
            {String(x)}
          </option>
        ))}
      </select>
    );
  } else if (field.kind === "yaml") {
    control = (
      <textarea
        id={`vf-${field.path.join(".")}`}
        rows={Math.min(8, Math.max(2, shownText.split("\n").length))}
        value={shownText}
        placeholder={hint || "YAML"}
        onChange={(e) => edit(e.target.value)}
        onBlur={done}
        spellCheck={false}
        className={inputClass}
      />
    );
  } else {
    control = (
      <input
        id={`vf-${field.path.join(".")}`}
        type={field.secretLike && field.kind === "string" ? "password" : "text"}
        autoComplete="off"
        value={shownText}
        placeholder={hint || (field.kind === "list" ? "a, b, c" : "")}
        onChange={(e) => edit(e.target.value)}
        onBlur={done}
        className={inputClass}
      />
    );
  }

  return (
    <div className="flex flex-col gap-1">
      <div className="flex items-baseline justify-between gap-2">
        <label
          htmlFor={`vf-${field.path.join(".")}`}
          className="font-mono text-[11.5px] text-text"
        >
          {field.path.join(".")}
          {field.required && <span className="text-err"> *</span>}
        </label>
        {isSet && (
          <button
            type="button"
            onClick={() => {
              done();
              onSet(undefined);
            }}
            className="text-[11px] text-text-faint hover:text-accent-text"
          >
            reset to default
          </button>
        )}
      </div>
      {control}
      {err && <div className="text-[11px] text-err">{err}</div>}
      {field.description && (
        <div
          className="line-clamp-3 text-[11px] text-text-faint"
          title={field.description}
        >
          {field.description}
        </div>
      )}
    </div>
  );
}
