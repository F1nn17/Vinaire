"use client";

import { useEffect, useRef, useState } from "react";
import { logout } from "@/lib/api/auth";
import { useWhoAmI } from "@/lib/api/keycloak";

// Roadmap Phase 10: a sign-out action against the session cookie. There's
// no client-visible username to show — the session is an opaque cookie the
// browser holds, not something this code reads (token mode's pasted
// ServiceAccount tokens have no human name attached anyway).
export function UserMenu() {
  const [open, setOpen] = useState(false);
  const [signOutError, setSignOutError] = useState(false);
  const rootRef = useRef<HTMLDivElement>(null);
  const { data: me } = useWhoAmI();

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
    setSignOutError(false);
    try {
      await logout();
    } catch {
      // the server could not end the session: say so instead of pretending, and let the person try again
      setSignOutError(true);
      return;
    }
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
        {me?.username ? me.username.slice(0, 1).toUpperCase() : "?"}
      </button>

      {open && (
        <div className="absolute right-0 top-[calc(100%+6px)] z-20 min-w-[160px] rounded-lg border border-border bg-surface p-1 shadow-lg">
          {me?.version && (
            <div className="border-b border-border px-3 py-2 text-[11px] text-text-muted">
              Vinaire {me.version}
            </div>
          )}
          {me?.username && (
            <div className="border-b border-border px-3 py-2 text-xs">
              <div className="font-semibold">{me.username}</div>
              {me.kubernetesUser && (
                <div className="mt-1 max-w-[260px] break-all font-mono text-[10.5px] text-text-muted">
                  The cluster knows you as {me.kubernetesUser}. If something is
                  forbidden, an administrator has to bind a role to that name.
                </div>
              )}
            </div>
          )}
          <button
            type="button"
            onClick={handleSignOut}
            className="w-full rounded-md px-3 py-2 text-left text-[12.5px] text-err hover:bg-surface-2"
          >
            Sign out
          </button>
          {signOutError && (
            <div className="px-3 pb-2 text-[11px] text-err">
              Could not end the session on the server — you are still signed in.
              Try again.
            </div>
          )}
        </div>
      )}
    </div>
  );
}
