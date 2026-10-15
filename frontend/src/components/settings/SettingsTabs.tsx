"use client";

import Link from "next/link";
import { useRouter } from "next/navigation";
import { useRememberedState } from "@/lib/hooks/useRememberedState";

// The Settings header: the title and the tab bar. "All resources" and "Security checks" are pages of
// their own (their addresses don't change), but they are part of Settings — this bar is on them too,
// so a click on "Sign-in" or "Справка" from there goes straight back to that tab, and the page you are
// on is marked.
export type SettingsTab = "crds" | "signin" | "help";

export const settingsTabs: { id: SettingsTab; label: string }[] = [
  { id: "crds", label: "Custom Resources" },
  { id: "signin", label: "Sign-in" },
  { id: "help", label: "Справка" },
];

// pages that live under Settings (they used to be in the sidebar)
export const settingsTools = [
  { id: "resources", href: "/resources", label: "All resources" },
  { id: "security", href: "/security", label: "Security checks" },
] as const;

export type SettingsActive = SettingsTab | (typeof settingsTools)[number]["id"];

const base = "px-3.5 py-2 text-[12.5px]";
const on = "border-b-2 border-accent font-semibold text-accent-text";

// `onTab` is given by the Settings page itself, which shows the tab's content in place; on the
// other pages a tab click remembers the choice and goes to /settings.
export function SettingsTabs({
  active,
  onTab,
}: {
  active: SettingsActive;
  onTab?: (t: SettingsTab) => void;
}) {
  const router = useRouter();
  const [, remember] = useRememberedState<SettingsTab>("settings-tab", "help");

  function pick(id: SettingsTab) {
    if (onTab) {
      onTab(id);
      return;
    }
    remember(id);
    router.push("/settings");
  }

  return (
    <div className="flex flex-col gap-[18px]">
      <div className="text-[15px] font-semibold">Settings</div>
      <div className="flex gap-1 border-b border-border">
        {settingsTabs.map((t) => (
          <button
            key={t.id}
            type="button"
            onClick={() => pick(t.id)}
            aria-current={active === t.id ? "page" : undefined}
            className={`${base} ${active === t.id ? on : "text-text-muted"}`}
          >
            {t.label}
          </button>
        ))}
        {settingsTools.map((l) => (
          <Link
            key={l.href}
            href={l.href}
            aria-current={active === l.id ? "page" : undefined}
            className={`${base} ${
              active === l.id ? on : "text-text-muted hover:text-text"
            }`}
          >
            {l.label}
          </Link>
        ))}
      </div>
    </div>
  );
}
