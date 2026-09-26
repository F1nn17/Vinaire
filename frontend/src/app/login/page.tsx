"use client";

import { useEffect, useState } from "react";
import { useRouter } from "next/navigation";
import useSWR from "swr";
import { getAuthConfig, loginWithToken, oidcLoginURL } from "@/lib/api/auth";
import { ApiError } from "@/lib/api/http";

// Roadmap Phase 10: which form this page shows depends entirely on the
// backend's active AUTH_MODE (GET /api/v1/auth/config) — token-paste for
// "token", a Keycloak redirect for "oidc". "disabled" never reaches this
// page at all (see proxy.ts, which skips the login redirect for it).
export default function LoginPage() {
  const router = useRouter();
  const { data: config } = useSWR("auth-config", getAuthConfig);
  const [token, setToken] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    if (config?.mode === "disabled") {
      router.replace("/");
    }
  }, [config, router]);

  async function handleTokenSubmit(e: React.FormEvent) {
    e.preventDefault();
    if (!token.trim()) return;
    setBusy(true);
    setError(null);
    try {
      await loginWithToken(token.trim());
      router.replace("/");
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "Login failed.");
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="flex min-h-screen items-center justify-center bg-bg px-4">
      <div className="w-full max-w-md rounded-xl border border-border bg-surface p-8">
        <div className="mb-1 font-mono text-lg font-semibold">Vinaire</div>
        <div className="mb-6 text-sm text-text-muted">Sign in to continue</div>

        {config?.mode === "token" && (
          <form onSubmit={handleTokenSubmit} className="flex flex-col gap-3">
            <label htmlFor="k8s-token" className="text-xs text-text-muted">
              Kubernetes ServiceAccount token
            </label>
            <textarea
              id="k8s-token"
              value={token}
              onChange={(e) => setToken(e.target.value)}
              placeholder="eyJhbGciOi..."
              rows={5}
              autoFocus
              className="resize-none rounded-lg border border-border bg-surface-2 p-3 font-mono text-xs outline-none focus:border-accent"
            />
            {error && <div className="text-xs text-err">{error}</div>}
            <button
              type="submit"
              disabled={busy || !token.trim()}
              className="rounded-lg bg-accent px-3.5 py-2 text-[13px] font-semibold text-white disabled:opacity-60"
            >
              {busy ? "Signing in…" : "Sign in"}
            </button>
            <p className="text-[11px] text-text-faint">
              Create one with{" "}
              <code className="font-mono">
                kubectl create token &lt;serviceaccount&gt; --duration=1h
              </code>
              . The token is never stored in this browser — only an opaque
              session cookie is.
            </p>
          </form>
        )}

        {config?.mode === "oidc" && (
          <a
            href={oidcLoginURL()}
            className="block rounded-lg bg-accent px-3.5 py-2 text-center text-[13px] font-semibold text-white"
          >
            Sign in with Keycloak
          </a>
        )}

        {!config && <div className="text-sm text-text-muted">Loading…</div>}
      </div>
    </div>
  );
}
