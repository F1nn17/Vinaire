import { getLang } from "./state";
import { en } from "./dict/en";
import { ru } from "./dict/ru";

// Translation is exact-match and per string: the English UI text is its own key
// (so a developer writes plain English), looked up in ru; the Russian help text is
// looked up in en for the English mode. Anything not in the dictionary — a pod
// name, a message from the server — passes through untouched.

const CYRILLIC = /[\u0400-\u04FF]/;

// leading/trailing whitespace around a string belongs to the layout (it sits next to
// an inline element), so it is kept; the middle is compared with whitespace collapsed
function split(text: string): [string, string, string] {
  const m = text.match(/^(\s*)([\s\S]*?)(\s*)$/);
  return m ? [m[1], m[2], m[3]] : ["", text, ""];
}

export function tr(text: string): string {
  const lang = getLang();
  if (lang === "en" && !CYRILLIC.test(text)) return text;
  if (lang === "ru" && CYRILLIC.test(text)) return text;
  const [pre, core, post] = split(text);
  if (core === "") return text;
  const dict = lang === "ru" ? ru : en;
  // multi-line text (code blocks) is stored as written, newlines included
  const hit = dict[core] ?? dict[core.replace(/\s+/g, " ")];
  return hit === undefined ? text : pre + hit + post;
}

// {n} placeholders for the few messages whose words must move around:
// t("{n} of {m} shown", {n: 3, m: 9}).
export function t(
  text: string,
  vars?: Record<string, string | number>,
): string {
  const out = tr(text);
  if (!vars) return out;
  return out.replace(/\{(\w+)\}/g, (_, k) =>
    k in vars ? String(vars[k]) : `{${k}}`,
  );
}

const TEXT_ATTRS = [
  "placeholder",
  "title",
  "aria-label",
  "alt",
  "aria-description",
] as const;

type Props = Record<string, unknown>;

// Applied to host elements (div, span, input…) as they are created: their text
// children and the attributes people read. Doing it here — where React elements are
// built — rather than patching the DOM means module-level constants (tab lists,
// column headers) are translated too, and React stays in charge of the page.
export function translateProps(type: unknown, props: Props): Props {
  if (typeof type !== "string" || props === null || typeof props !== "object")
    return props;
  let out: Props | null = null;
  const set = (k: string, v: unknown) => {
    if (!out) out = { ...props };
    out[k] = v;
  };
  const c = props.children;
  if (typeof c === "string") {
    const n = tr(c);
    if (n !== c) set("children", n);
  } else if (Array.isArray(c)) {
    let changed = false;
    const next = c.map((x) => {
      if (typeof x === "string") {
        const n = tr(x);
        if (n !== x) changed = true;
        return n;
      }
      return x;
    });
    if (changed) set("children", next);
  }
  for (const a of TEXT_ATTRS) {
    const v = props[a];
    if (typeof v === "string") {
      const n = tr(v);
      if (n !== v) set(a, n);
    }
  }
  if (type === "option" || type === "optgroup") {
    const v = props.label;
    if (typeof v === "string") {
      const n = tr(v);
      if (n !== v) set("label", n);
    }
  }
  return out ?? props;
}
