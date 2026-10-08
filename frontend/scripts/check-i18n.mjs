// Fails when the interface-language dictionaries have drifted from the code
// (Roadmap Phase 46): every Russian help fragment in the source needs an English
// entry, and no entry may be empty. English strings missing from ru.json are only
// reported — much of the UI text comes from the server and passes through as is.
import ts from "typescript";
import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

const root = path.join(
  path.dirname(fileURLToPath(import.meta.url)),
  "..",
  "src",
);
const read = (f) =>
  JSON.parse(fs.readFileSync(path.join(root, "lib/i18n/dict", f), "utf8"));
const ru = read("ru.json");
const en = read("en.json");

const CYR = /[\u0400-\u04FF]/;
const ENT = {
  amp: "&",
  lt: "<",
  gt: ">",
  quot: '"',
  apos: "'",
  ldquo: "“",
  rdquo: "”",
  nbsp: " ",
};
const decode = (s) =>
  s.replace(/&(#\d+|[a-z]+);/gi, (m, e) =>
    e[0] === "#" ? String.fromCharCode(Number(e.slice(1))) : (ENT[e] ?? m),
  );
const walk = (d, out = []) => {
  for (const e of fs.readdirSync(d, { withFileTypes: true })) {
    const p = path.join(d, e.name);
    if (e.isDirectory()) {
      if (e.name !== "i18n") walk(p, out);
    } else if (/\.tsx?$/.test(e.name)) out.push(p);
  }
  return out;
};

const missing = new Set();
for (const file of walk(root)) {
  const src = fs.readFileSync(file, "utf8");
  if (!CYR.test(src)) continue;
  const sf = ts.createSourceFile(
    file,
    src,
    ts.ScriptTarget.Latest,
    true,
    ts.ScriptKind.TSX,
  );
  const add = (raw) => {
    const t = decode(raw).replace(/\s+/g, " ").trim();
    if (CYR.test(t) && !(t in en) && !(decode(raw).trim() in en))
      missing.add(t);
  };
  const visit = (n) => {
    if (ts.isJsxText(n)) add(n.getText());
    else if (ts.isStringLiteral(n) || ts.isNoSubstitutionTemplateLiteral(n)) {
      if (!ts.isImportDeclaration(n.parent)) add(n.text);
    } else if (ts.isTemplateExpression(n)) {
      add(n.head.text);
      n.templateSpans.forEach((s) => add(s.literal.text));
    }
    ts.forEachChild(n, visit);
  };
  visit(sf);
}

let failed = false;
for (const [name, dict] of [
  ["ru", ru],
  ["en", en],
]) {
  for (const [k, v] of Object.entries(dict)) {
    if (!v || !k) {
      console.error(`${name}.json: empty entry for "${k}"`);
      failed = true;
    }
  }
}
if (missing.size) {
  failed = true;
  console.error(
    `${missing.size} Russian fragment(s) without an English entry in en.json:`,
  );
  for (const t of missing) console.error("  - " + t.slice(0, 100));
}
if (failed) process.exit(1);
console.log(
  `i18n ok: ${Object.keys(ru).length} ru, ${Object.keys(en).length} en entries`,
);
