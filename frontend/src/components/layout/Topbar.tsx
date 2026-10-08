"use client";

import { useCallback, useEffect, useState } from "react";
import { SearchIcon } from "@/components/icons";
import { AlertsBell } from "@/components/layout/AlertsBell";
import { BackendStatus } from "@/components/layout/BackendStatus";
import { LanguageSwitch } from "@/components/layout/LanguageSwitch";
import { ThemeSwitch } from "@/components/layout/ThemeSwitch";
import { ClusterPicker } from "@/components/layout/ClusterPicker";
import { CommandPalette } from "@/components/layout/CommandPalette";
import { UserMenu } from "@/components/layout/UserMenu";

export function Topbar() {
  const [searchOpen, setSearchOpen] = useState(false);
  const closeSearch = useCallback(() => setSearchOpen(false), []);

  useEffect(() => {
    function onKeyDown(e: KeyboardEvent) {
      if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === "k") {
        e.preventDefault();
        setSearchOpen(true);
      }
    }
    window.addEventListener("keydown", onKeyDown);
    return () => window.removeEventListener("keydown", onKeyDown);
  }, []);

  return (
    <div className="flex h-[60px] shrink-0 items-center gap-4 border-b border-border px-6">
      <ClusterPicker />

      <label htmlFor="global-search" className="sr-only">
        Search resources
      </label>
      <button
        type="button"
        onClick={() => setSearchOpen(true)}
        className="flex max-w-[480px] flex-1 items-center gap-2.5 rounded-lg border border-border bg-surface px-3.5 py-[9px] text-left"
      >
        <SearchIcon className="text-text-muted" />
        <span
          id="global-search"
          className="flex-1 font-mono text-[13px] text-text-faint"
        >
          Search resources…
        </span>
        <span className="rounded-[5px] border border-border px-1.5 py-0.5 font-mono text-[11px] text-text-faint">
          ⌘K
        </span>
      </button>

      <div className="ml-auto flex items-center gap-4">
        <BackendStatus />
        <ThemeSwitch />
        <LanguageSwitch />
        <AlertsBell />
        <UserMenu />
      </div>

      {searchOpen && <CommandPalette onClose={closeSearch} />}
    </div>
  );
}
