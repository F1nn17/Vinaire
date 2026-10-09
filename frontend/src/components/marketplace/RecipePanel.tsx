"use client";

import { useState } from "react";
import { t } from "@/lib/i18n/translate";
import {
  getResourceObject,
  useResourceTable,
  type TableRow,
} from "@/lib/api/allResources";
import {
  applyOption,
  defaultFields,
  fieldsError,
  type FieldValues,
  type Recipe,
  type RecipeField,
  type RecipeOption,
  type RecipeSection,
} from "@/lib/values/recipe";
import { backendFromService } from "@/components/editor/forms/fromApp";
import { FieldChecks } from "./FieldChecks";

// Roadmap Phase 59: the questions a recipe asks before an install — which database, how to reach
// the app — as a few choices and fields that become values in the "Your values" YAML. Nothing is
// written to the cluster here: it only edits the values text the person can still read and change.
// A password is never typed: a field of kind "secret" names an existing Secret.

const input =
  "rounded-lg border border-border bg-surface-2 px-2.5 py-1.5 font-mono text-xs outline-none placeholder:text-text-faint";

type Applied = { optionId: string; paths: string[][] };

export function RecipePanel({
  recipe,
  installNamespace,
  values,
  onValues,
}: {
  recipe: Recipe;
  installNamespace: string;
  values: string;
  onValues: (next: string) => void;
}) {
  // per section: what is applied to the values now (so choosing another option can take it back)
  const [applied, setApplied] = useState<Record<string, Applied>>({});
  const [selected, setSelected] = useState<Record<string, string>>(() =>
    Object.fromEntries(
      recipe.sections.map((s) => [
        s.id,
        s.options.find((o) => o.default)?.id ?? s.options[0].id,
      ]),
    ),
  );
  const [fields, setFields] = useState<Record<string, FieldValues>>({});

  function fieldsOf(section: RecipeSection, option: RecipeOption) {
    return fields[`${section.id}/${option.id}`] ?? defaultFields(option);
  }

  function apply(section: RecipeSection, option: RecipeOption) {
    const prev = applied[section.id]?.paths ?? [];
    const r = applyOption(values, prev, option, fieldsOf(section, option));
    onValues(r.text);
    setApplied({
      ...applied,
      [section.id]: { optionId: option.id, paths: r.paths },
    });
  }

  function choose(section: RecipeSection, option: RecipeOption) {
    setSelected({ ...selected, [section.id]: option.id });
    // an option with nothing to type is applied at once (which also takes the previous one back)
    if ((option.fields ?? []).length === 0) {
      const prev = applied[section.id]?.paths ?? [];
      const r = applyOption(values, prev, option, {});
      onValues(r.text);
      setApplied({
        ...applied,
        [section.id]: { optionId: option.id, paths: r.paths },
      });
    }
  }

  return (
    <div className="flex flex-col gap-4 rounded-lg border border-accent/40 bg-surface p-4 text-xs">
      <div>
        <div className="text-[13px] font-semibold">{t("Guided setup")}</div>
        {recipe.summary && (
          <div className="mt-1 text-text-muted">{recipe.summary}</div>
        )}
      </div>

      {recipe.guide && recipe.guide.length > 0 && (
        <details className="rounded-lg border border-border bg-surface-2 p-3">
          <summary className="cursor-pointer text-text-muted">
            {t("How this install works")}
          </summary>
          <div className="mt-2 flex flex-col gap-2 text-text-muted">
            {recipe.guide.map((p, i) => (
              <p key={i}>{p}</p>
            ))}
          </div>
        </details>
      )}

      {recipe.sections.map((section) => {
        const option =
          section.options.find((o) => o.id === selected[section.id]) ??
          section.options[0];
        const isApplied = applied[section.id]?.optionId === option.id;
        const f = fieldsOf(section, option);
        const err = fieldsError(option, f);
        return (
          <div key={section.id} className="flex flex-col gap-2">
            <div className="font-semibold">{section.title}</div>
            {section.help && (
              <div className="text-text-faint">{section.help}</div>
            )}
            <div className="flex flex-col gap-1.5">
              {section.options.map((o) => (
                <label
                  key={o.id}
                  className="flex items-start gap-2 rounded-lg border border-border bg-surface-2 px-3 py-2"
                >
                  <input
                    type="radio"
                    name={`recipe-${section.id}`}
                    checked={option.id === o.id}
                    onChange={() => choose(section, o)}
                    className="mt-0.5"
                  />
                  <span className="flex flex-col gap-0.5">
                    <span>{o.title}</span>
                    {o.note && (
                      <span className="text-text-faint">{o.note}</span>
                    )}
                  </span>
                </label>
              ))}
            </div>

            {(option.fields ?? []).length > 0 && (
              <div className="flex flex-col gap-2 rounded-lg border border-border p-3">
                {section.discover === "postgres" && (
                  <PostgresFinder
                    onPick={(host, port) =>
                      setFields({
                        ...fields,
                        [`${section.id}/${option.id}`]: {
                          ...f,
                          host,
                          ...(port ? { port } : {}),
                        },
                      })
                    }
                  />
                )}
                {option.fields!.map((fld) => (
                  <FieldInput
                    key={fld.key}
                    field={fld}
                    value={f[fld.key] ?? ""}
                    namespace={installNamespace}
                    onChange={(v) =>
                      setFields({
                        ...fields,
                        [`${section.id}/${option.id}`]: { ...f, [fld.key]: v },
                      })
                    }
                  />
                ))}
                <FieldChecks
                  option={option}
                  fields={f}
                  namespace={installNamespace}
                />
                <div className="flex items-center gap-3">
                  <button
                    type="button"
                    disabled={!!err}
                    onClick={() => apply(section, option)}
                    className="rounded-lg bg-accent px-3 py-1.5 font-semibold text-white disabled:opacity-50"
                  >
                    {isApplied
                      ? t("Update the values")
                      : t("Put into the values")}
                  </button>
                  {err ? (
                    <span className="text-warn">{err}</span>
                  ) : isApplied ? (
                    <span className="text-ok">
                      {t("Applied to “Your values”.")}
                    </span>
                  ) : null}
                </div>
              </div>
            )}
          </div>
        );
      })}
    </div>
  );
}

function FieldInput({
  field,
  value,
  namespace,
  onChange,
}: {
  field: RecipeField;
  value: string;
  namespace: string;
  onChange: (v: string) => void;
}) {
  const secrets = useResourceTable(
    field.kind === "secret" && namespace ? "core/v1/secrets" : null,
    namespace,
  );
  const classes = useResourceTable(
    field.kind === "ingressclass"
      ? "networking.k8s.io/v1/ingressclasses"
      : null,
    "",
  );
  const listId = `recipe-list-${field.key}`;
  const options =
    field.kind === "secret"
      ? (secrets.data?.rows ?? []).map((r) => r.name)
      : field.kind === "ingressclass"
        ? (classes.data?.rows ?? []).map((r) => r.name)
        : [];
  return (
    <label className="flex flex-col gap-1">
      <span className="text-text-muted">
        {field.label}
        {field.required ? " *" : ""}
      </span>
      <input
        value={value}
        list={options.length > 0 ? listId : undefined}
        placeholder={field.placeholder}
        inputMode={field.kind === "number" ? "numeric" : undefined}
        onChange={(e) => onChange(e.target.value)}
        className={input}
      />
      {options.length > 0 && (
        <datalist id={listId}>
          {options.map((o) => (
            <option key={o} value={o} />
          ))}
        </datalist>
      )}
      {field.help && <span className="text-text-faint">{field.help}</span>}
    </label>
  );
}

// "Found in this cluster": Services that look like a PostgreSQL (the common Helm chart and
// CloudNativePG labels). Read as the signed-in user; picking one fills the host and port.
function PostgresFinder({
  onPick,
}: {
  onPick: (host: string, port: string) => void;
}) {
  const a = useResourceTable(
    "core/v1/services",
    "",
    "app.kubernetes.io/name=postgresql",
  );
  const b = useResourceTable("core/v1/services", "", "cnpg.io/cluster");
  const rows: TableRow[] = [...(a.data?.rows ?? []), ...(b.data?.rows ?? [])]
    // headless and read-only/replica Services aren't where an app should connect
    .filter((r) => !/(-headless|-hl|-ro|-r|-metrics)$/.test(r.name));
  if (rows.length === 0) return null;

  async function pick(value: string) {
    if (!value) return;
    const [namespace, name] = value.split("|");
    let port = "";
    try {
      const obj = await getResourceObject("core/v1/services", namespace, name);
      port = String(backendFromService(obj.yaml)?.ports[0]?.port ?? "");
    } catch {
      // the host is still useful without the port
    }
    onPick(`${name}.${namespace}.svc.cluster.local`, port);
  }

  return (
    <label className="flex flex-col gap-1">
      <span className="text-text-muted">{t("Found in this cluster")}</span>
      <select
        aria-label="PostgreSQL found in this cluster"
        defaultValue=""
        onChange={(e) => void pick(e.target.value)}
        className={input}
      >
        <option value="">{t("Choose a PostgreSQL Service…")}</option>
        {rows.map((r) => (
          <option
            key={`${r.namespace}/${r.name}`}
            value={`${r.namespace}|${r.name}`}
          >
            {r.namespace}/{r.name}
          </option>
        ))}
      </select>
    </label>
  );
}
