"use client";

import { useState } from "react";
import { friendlyErrorMessage } from "@/lib/api/errors";
import {
  disableKeycloak,
  revokeSession,
  useSessions,
  setKeycloakTokenLogin,
  setupKeycloak,
  useKeycloakStatus,
  type KeycloakSetupResult,
} from "@/lib/api/keycloak";
import { ListSkeleton } from "@/components/ui/Skeleton";
import { KeycloakInstall, type InstalledKeycloak } from "./KeycloakInstall";
import { TypeToConfirmModal } from "@/components/ui/TypeToConfirmModal";

const input =
  "rounded-lg border border-border bg-surface px-3 py-1.75 font-mono text-xs outline-none placeholder:text-text-faint";

// Roadmap Phase 47: sign-in through Keycloak in one click. The administrator's
// password is sent once, in a POST body, and kept only in this form's state until
// the request returns.
export function KeycloakSetup() {
  const { data: status, error, isLoading, mutate } = useKeycloakStatus();
  const [result, setResult] = useState<KeycloakSetupResult | null>(null);
  const [removing, setRemoving] = useState(false);
  // a Keycloak just installed from the card above: the form below is filled in from it
  const [installed, setInstalled] = useState<InstalledKeycloak | null>(null);
  const [busy, setBusy] = useState(false);
  const [actionError, setActionError] = useState<string | null>(null);

  async function toggleToken(allow: boolean) {
    setBusy(true);
    setActionError(null);
    try {
      // show the choice at once; the server's answer follows
      if (status) await mutate({ ...status, tokenLogin: allow }, false);
      await setKeycloakTokenLogin(allow);
      await mutate();
    } catch (err) {
      await mutate();
      setActionError(friendlyErrorMessage(err));
    } finally {
      setBusy(false);
    }
  }

  async function remove() {
    setBusy(true);
    setActionError(null);
    try {
      await disableKeycloak();
      setRemoving(false);
      setResult(null);
      await mutate();
    } catch (err) {
      setActionError(friendlyErrorMessage(err));
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="flex max-w-3xl flex-col gap-[18px]">
      <div>
        <div className="text-[15px] font-semibold">Sign in with Keycloak</div>
        <div className="mt-1 text-xs text-text-muted">
          Gives Vinaire a login screen with user names and passwords instead of
          pasting a token. Enter a Keycloak administrator once: Vinaire creates
          a realm, a client for itself and (if you like) a first user, and
          switches the sign-in on — no restart. Pasting a token stays available
          as the way back in, until you turn it off here.
        </div>
      </div>

      {error ? (
        <div className="text-sm text-err">
          Failed to load: {friendlyErrorMessage(error)}
        </div>
      ) : isLoading || !status ? (
        <ListSkeleton rows={3} />
      ) : (
        <>
          <div className="rounded-[10px] border border-border bg-surface p-5">
            <div
              className={`text-[13px] font-semibold ${
                status.active ? "text-ok" : "text-warn"
              }`}
            >
              {status.active
                ? "Keycloak sign-in is on"
                : status.configured
                  ? "Saved, but not active"
                  : "Keycloak sign-in is off"}
            </div>
            {!status.available && status.reason && (
              <div className="mt-1.5 text-xs text-text-muted">
                {status.reason}
              </div>
            )}
            {status.configured && (
              <div className="mt-3 flex flex-col gap-1 font-mono text-[11px] text-text-muted">
                <div>Issuer: {status.issuer}</div>
                <div>Client: {status.clientId}</div>
                <div>Redirect: {status.redirectUrl}</div>
                {status.setUpBy && <div>Set up by: {status.setUpBy}</div>}
              </div>
            )}
            {status.pendingError && (
              <div className="mt-2 text-xs text-warn">
                Keycloak didn&apos;t answer when Vinaire started; it keeps
                trying. {status.pendingError}
              </div>
            )}
            {status.active && status.available && (
              <div className="mt-4 flex flex-wrap items-center gap-3">
                <label className="flex items-center gap-2 text-xs text-text-muted">
                  <input
                    type="checkbox"
                    checked={status.tokenLogin}
                    disabled={busy}
                    onChange={(e) => void toggleToken(e.target.checked)}
                  />
                  Also allow signing in with a pasted token
                </label>
                <button
                  type="button"
                  onClick={() => {
                    setActionError(null);
                    setRemoving(true);
                  }}
                  className="ml-auto rounded-lg border border-border px-3 py-1.5 text-xs text-err hover:border-err"
                >
                  Turn Keycloak sign-in off
                </button>
              </div>
            )}
            {actionError && (
              <div className="mt-2 text-xs text-err">{actionError}</div>
            )}
          </div>

          {result && <Done result={result} />}

          <SessionsList />

          {status.available && !status.active && (
            <KeycloakInstall onReady={setInstalled} />
          )}

          {status.available && (
            <SetupForm
              key={installed?.url ?? "form"}
              prefill={installed}
              reconfigure={status.configured}
              onDone={(r) => {
                setResult(r);
                void mutate();
              }}
            />
          )}
        </>
      )}

      {removing && (
        <TypeToConfirmModal
          title="Turn Keycloak sign-in off?"
          message="Vinaire forgets the saved client and secret and goes back to signing in with a pasted token. Keycloak itself — its realm, client and users — is not touched."
          expected="keycloak"
          confirmLabel="Turn off"
          busy={busy}
          error={actionError}
          onConfirm={() => void remove()}
          onCancel={() => setRemoving(false)}
        />
      )}
    </div>
  );
}

function Done({ result }: { result: KeycloakSetupResult }) {
  return (
    <div className="flex flex-col gap-3 rounded-[10px] border border-ok/40 bg-surface p-5 text-xs">
      <div className="text-[13px] font-semibold text-ok">
        Keycloak sign-in is on.
      </div>
      <ul className="list-disc pl-5 text-text-muted">
        {result.steps.map((s) => (
          <li key={s.what}>
            {s.what} — {s.status}
          </li>
        ))}
      </ul>
      <div className="rounded-lg border border-warn/40 bg-surface-2 p-3 text-text-muted">
        <b className="text-warn">One more thing, outside Vinaire.</b> Vinaire
        hands Keycloak&apos;s ID token to the Kubernetes API, so the
        cluster&apos;s API server must trust this issuer — otherwise signing in
        works, but every cluster call is a 401. Add to kube-apiserver (for k3s,
        under <code className="font-mono">kube-apiserver-arg</code> in{" "}
        <code className="font-mono">/etc/rancher/k3s/config.yaml</code>, then
        restart it):
        <pre className="mt-2 overflow-x-auto rounded bg-bg p-2 font-mono text-[11px]">
          {result.apiServerFlags.join("\n")}
        </pre>
        Then give your Keycloak users rights, for example{" "}
        <code className="font-mono">
          kubectl create clusterrolebinding alice-view
          --clusterrole=vinaire-viewer --user=oidc:alice
        </code>{" "}
        (or bind it from Vinaire&apos;s Access control page).
      </div>
    </div>
  );
}

function Hint({ children }: { children: React.ReactNode }) {
  return <span className="text-[11px] text-text-faint">{children}</span>;
}

function SetupForm({
  reconfigure,
  prefill,
  onDone,
}: {
  reconfigure: boolean;
  prefill: InstalledKeycloak | null;
  onDone: (r: KeycloakSetupResult) => void;
}) {
  const [f, setF] = useState({
    keycloakUrl: prefill?.url ?? "",
    adminRealm: "master",
    adminUser: prefill?.adminUser ?? "admin",
    adminPassword: prefill?.adminPassword ?? "",
    realm: "vinaire",
    clientId: "vinaire",
    createUser: true,
    user: "",
    userEmail: "",
    userPassword: "",
    userChangePassword: true,
    allowTokenLogin: true,
  });
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const set = (patch: Partial<typeof f>) => setF({ ...f, ...patch });

  const ready =
    f.keycloakUrl.trim() &&
    f.adminUser.trim() &&
    f.adminPassword &&
    (!f.createUser ||
      (f.user.trim() && f.userEmail.trim() && f.userPassword.length >= 8));

  async function submit(e: React.FormEvent) {
    e.preventDefault();
    setBusy(true);
    setError(null);
    try {
      const r = await setupKeycloak({
        keycloakUrl: f.keycloakUrl.trim(),
        adminRealm: f.adminRealm.trim(),
        adminUser: f.adminUser.trim(),
        adminPassword: f.adminPassword,
        realm: f.realm.trim(),
        clientId: f.clientId.trim(),
        user: f.createUser ? f.user.trim() : "",
        userEmail: f.createUser ? f.userEmail.trim() : "",
        userPassword: f.createUser ? f.userPassword : "",
        userChangePassword: f.userChangePassword,
        allowTokenLogin: f.allowTokenLogin,
      });
      // the passwords leave this form's state as soon as they were sent
      setF({ ...f, adminPassword: "", userPassword: "" });
      onDone(r);
    } catch (err) {
      setError(friendlyErrorMessage(err));
    } finally {
      setBusy(false);
    }
  }

  return (
    <form
      onSubmit={submit}
      className="flex flex-col gap-4 rounded-[10px] border border-border bg-surface p-5"
    >
      <div className="text-[13px] font-semibold">
        {reconfigure ? "Set it up again" : "Set it up"}
      </div>
      <div className="text-[11px] text-text-faint">
        No Keycloak yet? Use the card above to install one. Already have one?
        Enter its address and an administrator of it; every field below says
        where its value comes from.
      </div>
      <label className="flex flex-col gap-1.5 text-xs text-text-muted">
        Keycloak URL
        <input
          aria-label="Keycloak URL"
          placeholder="https://keycloak.example.com"
          value={f.keycloakUrl}
          onChange={(e) => set({ keycloakUrl: e.target.value })}
          className={input}
        />
        <Hint>
          {prefill
            ? "Filled in from the Keycloak installed above."
            : "The address Keycloak is reached on — one your browser and Vinaire can both open. Vinaire only talks to a Keycloak over https."}
        </Hint>
      </label>
      <div className="grid grid-cols-3 gap-3">
        <label className="flex flex-col gap-1.5 text-xs text-text-muted">
          Administrator
          <input
            aria-label="Administrator"
            value={f.adminUser}
            onChange={(e) => set({ adminUser: e.target.value })}
            className={input}
            autoComplete="off"
          />
          <Hint>
            {prefill
              ? "admin — the account the installed Keycloak was started with."
              : "A Keycloak administrator (in a Keycloak you installed yourself, the account you created for it)."}
          </Hint>
        </label>
        <label className="flex flex-col gap-1.5 text-xs text-text-muted">
          Administrator password
          <input
            aria-label="Administrator password"
            type="password"
            value={f.adminPassword}
            onChange={(e) => set({ adminPassword: e.target.value })}
            className={input}
            autoComplete="new-password"
          />
          <Hint>
            {prefill
              ? "Generated and filled in. It is also in the Secret keycloak (key admin-password) of the namespace Keycloak was installed in."
              : "Used once, to log in to Keycloak, and never stored."}
          </Hint>
        </label>
        <label className="flex flex-col gap-1.5 text-xs text-text-muted">
          Administrator&apos;s realm
          <input
            aria-label="Administrator realm"
            value={f.adminRealm}
            onChange={(e) => set({ adminRealm: e.target.value })}
            className={input}
          />
          <Hint>
            Where the administrator account lives: master, unless you changed
            it.
          </Hint>
        </label>
        <label className="flex flex-col gap-1.5 text-xs text-text-muted">
          Realm for Vinaire&apos;s users
          <input
            aria-label="Realm"
            value={f.realm}
            onChange={(e) => set({ realm: e.target.value })}
            className={input}
          />
          <Hint>
            Created if it does not exist. The people who sign in to Vinaire live
            here; keep vinaire unless you share this Keycloak.
          </Hint>
        </label>
        <label className="flex flex-col gap-1.5 text-xs text-text-muted">
          Client id
          <input
            aria-label="Client id"
            value={f.clientId}
            onChange={(e) => set({ clientId: e.target.value })}
            className={input}
          />
          <Hint>
            The client Vinaire signs in as; created if missing (confidential,
            code flow with PKCE). Its secret is stored encrypted.
          </Hint>
        </label>
      </div>

      <label className="flex items-center gap-2 text-xs text-text-muted">
        <input
          type="checkbox"
          checked={f.createUser}
          onChange={(e) => set({ createUser: e.target.checked })}
        />
        Create a first user in this realm
      </label>
      {f.createUser && (
        <div className="grid grid-cols-3 gap-3">
          <label className="flex flex-col gap-1.5 text-xs text-text-muted">
            User name
            <input
              aria-label="User name"
              value={f.user}
              onChange={(e) => set({ user: e.target.value })}
              className={input}
              autoComplete="off"
            />
          </label>
          <label className="flex flex-col gap-1.5 text-xs text-text-muted">
            E-mail
            <input
              aria-label="E-mail"
              value={f.userEmail}
              onChange={(e) => set({ userEmail: e.target.value })}
              className={input}
            />
          </label>
          <label className="flex flex-col gap-1.5 text-xs text-text-muted">
            Password (at least 8 characters)
            <input
              aria-label="User password"
              type="password"
              value={f.userPassword}
              onChange={(e) => set({ userPassword: e.target.value })}
              className={input}
              autoComplete="new-password"
            />
          </label>
          <label className="col-span-3 flex items-center gap-2 text-xs text-text-muted">
            <input
              type="checkbox"
              checked={f.userChangePassword}
              onChange={(e) => set({ userChangePassword: e.target.checked })}
            />
            Ask the user to change the password at first sign-in
          </label>
        </div>
      )}

      <label className="flex items-center gap-2 text-xs text-text-muted">
        <input
          type="checkbox"
          checked={f.allowTokenLogin}
          onChange={(e) => set({ allowTokenLogin: e.target.checked })}
        />
        Keep signing in with a pasted token available (recommended until you
        have checked that Keycloak sign-in works)
      </label>

      <div className="text-[11px] text-text-faint">
        The administrator&apos;s password is used once, to log in to Keycloak,
        and is never stored. What is kept is the new client&apos;s secret,
        encrypted.
      </div>
      {error && <div className="text-xs text-err">{error}</div>}
      <div className="flex justify-end">
        <button
          type="submit"
          disabled={busy || !ready}
          className="rounded-lg bg-accent px-4 py-2 text-[12.5px] font-semibold text-white disabled:opacity-50"
        >
          {busy ? "Setting up…" : "Set up Keycloak sign-in"}
        </button>
      </div>
    </form>
  );
}

// Who is signed in, and a way to end a session.
function SessionsList() {
  const { data, error, mutate } = useSessions();
  const [busy, setBusy] = useState<string | null>(null);
  const [err, setErr] = useState<string | null>(null);

  async function end(ref: string) {
    setBusy(ref);
    setErr(null);
    try {
      await revokeSession(ref);
      await mutate();
    } catch (e) {
      setErr(friendlyErrorMessage(e));
    } finally {
      setBusy(null);
    }
  }

  if (error || !data) return null;
  return (
    <div className="rounded-[10px] border border-border bg-surface p-5">
      <div className="text-[13px] font-semibold">Signed-in sessions</div>
      <div className="mt-1 text-[11px] text-text-faint">
        {data.shared
          ? "Sessions are kept sealed in a Secret, shared by every replica, and survive a restart."
          : "Sessions are kept in this pod's memory: a restart signs everyone out, and only one replica can run."}
      </div>
      {data.sessions.length === 0 ? (
        <div className="mt-3 text-xs text-text-muted">No sessions.</div>
      ) : (
        <table className="mt-3 w-full text-xs">
          <thead className="text-left text-text-faint">
            <tr>
              <th className="py-1 font-normal">Who</th>
              <th className="py-1 font-normal">Until</th>
              <th />
            </tr>
          </thead>
          <tbody>
            {data.sessions.map((s) => (
              <tr key={s.ref} className="border-t border-border">
                <td className="py-1.5 font-mono">
                  {s.username || s.owner}
                  {s.current && (
                    <span className="ml-2 rounded bg-surface-2 px-1.5 py-0.5 text-[10px] text-accent-text">
                      this session
                    </span>
                  )}
                </td>
                <td className="py-1.5 text-text-muted">
                  {new Date(s.expiresAt).toLocaleString()}
                </td>
                <td className="py-1.5 text-right">
                  <button
                    type="button"
                    disabled={s.current || busy === s.ref}
                    onClick={() => void end(s.ref)}
                    className="rounded-lg border border-border px-2.5 py-1 text-[11px] text-err hover:border-err disabled:opacity-40"
                  >
                    End session
                  </button>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
      {err && <div className="mt-2 text-xs text-err">{err}</div>}
    </div>
  );
}
