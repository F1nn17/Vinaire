"use client";

import { Fragment, useSyncExternalStore, type ReactNode } from "react";
import { getLang, getServerLang, subscribeLang } from "@/lib/i18n/state";

// Re-renders the whole tree when the interface language changes. Elements are
// translated as they are created (lib/i18n/translate.ts), so a language switch has
// to build them again: giving the subtree a key that is the language remounts it.
// The server snapshot is always "en", matching the static HTML, so hydration agrees;
// the saved language is applied right after.
export function I18nProvider({ children }: { children: ReactNode }) {
  const lang = useSyncExternalStore(subscribeLang, getLang, getServerLang);
  return <Fragment key={lang}>{children}</Fragment>;
}
