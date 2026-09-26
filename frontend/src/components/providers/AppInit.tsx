"use client";

import { useEffect, type ReactNode } from "react";
import { initCurrentCluster } from "@/lib/clusterStore";

// Restores the last-selected cluster (Roadmap Phase 11) from localStorage
// on first client render. No session/auth provider needed here — the
// session lives entirely in an HttpOnly cookie the browser manages itself
// (Roadmap Phase 10); there's nothing for React to hold.
export function AppInit({ children }: { children: ReactNode }) {
  useEffect(() => {
    initCurrentCluster();
  }, []);

  return <>{children}</>;
}
