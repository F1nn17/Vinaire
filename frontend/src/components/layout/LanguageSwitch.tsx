"use client";

import { useSyncExternalStore } from "react";
import {
  getLang,
  getServerLang,
  setLang,
  subscribeLang,
  type Lang,
} from "@/lib/i18n/state";

const options: { lang: Lang; label: string }[] = [
  { lang: "en", label: "English" },
  { lang: "ru", label: "Русский" },
];

// EN | RU. The choice is kept in a cookie and applies to the whole interface.
export function LanguageSwitch() {
  const lang = useSyncExternalStore(subscribeLang, getLang, getServerLang);
  return (
    <div
      role="group"
      aria-label="Interface language"
      title="Interface language"
      className="flex overflow-hidden rounded-lg border border-border text-xs"
    >
      {options.map((o) => (
        <button
          key={o.lang}
          type="button"
          aria-pressed={lang === o.lang}
          onClick={() => setLang(o.lang)}
          className={`px-2.5 py-1 font-medium ${
            lang === o.lang
              ? "bg-accent text-white"
              : "text-text-faint hover:text-text"
          }`}
        >
          {lang === o.lang ? "✓ " : ""}
          {o.label}
        </button>
      ))}
    </div>
  );
}
