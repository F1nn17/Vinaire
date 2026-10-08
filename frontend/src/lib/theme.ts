import { useSyncExternalStore } from "react";

// The interface theme (Roadmap Phase 56). `mode` is what the person chose — auto (follow the
// system), dark or light — kept in a cookie (web storage is off-limits in this app, and the
// choice isn't sensitive). The *resolved* theme, dark or light, is what the page actually
// shows; it sits on <html data-theme>, where public/theme-init.js puts it before first paint.

export type ThemeMode = "auto" | "dark" | "light";
export type ResolvedTheme = "dark" | "light";

const COOKIE = "vinaire_theme";
const listeners = new Set<() => void>();

function readMode(): ThemeMode {
  if (typeof document === "undefined") return "auto";
  const m = document.cookie.match(
    new RegExp(`(?:^|; )${COOKIE}=(auto|dark|light)`),
  );
  return m ? (m[1] as ThemeMode) : "auto";
}

function systemPrefersLight(): boolean {
  return (
    typeof window !== "undefined" &&
    !!window.matchMedia &&
    window.matchMedia("(prefers-color-scheme: light)").matches
  );
}

export function resolve(mode: ThemeMode): ResolvedTheme {
  if (mode === "dark") return "dark";
  if (mode === "light") return "light";
  return systemPrefersLight() ? "light" : "dark";
}

let mode: ThemeMode = "auto";
let resolved: ResolvedTheme = "dark";
let started = false;

function apply() {
  resolved = resolve(mode);
  if (typeof document !== "undefined") {
    document.documentElement.setAttribute("data-theme", resolved);
  }
  listeners.forEach((l) => l());
}

function start() {
  if (started || typeof window === "undefined") return;
  started = true;
  mode = readMode();
  apply();
  // in auto mode a change of the system's setting applies at once
  window
    .matchMedia?.("(prefers-color-scheme: light)")
    .addEventListener?.("change", () => {
      if (mode === "auto") apply();
    });
}

export function getThemeMode(): ThemeMode {
  return mode;
}

export function setThemeMode(next: ThemeMode): void {
  mode = next;
  if (typeof document !== "undefined") {
    document.cookie = `${COOKIE}=${next}; Path=/; Max-Age=31536000; SameSite=Lax`;
  }
  apply();
}

function subscribe(cb: () => void): () => void {
  start();
  listeners.add(cb);
  return () => listeners.delete(cb);
}

// Server and first client render both say "auto"/"dark": the static HTML can't know the
// person's choice, and <html data-theme> (set before paint) already shows the right colours.
export function useThemeMode(): ThemeMode {
  return useSyncExternalStore(
    subscribe,
    () => mode,
    () => "auto",
  );
}

export function useResolvedTheme(): ResolvedTheme {
  return useSyncExternalStore(
    subscribe,
    () => resolved,
    () => "dark",
  );
}

// Terminal colours (xterm draws on a canvas and can't read CSS variables).
export const terminalThemes: Record<
  ResolvedTheme,
  {
    background: string;
    foreground: string;
    cursor: string;
    selectionBackground: string;
  }
> = {
  dark: {
    background: "#0a0b0e",
    foreground: "#edeff3",
    cursor: "#edeff3",
    selectionBackground: "#3a3f4d",
  },
  light: {
    background: "#f6f7fb",
    foreground: "#14161c",
    cursor: "#14161c",
    selectionBackground: "#c9cfdf",
  },
};
