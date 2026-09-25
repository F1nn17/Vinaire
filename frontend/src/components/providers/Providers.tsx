"use client";

import { SessionProvider } from "next-auth/react";
import { useEffect, type ReactNode } from "react";
import { initCurrentCluster } from "@/lib/clusterStore";

export function Providers({ children }: { children: ReactNode }) {
  useEffect(() => {
    initCurrentCluster();
  }, []);

  return <SessionProvider>{children}</SessionProvider>;
}
