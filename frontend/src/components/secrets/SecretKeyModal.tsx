"use client";

import { useState } from "react";
import { t } from "@/lib/i18n/translate";
import { friendlyErrorMessage } from "@/lib/api/errors";
import { setSecretKeys } from "@/lib/values/loginHints";

const keyRe = /^[-._a-zA-Z0-9]{1,253}$/;
const inputClass =
  "w-full rounded-lg border border-border bg-surface-2 px-3 py-1.75 font-mono text-xs outline-none";

// Add a key to a Secret, or change the value of one it has — the login (`username`), a database
// name, a host. The other keys are kept as they are. The key and the value are typed here; the
// update goes through the generic YAML update (Phase 25), as the signed-in user.
export function SecretKeyModal({
  namespace,
  name,
  existing,
  initialKey = "",
  onClose,
  onSaved,
}: {
  namespace: string;
  name: string;
  existing: string[];
  initialKey?: string;
  onClose: () => void;
  onSaved: () => void;
}) {
  const [key, setKey] = useState(initialKey);
  const [value, setValue] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const replaces = existing.includes(key);
  const invalid = !keyRe.test(key) || value === "" || value.length > 100_000;

  async function save() {
    setBusy(true);
    setError(null);
    try {
      await setSecretKeys(namespace, name, { [key]: value });
      onSaved();
      onClose();
    } catch (e) {
      setError(friendlyErrorMessage(e));
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 p-4">
      <div className="flex w-full max-w-md flex-col gap-3 rounded-xl border border-border bg-surface p-5 text-sm">
        <div className="font-semibold">
          {t("Add or change a key in {name}", { name })}
        </div>
        <div className="flex flex-wrap gap-1.5 text-[11px]">
          {["username", "password", "dbname", "host", "port"].map((k) => (
            <button
              key={k}
              type="button"
              onClick={() => setKey(k)}
              className={`rounded-md border px-2 py-0.5 font-mono ${
                key === k
                  ? "border-accent text-accent-text"
                  : "border-border text-text-muted hover:border-accent"
              }`}
            >
              {k}
            </button>
          ))}
        </div>
        <label className="flex flex-col gap-1 text-xs">
          <span className="font-semibold">{t("Key")}</span>
          <input
            autoFocus
            className={inputClass}
            value={key}
            onChange={(e) => setKey(e.target.value)}
            placeholder="username"
          />
        </label>
        <label className="flex flex-col gap-1 text-xs">
          <span className="font-semibold">{t("Value")}</span>
          <input
            className={inputClass}
            value={value}
            onChange={(e) => setValue(e.target.value)}
            autoComplete="off"
          />
        </label>
        {replaces && (
          <div className="text-xs text-warn">
            {t("This key already exists: its value will be replaced.")}
          </div>
        )}
        <div className="text-[11px] text-text-muted">
          {t(
            "The other keys of the Secret stay as they are. Whatever already uses the Secret (a database, an application) picks up a changed value only after it restarts.",
          )}
        </div>
        {error && <div className="text-xs text-err">{error}</div>}
        <div className="flex justify-end gap-2">
          <button
            type="button"
            onClick={onClose}
            className="rounded-lg border border-border px-4 py-2 text-xs"
          >
            {t("Cancel")}
          </button>
          <button
            type="button"
            disabled={invalid || busy}
            onClick={() => void save()}
            className="rounded-lg bg-accent px-4 py-2 text-xs font-semibold text-white disabled:opacity-50"
          >
            {busy ? t("Saving…") : t("Save")}
          </button>
        </div>
      </div>
    </div>
  );
}
