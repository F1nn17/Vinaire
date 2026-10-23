"use client";

import { useEffect, useState } from "react";
import { useRouter } from "next/navigation";
import useSWR from "swr";
import {
  getAuthConfig,
  loginWithPassword,
  loginWithToken,
  oidcLoginURL,
} from "@/lib/api/auth";
import { ApiError } from "@/lib/api/http";

// Roadmap Phase 10: which form this page shows depends entirely on the
// backend's active AUTH_MODE (GET /api/v1/auth/config) — token-paste for
// "token", a Keycloak redirect for "oidc". "disabled" never reaches this
// page at all (see proxy.ts, which skips the login redirect for it).
export default function LoginPage() {
  const router = useRouter();
  const { data: config } = useSWR("auth-config", getAuthConfig);
  const [token, setToken] = useState("");
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const [code, setCode] = useState("");
  const [askCode, setAskCode] = useState(false);
  const [needsProviderPage, setNeedsProviderPage] = useState(false);
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

  async function handlePasswordSubmit(e: React.FormEvent) {
    e.preventDefault();
    if (!username.trim() || !password) return;
    setBusy(true);
    setError(null);
    setNeedsProviderPage(false);
    try {
      await loginWithPassword(username.trim(), password, code.trim());
      setPassword("");
      router.replace("/");
    } catch (err) {
      setPassword("");
      if (err instanceof ApiError) {
        setError(err.message);
        setNeedsProviderPage(err.data?.code === "needs_provider_page");
      } else {
        setError("Sign-in failed.");
      }
    } finally {
      setBusy(false);
    }
  }

  // Keycloak sign-in is on when the deployment is in oidc mode, or once it was set up
  // from Settings in token mode; a pasted token stays unless it was turned off.
  const oidcOn = config?.mode === "oidc" || config?.oidc === true;
  const tokenOn = config?.mode === "token" && config?.tokenLogin !== false;
  // Vinaire's own username and password form, instead of the hop to Keycloak's page (Phase 85)
  const directOn = oidcOn && config?.oidcDirect === true;

  return (
    <div className="flex min-h-screen items-center justify-center bg-bg px-4">
      <div className="w-full max-w-md rounded-xl border border-border bg-surface p-8">
        <div className="mb-1 font-mono text-lg font-semibold">Vinaire</div>
        <div className="mb-6 text-sm text-text-muted">Sign in to continue</div>

        {oidcOn && !directOn && (
          <a
            href={oidcLoginURL()}
            className="block rounded-lg bg-accent px-3.5 py-2 text-center text-[13px] font-semibold text-white"
          >
            Sign in with Keycloak
          </a>
        )}

        {directOn && (
          <form onSubmit={handlePasswordSubmit} className="flex flex-col gap-3">
            <label htmlFor="kc-user" className="text-xs text-text-muted">
              Username
            </label>
            <input
              id="kc-user"
              value={username}
              onChange={(e) => setUsername(e.target.value)}
              autoComplete="username"
              autoFocus
              className="rounded-lg border border-border bg-surface-2 px-3 py-2 text-sm outline-none focus:border-accent"
            />
            <label htmlFor="kc-pass" className="text-xs text-text-muted">
              Password
            </label>
            <input
              id="kc-pass"
              type="password"
              value={password}
              onChange={(e) => setPassword(e.target.value)}
              autoComplete="current-password"
              className="rounded-lg border border-border bg-surface-2 px-3 py-2 text-sm outline-none focus:border-accent"
            />
            {askCode ? (
              <>
                <label htmlFor="kc-code" className="text-xs text-text-muted">
                  One-time code
                </label>
                <input
                  id="kc-code"
                  value={code}
                  onChange={(e) => setCode(e.target.value)}
                  autoComplete="one-time-code"
                  inputMode="numeric"
                  className="rounded-lg border border-border bg-surface-2 px-3 py-2 text-sm outline-none focus:border-accent"
                />
              </>
            ) : (
              <button
                type="button"
                onClick={() => setAskCode(true)}
                className="self-start text-[11px] text-text-faint underline"
              >
                I have a one-time code
              </button>
            )}
            {error && <div className="text-xs text-err">{error}</div>}
            {needsProviderPage && (
              <a
                href={oidcLoginURL()}
                className="rounded-lg border border-border px-3.5 py-2 text-center text-[12.5px] text-accent-text"
              >
                Continue on Keycloak&apos;s page
              </a>
            )}
            <button
              type="submit"
              disabled={busy || !username.trim() || !password}
              className="rounded-lg bg-accent px-3.5 py-2 text-[13px] font-semibold text-white disabled:opacity-60"
            >
              {busy ? "Signing in…" : "Sign in"}
            </button>
            {!needsProviderPage && (
              <a
                href={oidcLoginURL()}
                className="text-center text-[11px] text-text-faint underline"
              >
                Sign in on Keycloak&apos;s page instead
              </a>
            )}
          </form>
        )}

        {oidcOn && tokenOn && (
          <div className="my-4 text-center text-[11px] text-text-faint">
            or paste a token
          </div>
        )}

        {tokenOn && (
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
              autoFocus={!oidcOn}
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

        {!config && <div className="text-sm text-text-muted">Loading…</div>}
      </div>
    </div>
  );
}
