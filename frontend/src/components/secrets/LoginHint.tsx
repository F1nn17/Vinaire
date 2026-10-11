"use client";

import { useState } from "react";
import useSWR from "swr";
import { t } from "@/lib/i18n/translate";
import { friendlyErrorMessage } from "@/lib/api/errors";
import {
  addLoginToSecret,
  findLoginHint,
  lacksLogin,
} from "@/lib/values/loginHints";

// "Where is the login?" — shown under a Secret that holds a password but no user name: finds it in
// the workload that uses the Secret and offers to copy it in.
export function LoginHint({
  namespace,
  name,
  keys,
  onChanged,
}: {
  namespace: string;
  name: string;
  keys: { label: string }[];
  onChanged: () => void;
}) {
  const missing = lacksLogin(keys);
  const hint = useSWR(
    missing ? ["login-hint", namespace, name] : null,
    () => findLoginHint(namespace, name),
    { revalidateOnFocus: false },
  );
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  if (!missing || !hint.data) return null;
  const h = hint.data;

  async function add() {
    setBusy(true);
    setError(null);
    try {
      await addLoginToSecret(namespace, name, h);
      onChanged();
      void hint.mutate(undefined, { revalidate: false });
    } catch (e) {
      setError(friendlyErrorMessage(e));
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="flex flex-col gap-2 rounded-[10px] border border-accent/40 bg-surface p-4 text-xs">
      <div className="font-semibold">
        {t("The login isn't stored in this Secret")}
      </div>
      <div className="text-text-muted">
        {t("It is set on the {kind} {name}:", {
          kind: h.kind,
          name: h.workload,
        })}
      </div>
      <div className="flex flex-col gap-1 font-mono">
        {h.username && (
          <div>
            {t("User")}: <b>{h.username.value}</b>{" "}
            <span className="text-text-faint">({h.username.env})</span>
          </div>
        )}
        {h.database && (
          <div>
            {t("Database")}: <b>{h.database.value}</b>{" "}
            <span className="text-text-faint">({h.database.env})</span>
          </div>
        )}
      </div>
      <div>
        <button
          type="button"
          disabled={busy}
          onClick={() => void add()}
          className="rounded-lg border border-border bg-surface-2 px-3 py-1.5 hover:border-accent disabled:opacity-60"
        >
          {busy
            ? t("Adding…")
            : t("Add them to this Secret (as username and dbname)")}
        </button>
      </div>
      {error && <div className="text-err">{error}</div>}
    </div>
  );
}
