import { parseDocument } from "yaml";
import { t } from "@/lib/i18n/translate";

// Install recipes (Roadmap Phase 59): the answers to a recipe's questions become Helm values.
// The recipe itself comes from the backend (data shipped with Vinaire); this file only turns a
// chosen option and the typed fields into edits of the "Your values" YAML — through the YAML
// Document API, so comments and everything else the person wrote survive.

export type RecipeField = {
  key: string;
  label: string;
  help?: string;
  placeholder?: string;
  default?: string;
  required?: boolean;
  kind?: "" | "number" | "secret" | "ingressclass";
  pattern?: string;
  // a secret field that can be filled by creating the Secret with a generated password
  generate?: {
    passwordKey?: string;
    passwordKeyField?: string;
    user?: string;
    userKey?: string;
    userKeyField?: string;
  };
};

export type RecipeSet = { path: string; value: unknown; as?: "number" };

export type RecipeOption = {
  id: string;
  title: string;
  note?: string;
  default?: boolean;
  fields?: RecipeField[];
  set: RecipeSet[];
};

export type RecipeSection = {
  id: string;
  title: string;
  help?: string;
  discover?: string;
  options: RecipeOption[];
};

export type Recipe = {
  summary?: string;
  guide?: string[];
  // shown once the install is done; {release} and {namespace} are filled in
  after?: string[];
  sections: RecipeSection[];
};

export type FieldValues = Record<string, string>;

const placeholderRe = /\{(\??)([a-zA-Z][a-zA-Z0-9]*)\}/g;
const optionalOnly = /^\{\?[a-zA-Z][a-zA-Z0-9]*\}$/;

// Substitute {field} placeholders. `undefined` = a placeholder names a field that is empty, so
// the whole entry is skipped (an empty Ingress class must not become `ingressClassName: ""`).
export function substitute(value: unknown, fields: FieldValues): unknown {
  if (typeof value === "string") {
    let missing = false;
    const out = value.replace(placeholderRe, (_, opt: string, k: string) => {
      const v = (fields[k] ?? "").trim();
      if (v === "" && opt !== "?") missing = true;
      return v;
    });
    // a value that is only an optional placeholder, left empty, is "not there" (the caller
    // drops the key) rather than an empty string
    if (!missing && out === "" && optionalOnly.test(value)) return null;
    return missing ? undefined : out;
  }
  if (Array.isArray(value)) {
    const items = value.map((v) => substitute(v, fields));
    return items.some((v) => v === undefined)
      ? undefined
      : items.filter((v) => v !== null);
  }
  if (value && typeof value === "object") {
    const out: Record<string, unknown> = {};
    for (const [k, v] of Object.entries(value as Record<string, unknown>)) {
      const s = substitute(v, fields);
      if (s === undefined) return undefined;
      if (s === null) continue; // an optional field left empty: the key is left out
      out[k] = s;
    }
    return out;
  }
  return value;
}

export type Patch = { path: string[]; value: unknown };

export function patchesFor(option: RecipeOption, fields: FieldValues): Patch[] {
  const out: Patch[] = [];
  for (const s of option.set) {
    let v = substitute(s.value, fields);
    if (v === undefined || v === null) continue;
    if (s.as === "number") {
      const n = Number(v);
      if (!Number.isFinite(n)) continue;
      v = n;
    }
    out.push({ path: s.path.split("."), value: v });
  }
  return out;
}

// The field values an option starts with: its defaults.
export function defaultFields(option: RecipeOption): FieldValues {
  const out: FieldValues = {};
  for (const f of option.fields ?? []) out[f.key] = f.default ?? "";
  return out;
}

// Why the typed fields can't be applied yet, or null.
export function fieldsError(
  option: RecipeOption,
  fields: FieldValues,
): string | null {
  for (const f of option.fields ?? []) {
    const v = (fields[f.key] ?? "").trim();
    if (v === "") {
      if (f.required) return t("{label} is required", { label: t(f.label) });
      continue;
    }
    if (f.kind === "number") {
      const n = Number(v);
      if (!/^\d+$/.test(v) || n < 1 || n > 65535) {
        return t("{label} must be a number from 1 to 65535", {
          label: t(f.label),
        });
      }
    }
    if (f.pattern && !new RegExp(f.pattern).test(v)) {
      return t("{label} isn't in the expected form", { label: t(f.label) });
    }
  }
  return null;
}

// Apply an option to the values text: first take back what the section's previous option set
// (the paths in `previous`, unless the new option sets them again), then write the new values.
// Returns the new text and the paths this option wrote.
export function applyOption(
  text: string,
  previous: string[][],
  option: RecipeOption,
  fields: FieldValues,
): { text: string; paths: string[][] } {
  const doc = parseDocument(text);
  const patches = patchesFor(option, fields);
  const keep = new Set(patches.map((p) => p.path.join(".")));
  for (const path of previous) {
    if (!keep.has(path.join(".")) && doc.hasIn(path)) doc.deleteIn(path);
    pruneEmptyParents(doc, path);
  }
  for (const p of patches) doc.setIn(p.path, p.value);
  let out = doc.toString();
  if (out.trim() === "{}") out = "";
  return { text: out, paths: patches.map((p) => p.path) };
}

// a deleted value can leave `a: {}` behind: remove maps this left empty
function pruneEmptyParents(
  doc: ReturnType<typeof parseDocument>,
  path: string[],
) {
  for (let i = path.length - 1; i > 0; i--) {
    const parent = path.slice(0, i);
    const node = doc.getIn(parent, true) as { items?: unknown[] } | undefined;
    if (node && Array.isArray(node.items) && node.items.length === 0) {
      doc.deleteIn(parent);
    } else break;
  }
}

// ---- charts without a recipe --------------------------------------------------------
// Many charts (the Bitnami family above all) bundle a database as a sub-chart switched by
// `<db>.enabled` and take their own through an `externalDatabase` block. A chart without a recipe
// that follows this convention still gets a hint and one button: switch the bundled database off
// and drop the chart's own `externalDatabase` skeleton (as its defaults write it) into the values,
// to be filled in. It guesses nothing about the values — it only copies what the chart documents.

const bundledDatabases = ["postgresql", "postgresql-ha", "mariadb", "mysql"];

export type ExternalDbHint = {
  bundled: string;
  skeleton: Record<string, unknown>;
};

export function detectExternalDb(defaultsText: string): ExternalDbHint | null {
  let defaults: unknown;
  try {
    defaults = parseDocument(defaultsText).toJS();
  } catch {
    return null;
  }
  if (!defaults || typeof defaults !== "object") return null;
  const d = defaults as Record<string, unknown>;
  const ext = d.externalDatabase;
  if (!ext || typeof ext !== "object" || Array.isArray(ext)) return null;
  const bundled = bundledDatabases.find((k) => {
    const v = d[k];
    return (
      v &&
      typeof v === "object" &&
      (v as Record<string, unknown>).enabled === true
    );
  });
  if (!bundled) return null;
  return { bundled, skeleton: { ...(ext as Record<string, unknown>) } };
}

// Switch the bundled database off and add the externalDatabase skeleton (keys already in the
// values are kept as they are).
export function applyExternalDb(text: string, hint: ExternalDbHint): string {
  const doc = parseDocument(text);
  doc.setIn([hint.bundled, "enabled"], false);
  for (const [k, v] of Object.entries(hint.skeleton)) {
    if (!doc.hasIn(["externalDatabase", k]))
      doc.setIn(["externalDatabase", k], v);
  }
  if ("enabled" in hint.skeleton)
    doc.setIn(["externalDatabase", "enabled"], true);
  return doc.toString();
}
