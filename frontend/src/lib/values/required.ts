import { parse } from "yaml";
import { parseValuesText } from "./schemaForm";

// Roadmap Phase 71: when a chart refuses to render until a value is given, the install page asks for
// it (components/marketplace/RequiredValues.tsx) and writes the answer into the values YAML here —
// keeping the comments and whatever else the person already typed.

export type ValueNeed = {
  path: string;
  kind: "string" | "number" | "bool" | "object" | "list";
  sensitive?: boolean;
  secretRef?: boolean;
  children?: string[];
  guessed?: boolean;
};

export type RenderProblem = {
  message: string;
  template?: string;
  required: boolean;
  needs?: ValueNeed[];
};

// Sets a dotted path in the values text; text that isn't valid YAML is returned unchanged.
export function setPathInValues(
  text: string,
  path: string,
  value: unknown,
): string {
  const { doc, error } = parseValuesText(text);
  if (error) return text;
  doc.setIn(path.split("."), value);
  const out = doc.toString();
  return out.trim() === "{}" ? "" : out;
}

// What the person typed for a list or an object (YAML), or an error message.
export function parseYamlValue(
  text: string,
): { value: unknown } | { error: string } {
  try {
    return { value: parse(text) };
  } catch (e) {
    return { error: e instanceof Error ? e.message : String(e) };
  }
}

// A good default name for a Secret made for a value path: "<release>-<leaf in dashes>".
export function suggestSecretName(release: string, path: string): string {
  const leaf = path.slice(path.lastIndexOf(".") + 1);
  const kebab = leaf
    .replace(/([a-z0-9])([A-Z])/g, "$1-$2")
    .toLowerCase()
    .replace(/[^a-z0-9-]+/g, "-")
    .replace(/^-+|-+$/g, "");
  const name = `${release || "app"}-${kebab || "secret"}`;
  return name.slice(0, 63).replace(/-+$/, "");
}

// The alternative to offer first: a Secret reference keeps the password out of Helm's release record.
export function preferredNeed(needs: ValueNeed[]): number {
  const i = needs.findIndex(
    (n) => n.secretRef && (n.children?.length ?? 0) > 0,
  );
  return i >= 0 ? i : 0;
}

// Only a password-like Secret reference is worth generating a Secret for (not a connection string).
export function canGenerateSecret(n: ValueNeed): boolean {
  const leaf = n.path.slice(n.path.lastIndexOf(".") + 1);
  return (
    !!n.secretRef &&
    (n.children ?? []).includes("name") &&
    (n.children ?? []).includes("key") &&
    /(password|passwd|token|apikey)/i.test(leaf)
  );
}
