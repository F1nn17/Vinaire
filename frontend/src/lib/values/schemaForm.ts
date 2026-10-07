import { Document, parse, parseDocument, stringify } from "yaml";

// A form for a Helm chart's values, built from the chart's own values.schema.json
// (Roadmap Phase 43). The form is only a friendlier way to edit the same YAML the
// "Your values" tab holds: edits go through the YAML Document API, so comments and
// keys the form doesn't know about survive, and a field left alone isn't written
// at all — the values stay "only what you changed".
//
// Supported: object / string / integer / number / boolean, enum, arrays of scalars,
// local $ref, and (as a YAML text box) anything else — maps, arrays of objects,
// oneOf/anyOf. A schema that yields no fields means the form isn't offered.

type JSONSchema = {
  type?: string | string[];
  properties?: Record<string, JSONSchema>;
  additionalProperties?: boolean | JSONSchema;
  items?: JSONSchema;
  enum?: unknown[];
  default?: unknown;
  description?: string;
  title?: string;
  minimum?: number;
  maximum?: number;
  pattern?: string;
  format?: string;
  required?: string[];
  $ref?: string;
  allOf?: JSONSchema[];
  oneOf?: JSONSchema[];
  anyOf?: JSONSchema[];
  $defs?: Record<string, JSONSchema>;
  definitions?: Record<string, JSONSchema>;
};

export type FieldKind =
  | "string"
  | "number"
  | "integer"
  | "boolean"
  | "enum"
  | "list" // array of scalars, edited as a comma-separated line
  | "yaml"; // anything the form can't draw: edited as a YAML snippet

export type Field = {
  path: string[];
  kind: FieldKind;
  label: string;
  description?: string;
  enumValues?: unknown[];
  schemaDefault?: unknown;
  required?: boolean;
  min?: number;
  max?: number;
  secretLike?: boolean;
  // a "list" whose items are numbers
  itemNumeric?: boolean;
};

export type Section = { name: string; fields: Field[] };

const MAX_DEPTH = 5;
const MAX_FIELDS = 1500;

function resolve(root: JSONSchema, s: JSONSchema, seen = 0): JSONSchema {
  let cur = s;
  for (let i = 0; i < 8 && cur.$ref; i++) {
    const ref = cur.$ref;
    if (!ref.startsWith("#/") || seen > 16) return cur;
    let target: unknown = root;
    for (const part of ref.slice(2).split("/")) {
      target = (target as Record<string, unknown> | undefined)?.[
        part.replace(/~1/g, "/").replace(/~0/g, "~")
      ];
    }
    if (!target || typeof target !== "object") return cur;
    cur = {
      ...(target as JSONSchema),
      description: cur.description ?? (target as JSONSchema).description,
    };
  }
  return cur;
}

function mainType(s: JSONSchema): string | undefined {
  if (Array.isArray(s.type)) return s.type.find((t) => t !== "null");
  return s.type;
}

const secretName = /(password|passwd|secret|token|apikey|api_key|privatekey)/i;

function leaf(path: string[], s: JSONSchema, required: boolean): Field | null {
  const label = path[path.length - 1];
  const base = {
    path,
    label,
    description: s.description ?? s.title,
    schemaDefault: s.default,
    required,
    min: s.minimum,
    max: s.maximum,
    secretLike: secretName.test(label),
  };
  if (s.enum && s.enum.length > 0 && s.enum.length <= 100) {
    return { ...base, kind: "enum", enumValues: s.enum };
  }
  switch (mainType(s)) {
    case "boolean":
      return { ...base, kind: "boolean" };
    case "integer":
      return { ...base, kind: "integer" };
    case "number":
      return { ...base, kind: "number" };
    case "string":
      return { ...base, kind: "string" };
    case "array": {
      const it = s.items;
      const t = it && typeof it === "object" ? mainType(it) : undefined;
      if (t === "string" || t === "integer" || t === "number") {
        return { ...base, kind: "list", itemNumeric: t !== "string" };
      }
      return { ...base, kind: "yaml" };
    }
    default:
      return null;
  }
}

function collect(
  root: JSONSchema,
  schema: JSONSchema,
  path: string[],
  required: boolean,
  out: Field[],
  depth: number,
): void {
  if (out.length >= MAX_FIELDS) return;
  const s = resolve(root, schema);
  // allOf: take what its parts say about properties
  if (s.allOf && !s.properties) {
    for (const part of s.allOf) collect(root, part, path, required, out, depth);
    return;
  }
  if (s.properties && (mainType(s) === "object" || mainType(s) === undefined)) {
    if (depth >= MAX_DEPTH) {
      if (path.length)
        out.push({
          path,
          kind: "yaml",
          label: path[path.length - 1],
          description: s.description,
          required,
        });
      return;
    }
    const req = new Set(s.required ?? []);
    for (const [k, sub] of Object.entries(s.properties)) {
      collect(root, sub, [...path, k], req.has(k), out, depth + 1);
    }
    return;
  }
  if (path.length === 0) return;
  // a map, a oneOf, an array of objects… — the form can't draw it, so a YAML snippet
  if (s.oneOf || s.anyOf || mainType(s) === "object") {
    out.push({
      path,
      kind: "yaml",
      label: path[path.length - 1],
      description: s.description ?? s.title,
      schemaDefault: s.default,
      required,
    });
    return;
  }
  const f = leaf(path, s, required);
  if (f) out.push(f);
}

// The fields of a schema, grouped by the top-level key they sit under (values with
// no group — top-level scalars — go in "General").
export function buildSections(schemaText: string): Section[] {
  let root: JSONSchema;
  try {
    root = JSON.parse(schemaText) as JSONSchema;
  } catch {
    return [];
  }
  if (!root || typeof root !== "object") return [];
  const fields: Field[] = [];
  collect(root, root, [], false, fields, 0);
  const groups = new Map<string, Field[]>();
  for (const f of fields) {
    const g = f.path.length > 1 ? f.path[0] : "General";
    groups.set(g, [...(groups.get(g) ?? []), f]);
  }
  return [...groups.entries()]
    .sort(([a], [b]) =>
      a === "General" ? -1 : b === "General" ? 1 : a.localeCompare(b),
    )
    .map(([name, fs]) => ({ name, fields: fs }));
}

// ---- reading and writing the values YAML --------------------------------------

export function parseValuesText(text: string): {
  doc: Document;
  error?: string;
} {
  const doc = parseDocument(text || "");
  if (doc.errors.length > 0) {
    return { doc, error: doc.errors[0].message };
  }
  if (
    doc.contents !== null &&
    !(doc.contents as { items?: unknown }).items &&
    doc.toJS() !== null
  ) {
    return { doc, error: "The values must be a YAML map." };
  }
  return { doc };
}

export function getAt(obj: unknown, path: string[]): unknown {
  let cur: unknown = obj;
  for (const p of path) {
    if (cur === null || typeof cur !== "object") return undefined;
    cur = (cur as Record<string, unknown>)[p];
  }
  return cur;
}

// The user's override for a field ("what they changed") and the effective default
// (the chart's own default values.yaml, else the schema's default).
export function overrideOf(doc: Document, f: Field): unknown {
  return doc.getIn(f.path);
}

export function defaultOf(defaults: unknown, f: Field): unknown {
  const d = getAt(defaults, f.path);
  return d === undefined ? f.schemaDefault : d;
}

// Set (or, with undefined, remove) a field in the document, keeping comments, and
// prune maps that become empty so clearing a field leaves no `a: {b: {}}` husk.
export function applyValue(text: string, f: Field, value: unknown): string {
  const { doc, error } = parseValuesText(text);
  if (error) return text;
  if (value === undefined) {
    doc.deleteIn(f.path);
    for (let n = f.path.length - 1; n >= 1; n--) {
      const parent = doc.getIn(f.path.slice(0, n), true) as
        { items?: unknown[] } | undefined;
      if (parent && Array.isArray(parent.items) && parent.items.length === 0) {
        doc.deleteIn(f.path.slice(0, n));
      } else break;
    }
  } else {
    doc.setIn(f.path, value);
  }
  const out = doc.toString();
  return out.trim() === "{}" ? "" : out;
}

// ---- field value <-> text -----------------------------------------------------

export function toInput(f: Field, v: unknown): string {
  if (v === undefined || v === null) return "";
  if (f.kind === "list") return Array.isArray(v) ? v.join(", ") : String(v);
  if (f.kind === "yaml") return stringify(v).trimEnd();
  return String(v);
}

// Text from the form -> the value to store; undefined removes the override; an
// Error says why the text isn't acceptable.
export function fromInput(f: Field, text: string): unknown | Error {
  const t = text.trim();
  if (t === "") return undefined;
  switch (f.kind) {
    case "integer": {
      if (!/^-?\d+$/.test(t)) return new Error("a whole number");
      const n = Number(t);
      return rangeCheck(f, n);
    }
    case "number": {
      const n = Number(t);
      if (!Number.isFinite(n)) return new Error("a number");
      return rangeCheck(f, n);
    }
    case "list": {
      const parts = t
        .split(",")
        .map((x) => x.trim())
        .filter(Boolean);
      if (!f.itemNumeric) return parts;
      const nums = parts.map(Number);
      return nums.some((n) => !Number.isFinite(n))
        ? new Error("a comma-separated list of numbers")
        : nums;
    }
    case "yaml": {
      try {
        return parse(t) as unknown;
      } catch (e) {
        return new Error("valid YAML: " + (e as Error).message.split("\n")[0]);
      }
    }
    default:
      return text; // string / enum: as typed
  }
}

function rangeCheck(f: Field, n: number): number | Error {
  if (f.min !== undefined && n < f.min) return new Error(`at least ${f.min}`);
  if (f.max !== undefined && n > f.max) return new Error(`at most ${f.max}`);
  return n;
}
