"use client";

import { setThemeMode, useThemeMode, type ThemeMode } from "@/lib/theme";

const options: { mode: ThemeMode; label: string; title: string }[] = [
  { mode: "light", label: "☀", title: "Light theme" },
  { mode: "auto", label: "Auto", title: "Follow the system's theme" },
  { mode: "dark", label: "☾", title: "Dark theme" },
];

// Light | Auto | Dark. The choice is kept in a cookie and applies to the whole interface.
export function ThemeSwitch() {
  const mode = useThemeMode();
  return (
    <div
      role="group"
      aria-label="Theme"
      title="Theme"
      className="flex overflow-hidden rounded-lg border border-border text-xs"
    >
      {options.map((o) => (
        <button
          key={o.mode}
          type="button"
          aria-pressed={mode === o.mode}
          aria-label={o.title}
          title={o.title}
          onClick={() => setThemeMode(o.mode)}
          className={`px-2.5 py-1 font-medium ${
            mode === o.mode
              ? "bg-accent text-white"
              : "text-text-faint hover:text-text"
          }`}
        >
          {o.label}
        </button>
      ))}
    </div>
  );
}
