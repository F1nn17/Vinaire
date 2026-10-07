// The interface language (Roadmap Phase 46). One module-level value that every
// translated render reads, plus a cookie to remember the choice. A cookie, not web
// storage (the project forbids that), and not sensitive: it holds "en" or "ru".

export type Lang = "en" | "ru";

const COOKIE = "vinaire_lang";
const listeners = new Set<() => void>();

// Always "en" until the app has hydrated: the static HTML is English, and the first
// client render must match it. initLang() then reads the real choice.
let current: Lang = "en";

export function getLang(): Lang {
  return current;
}

export function getServerLang(): Lang {
  return "en";
}

function readCookie(): Lang | null {
  const m = document.cookie.match(new RegExp(`(?:^|; )${COOKIE}=(en|ru)`));
  return m ? (m[1] as Lang) : null;
}

// The saved choice, else the browser's language (Russian browsers get Russian).
export function initLang(): void {
  if (typeof document === "undefined") return;
  const saved = readCookie();
  const next: Lang =
    saved ?? (navigator.language?.toLowerCase().startsWith("ru") ? "ru" : "en");
  if (next !== current) {
    current = next;
    document.documentElement.lang = next;
    listeners.forEach((l) => l());
  }
}

export function setLang(next: Lang): void {
  current = next;
  if (typeof document !== "undefined") {
    document.cookie = `${COOKIE}=${next}; Path=/; Max-Age=31536000; SameSite=Lax`;
    document.documentElement.lang = next;
  }
  listeners.forEach((l) => l());
}

export function subscribeLang(onChange: () => void): () => void {
  listeners.add(onChange);
  initLang(); // hydration is done by the time a component subscribes
  return () => listeners.delete(onChange);
}
