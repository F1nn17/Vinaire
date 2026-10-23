// Roadmap Phase 56: keeps both themes readable and the colours in one place.
//  1. WCAG contrast of every text colour on the surfaces it is drawn on, in the dark and the
//     light theme (read from src/app/globals.css).
//  2. No hard-coded colour anywhere else in the source: every colour comes from a token, so a
//     theme change reaches the whole interface.
import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

const root = path.join(
  path.dirname(fileURLToPath(import.meta.url)),
  "..",
  "src",
);
const css = fs.readFileSync(path.join(root, "app/globals.css"), "utf8");

function block(selector) {
  const i = css.indexOf(selector);
  if (i < 0) throw new Error(`no ${selector} block in globals.css`);
  const body = css.slice(css.indexOf("{", i) + 1, css.indexOf("}", i));
  const out = {};
  for (const m of body.matchAll(/--([a-z0-9-]+):\s*([^;]+);/g))
    out[m[1]] = m[2].trim();
  return out;
}

// ---- colour maths ----------------------------------------------------------------
const hex = (h) => {
  const m = /^#([0-9a-f]{6})$/i.exec(h);
  if (!m) throw new Error(`not a #rrggbb colour: ${h}`);
  const n = parseInt(m[1], 16);
  return [(n >> 16) & 255, (n >> 8) & 255, n & 255];
};
const lin = (c) => {
  c /= 255;
  return c <= 0.04045 ? c / 12.92 : ((c + 0.055) / 1.055) ** 2.4;
};
const lum = ([r, g, b]) => 0.2126 * lin(r) + 0.7152 * lin(g) + 0.0722 * lin(b);
const ratio = (a, b) => {
  const [hi, lo] = [lum(a), lum(b)].sort((x, y) => y - x);
  return (hi + 0.05) / (lo + 0.05);
};
// oklab round-trip, for `color-mix(in oklch, accent P%, white|black Q%)`
const toLinear = (rgb) => rgb.map(lin);
const toOklab = ([r, g, b]) => {
  const l = Math.cbrt(0.4122214708 * r + 0.5363325363 * g + 0.0514459929 * b);
  const m = Math.cbrt(0.2119034982 * r + 0.6806995451 * g + 0.1073969566 * b);
  const s = Math.cbrt(0.0883024619 * r + 0.2817188376 * g + 0.6299787005 * b);
  return [
    0.2104542553 * l + 0.793617785 * m - 0.0040720468 * s,
    1.9779984951 * l - 2.428592205 * m + 0.4505937099 * s,
    0.0259040371 * l + 0.7827717662 * m - 0.808675766 * s,
  ];
};
const fromOklab = ([L, a, b]) => {
  const l = (L + 0.3963377774 * a + 0.2158037573 * b) ** 3;
  const m = (L - 0.1055613458 * a - 0.0638541728 * b) ** 3;
  const s = (L - 0.0894841775 * a - 1.291485548 * b) ** 3;
  const lr = [
    4.0767416621 * l - 3.3077115913 * m + 0.2309699292 * s,
    -1.2684380046 * l + 2.6097574011 * m - 0.3413193965 * s,
    -0.0041960863 * l - 0.7034186147 * m + 1.707614701 * s,
  ];
  return lr.map((c) => {
    c = Math.min(1, Math.max(0, c));
    return Math.round(
      255 * (c <= 0.0031308 ? 12.92 * c : 1.055 * c ** (1 / 2.4) - 0.055),
    );
  });
};
// (a polar mix in oklch differs from this in hue handling only; for a near-neutral second colour
// the lightness — which is what contrast depends on — is the same)
function mixAccent(accent, pct, other) {
  const A = toOklab(toLinear(hex(accent)));
  const O = toOklab(toLinear(other));
  const t = pct / 100;
  return fromOklab(A.map((v, i) => v * t + O[i] * (1 - t)));
}
function resolveAccentText(t) {
  const m =
    /color-mix\(in oklch,\s*var\(--accent\)\s+(\d+)%,\s*(white|black)\s+\d+%\)/.exec(
      t["accent-text"],
    );
  if (!m) throw new Error(`unexpected --accent-text: ${t["accent-text"]}`);
  return mixAccent(
    t.accent,
    Number(m[1]),
    m[2] === "white" ? [255, 255, 255] : [0, 0, 0],
  );
}

// ---- 1. contrast -----------------------------------------------------------------
const themes = {
  dark: block(":root {"),
  light: block(':root[data-theme="light"]'),
};
const AA = 4.5;
let failed = false;
const fail = (m) => {
  failed = true;
  console.error("  ✗ " + m);
};
for (const [name, t0] of Object.entries(themes)) {
  const t = { ...themes.dark, ...t0 }; // the light block overrides the dark defaults
  const colour = (k) => hex(t[k]);
  const accentText = resolveAccentText(t);
  const surfaces = ["bg", "surface", "surface-2"];
  const pairs = [];
  for (const s of surfaces) {
    pairs.push(
      ["text", s, AA],
      ["text-muted", s, AA],
      ["ok", s, AA],
      ["warn", s, AA],
      ["err", s, AA],
    );
  }
  // secondary text (hints, timestamps) must stay readable on the two main surfaces
  pairs.push(["text-faint", "bg", AA], ["text-faint", "surface", AA]);
  let n = 0;
  for (const [fg, bg, min] of pairs) {
    const r = ratio(colour(fg), colour(bg));
    n++;
    if (r < min)
      fail(`${name}: ${fg} on ${bg} is ${r.toFixed(2)}:1 (needs ${min}:1)`);
  }
  for (const s of surfaces.slice(0, 2)) {
    n++;
    const r = ratio(accentText, colour(s));
    if (r < AA)
      fail(`${name}: accent-text on ${s} is ${r.toFixed(2)}:1 (needs ${AA}:1)`);
  }
  // white text on the accent (primary buttons)
  n++;
  const rw = ratio([255, 255, 255], colour("accent"));
  if (rw < AA)
    fail(`${name}: white on accent is ${rw.toFixed(2)}:1 (needs ${AA}:1)`);
  // the code background must still separate from the page and keep the text readable
  n++;
  const rc = ratio(colour("text"), colour("code-bg"));
  if (rc < AA) fail(`${name}: text on code-bg is ${rc.toFixed(2)}:1`);
  console.log(`${name}: ${n} contrast pairs checked`);
}

// ---- 2. no colour outside the tokens ---------------------------------------------
// keycloakThemeFiles.ts is the text of Keycloak's sign-in theme (a stylesheet Vinaire hands out), not Vinaire's own UI
const allowed = new Set([
  "app/globals.css",
  "lib/theme.ts",
  "lib/keycloakThemeFiles.ts",
]);
const colourRe =
  /#[0-9a-fA-F]{3,8}\b|\brgba?\(|\bhsla?\(|\boklch\(|\bcolor-mix\(/;
const walk = (d, out = []) => {
  for (const e of fs.readdirSync(d, { withFileTypes: true })) {
    const p = path.join(d, e.name);
    if (e.isDirectory()) walk(p, out);
    else if (/\.(tsx?|css)$/.test(e.name)) out.push(p);
  }
  return out;
};
for (const file of walk(root)) {
  const rel = path.relative(root, file).replace(/\\/g, "/");
  if (allowed.has(rel) || rel.startsWith("lib/i18n/")) continue;
  fs.readFileSync(file, "utf8")
    .split("\n")
    .forEach((line, i) => {
      const code = line.replace(/\/\/.*$/, "");
      if (colourRe.test(code))
        fail(
          `${rel}:${i + 1} has a hard-coded colour — use a token from globals.css: ${line.trim().slice(0, 100)}`,
        );
    });
}
if (failed) process.exit(1);
console.log("theme ok");
