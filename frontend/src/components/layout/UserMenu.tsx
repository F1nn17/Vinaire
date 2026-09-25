"use client";

import { useEffect, useRef, useState } from "react";
import { useSession, signOut } from "next-auth/react";

// Roadmap Phase 10: replaces the decorative avatar circle with the actual
// logged-in user (from the Keycloak session) and a real sign-out action.
export function UserMenu() {
  const { data: session } = useSession();
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

  const name = session?.user?.name ?? session?.user?.email ?? "?";
  const initial = name.charAt(0).toUpperCase();

  return (
    <div ref={rootRef} className="relative">
      <button
        type="button"
        onClick={() => setOpen((v) => !v)}
        aria-label="User menu"
        className="flex h-[30px] w-[30px] items-center justify-center rounded-full border border-border bg-surface-2 text-xs font-semibold"
      >
        {initial}
      </button>

      {open && (
        <div className="absolute right-0 top-[calc(100%+6px)] z-20 min-w-[200px] rounded-lg border border-border bg-surface p-1 shadow-lg">
          <div className="truncate px-3 py-2 text-xs text-text-muted">
            {name}
          </div>
          <button
            type="button"
            onClick={() => signOut({ callbackUrl: "/" })}
            className="w-full rounded-md px-3 py-2 text-left text-[12.5px] text-err hover:bg-surface-2"
          >
            Sign out
          </button>
        </div>
      )}
    </div>
  );
}
