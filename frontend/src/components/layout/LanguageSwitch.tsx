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
  { lang: "en", label: "EN" },
  { lang: "ru", label: "RU" },
];

// EN | RU. The choice is kept in a cookie and applies to the whole interface.
export function LanguageSwitch() {
  const lang = useSyncExternalStore(subscribeLang, getLang, getServerLang);
  return (
    <div
      role="group"
      aria-label="Language"
      className="flex overflow-hidden rounded-lg border border-border text-[11px]"
    >
      {options.map((o) => (
        <button
          key={o.lang}
          type="button"
          aria-pressed={lang === o.lang}
          onClick={() => setLang(o.lang)}
          className={`px-2 py-1 font-mono ${
            lang === o.lang
              ? "bg-surface-2 text-accent-text"
              : "text-text-faint hover:text-text"
          }`}
        >
          {o.label}
        </button>
      ))}
    </div>
  );
}
