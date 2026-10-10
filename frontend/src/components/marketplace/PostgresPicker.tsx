"use client";

import { useEffect, useRef, useState } from "react";
import useSWR from "swr";
import { t } from "@/lib/i18n/translate";
import {
  getResourceObject,
  useResourceTable,
  type TableRow,
} from "@/lib/api/allResources";
import { backendFromService } from "@/components/editor/forms/fromApp";
import { friendlyErrorMessage } from "@/lib/api/errors";
import {
  accountOf,
  copyCredentials,
  findCredentialSources,
  type CredentialSource,
} from "@/lib/values/secretHelpers";

const select =
  "rounded-lg border border-border bg-surface-2 px-2.5 py-1.5 font-mono text-xs outline-none";

type Fill = Record<string, string>;

// Finds the PostgreSQL that already runs in the cluster (Services of the common Helm chart and of
// CloudNativePG, read as the signed-in user) and fills the database fields of the form with it:
// address, port, and — when its credentials Secret is in the install namespace — user, database and
// the Secret itself. A Secret in another namespace is copied into the install namespace on a click
// (the password goes straight from one Secret to the other through the audited reveal; it is never
// shown). The same discovery as the Marketplace's guided setup, for the image wizard.
export function PostgresPicker({
  installNamespace,
  secretName,
  onFill,
}: {
  installNamespace: string;
  secretName: string;
  onFill: (values: Fill) => void;
}) {
  const a = useResourceTable(
    "core/v1/services",
    "",
    "app.kubernetes.io/name=postgresql",
  );
  const b = useResourceTable("core/v1/services", "", "cnpg.io/cluster");
  const loaded = !!a.data && !!b.data;
  const rows: TableRow[] = [...(a.data?.rows ?? []), ...(b.data?.rows ?? [])]
    // headless and read-only/replica Services aren't where an application should connect
    .filter((r) => !/(-headless|-hl|-ro|-r|-metrics)$/.test(r.name));
  const [picked, setPicked] = useState<{ ns: string; name: string } | null>(
    null,
  );
  const auto = useRef(false);

  async function pick(ns: string, name: string) {
    setPicked({ ns, name });
    let port = "";
    try {
      const obj = await getResourceObject("core/v1/services", ns, name);
      port = String(backendFromService(obj.yaml)?.ports[0]?.port ?? "");
    } catch {
      // the address is still useful without the port
    }
    onFill({
      dbHost: `${name}.${ns}.svc.cluster.local`,
      ...(port ? { dbPort: port } : {}),
    });
  }

  // One database found (or several: the one in the install namespace first) — take it without
  // being asked.
  useEffect(() => {
    if (!loaded || auto.current || rows.length === 0) return;
    auto.current = true;
    const first = rows.find((r) => r.namespace === installNamespace) ?? rows[0];
    void pick(first.namespace ?? "", first.name);
    // once, when the list arrives
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [loaded, rows.length]);

  if (!loaded) return null;
  if (rows.length === 0) {
    return (
      <div className="rounded-lg border border-border bg-surface-2 p-3 text-xs text-text-muted">
        {t(
          "No PostgreSQL was found in this cluster. Install one first (CloudNativePG from the Marketplace), or fill the database fields in yourself.",
        )}
      </div>
    );
  }

  return (
    <div className="flex flex-col gap-2 rounded-lg border border-border bg-surface-2 p-3 text-xs">
      <label className="flex flex-col gap-1">
        <span className="font-semibold">
          {t("PostgreSQL found in this cluster")}
        </span>
        <select
          aria-label="PostgreSQL found in this cluster"
          value={picked ? `${picked.ns}|${picked.name}` : ""}
          onChange={(e) => {
            const [ns, name] = e.target.value.split("|");
            if (ns && name) void pick(ns, name);
          }}
          className={select}
        >
          {rows.map((r) => (
            <option
              key={`${r.namespace}/${r.name}`}
              value={`${r.namespace}|${r.name}`}
            >
              {r.namespace}/{r.name}
            </option>
          ))}
        </select>
      </label>
      {picked && (
        <Credentials
          key={`${picked.ns}/${picked.name}`}
          picked={picked}
          installNamespace={installNamespace}
          secretName={secretName}
          onFill={onFill}
        />
      )}
    </div>
  );
}

function Credentials({
  picked,
  installNamespace,
  secretName,
  onFill,
}: {
  picked: { ns: string; name: string };
  installNamespace: string;
  secretName: string;
  onFill: (values: Fill) => void;
}) {
  const secrets = useResourceTable("core/v1/secrets", picked.ns);
  const sources = useSWR(
    secrets.data ? ["pg-creds", picked.ns, picked.name] : null,
    () => findCredentialSources(picked.ns, picked.name, secrets.data!.rows),
    { revalidateOnFocus: false },
  );
  const list: CredentialSource[] = sources.data ?? [];
  const [chosen, setChosen] = useState("");
  const [busy, setBusy] = useState(false);
  const [msg, setMsg] = useState<{ ok: boolean; text: string } | null>(null);
  const done = useRef(false);
  const source = list.find((c) => c.name === chosen) ?? list[0];
  const sameNamespace = picked.ns === installNamespace;

  // The Secret is already beside the install: use it where it is, no copy.
  async function applyInPlace(s: CredentialSource) {
    try {
      const acc = await accountOf(s, picked.name);
      onFill({
        dbSecret: s.name,
        dbSecretKey: acc.passwordKey,
        ...(acc.username ? { dbUser: acc.username } : {}),
        ...(acc.database ? { dbName: acc.database } : {}),
      });
      setMsg(report(acc.superuser, acc.userUnknown, s.name, false));
    } catch (e) {
      setMsg({ ok: false, text: friendlyErrorMessage(e) });
    }
  }

  useEffect(() => {
    if (done.current || !source || !sameNamespace) return;
    done.current = true;
    void applyInPlace(source);
    // once, when the Secret is found
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [source?.name, sameNamespace]);

  async function copy() {
    if (!source) return;
    if (!installNamespace) {
      setMsg({ ok: false, text: t("Enter the install namespace first.") });
      return;
    }
    setBusy(true);
    setMsg(null);
    try {
      const r = await copyCredentials(source, picked.name, {
        namespace: installNamespace,
        name: secretName,
      });
      onFill({
        dbSecret: secretName,
        dbSecretKey: "password",
        ...(r.username ? { dbUser: r.username } : {}),
        ...(r.database ? { dbName: r.database } : {}),
      });
      setMsg(report(r.superuser, r.userUnknown, secretName, true));
    } catch (e) {
      setMsg({ ok: false, text: friendlyErrorMessage(e) });
    } finally {
      setBusy(false);
    }
  }

  if (secrets.data && sources.data && list.length === 0) {
    return (
      <span className="text-text-muted">
        {t(
          "No credentials Secret was found beside it: fill the user and the Secret in yourself.",
        )}
      </span>
    );
  }
  if (!source) return null;

  return (
    <div className="flex flex-col gap-1.5">
      <span className="text-text-muted">{t("Its credentials")}</span>
      <div className="flex flex-wrap items-center gap-2">
        {list.length > 1 ? (
          <select
            aria-label="Credentials Secret"
            value={source.name}
            onChange={(e) => {
              setChosen(e.target.value);
              const s = list.find((c) => c.name === e.target.value);
              if (s && sameNamespace) void applyInPlace(s);
            }}
            className={select}
          >
            {list.map((c) => (
              <option key={c.name} value={c.name}>
                {c.namespace}/{c.name}
              </option>
            ))}
          </select>
        ) : (
          <span className="font-mono">
            {source.namespace}/{source.name}
          </span>
        )}
        {!sameNamespace && (
          <button
            type="button"
            disabled={busy}
            onClick={() => void copy()}
            className="rounded-lg border border-border px-3 py-1.5 hover:bg-surface"
          >
            {busy
              ? t("Copying…")
              : t("Copy them into {ns} as {name} and use them", {
                  ns: installNamespace || "…",
                  name: secretName,
                })}
          </button>
        )}
      </div>
      {msg && (
        <span className={msg.ok ? "text-ok" : "text-err"}>{msg.text}</span>
      )}
    </div>
  );
}

function report(
  superuser: boolean,
  userUnknown: boolean,
  secret: string,
  copied: boolean,
): { ok: boolean; text: string } {
  return {
    ok: !userUnknown,
    text:
      (copied
        ? t("Secret {name} created and the fields filled in.", { name: secret })
        : t("Using the Secret {name} as it is; the fields are filled in.", {
            name: secret,
          })) +
      (superuser
        ? " " +
          t(
            "These are the database's administrator credentials — for anything beyond a trial, create a dedicated user and use its credentials instead.",
          )
        : "") +
      (userUnknown
        ? " ⚠ " +
          t(
            "The user name couldn't be told — enter the user this password belongs to (and check the database name) before installing.",
          )
        : ""),
  };
}
