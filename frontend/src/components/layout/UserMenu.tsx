"use client";

import { useEffect, useRef, useState } from "react";
import { logout } from "@/lib/api/auth";

// Roadmap Phase 10: a sign-out action against the session cookie. There's
// no client-visible username to show — the session is an opaque cookie the
// browser holds, not something this code reads (token mode's pasted
// ServiceAccount tokens have no human name attached anyway).
export function UserMenu() {
  const [open, setOpen] = useState(false);
  const rootRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    function onClickOutside(e: MouseEvent) {
      if (rootRef.current && !rootRef.current.contains(e.target as Node)) {
        setOpen(false);
      }
    }
    document.addEventListener("mousedown", onClickOutside);
    return () => document.removeEventListener("mousedown", onClickOutside);
  }, []);

  async function handleSignOut() {
    await logout();
    window.location.href = "/login";
  }

  return (
    <div ref={rootRef} className="relative">
      <button
        type="button"
        onClick={() => setOpen((v) => !v)}
        aria-label="User menu"
        className="flex h-[30px] w-[30px] items-center justify-center rounded-full border border-border bg-surface-2 text-xs font-semibold"
      >
        ?
      </button>

      {open && (
        <div className="absolute right-0 top-[calc(100%+6px)] z-20 min-w-[160px] rounded-lg border border-border bg-surface p-1 shadow-lg">
          <button
            type="button"
            onClick={handleSignOut}
            className="w-full rounded-md px-3 py-2 text-left text-[12.5px] text-err hover:bg-surface-2"
          >
            Sign out
          </button>
        </div>
      )}
    </div>
  );
}
