"use client";

import { useEffect, useRef, useState } from "react";
import useSWR from "swr";
import { t } from "@/lib/i18n/translate";
import {
  getResourceObject,
  useResourceTable,
  type TableRow,
} from "@/lib/api/allResources";
import {
  applyOption,
  patchesFor,
  defaultFields,
  fieldsError,
  type FieldValues,
  type Recipe,
  type RecipeField,
  type RecipeOption,
  type RecipeSection,
} from "@/lib/values/recipe";
import { backendFromService } from "@/components/editor/forms/fromApp";
import { FieldChecks } from "./FieldChecks";
import { friendlyErrorMessage } from "@/lib/api/errors";
import { revealSecretKey } from "@/lib/api/secrets";
import {
  copyCredentials,
  createSecret,
  findCredentialSources,
  randomPassword,
  type CredentialSource,
} from "@/lib/values/secretHelpers";

// Roadmap Phase 59: the questions a recipe asks before an install — which database, how to reach
// the app — as a few choices and fields that become values in the "Your values" YAML. Nothing is
// written to the cluster here: it only edits the values text the person can still read and change.
// A password is never typed: a field of kind "secret" names an existing Secret.

const input =
  "rounded-lg border border-border bg-surface-2 px-2.5 py-1.5 font-mono text-xs outline-none placeholder:text-text-faint";

type Applied = { optionId: string; paths: string[][] };

export function RecipePanel({
  recipe,
  installNamespace,
  release,
  values,
  onValues,
}: {
  recipe: Recipe;
  installNamespace: string;
  release: string;
  values: string;
  onValues: (next: string) => void;
}) {
  // per section: what is applied to the values now (so choosing another option can take it back)
  const [applied, setApplied] = useState<Record<string, Applied>>(() =>
    // options that are written into the values on open (see the effect below) count as applied
    Object.fromEntries(
      recipe.sections.flatMap((s) => {
        const o = s.options.find((x) => x.default && x.auto);
        return o
          ? [
              [
                s.id,
                { optionId: o.id, paths: patchesFor(o, {}).map((p) => p.path) },
              ],
            ]
          : [];
      }),
    ),
  );
  const autoDone = useRef(false);
  useEffect(() => {
    if (autoDone.current) return;
    autoDone.current = true;
    let text = values;
    for (const s of recipe.sections) {
      const o = s.options.find((x) => x.default && x.auto);
      if (o) text = applyOption(text, [], o, {}).text;
    }
    if (text !== values) onValues(text);
    // once, when the panel opens
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);
  const [selected, setSelected] = useState<Record<string, string>>(() =>
    Object.fromEntries(
      recipe.sections.map((s) => [
        s.id,
        s.options.find((o) => o.default)?.id ?? s.options[0].id,
      ]),
    ),
  );
  const [fields, setFields] = useState<Record<string, FieldValues>>({});

  function fieldsOf(section: RecipeSection, option: RecipeOption) {
    return fields[`${section.id}/${option.id}`] ?? defaultFields(option);
  }

  function apply(section: RecipeSection, option: RecipeOption) {
    const prev = applied[section.id]?.paths ?? [];
    const r = applyOption(values, prev, option, fieldsOf(section, option));
    onValues(r.text);
    setApplied({
      ...applied,
      [section.id]: { optionId: option.id, paths: r.paths },
    });
  }

  function choose(section: RecipeSection, option: RecipeOption) {
    setSelected({ ...selected, [section.id]: option.id });
    // an option with nothing to type is applied at once (which also takes the previous one back)
    if ((option.fields ?? []).length === 0) {
      const prev = applied[section.id]?.paths ?? [];
      const r = applyOption(values, prev, option, {});
      onValues(r.text);
      setApplied({
        ...applied,
        [section.id]: { optionId: option.id, paths: r.paths },
      });
    }
  }

  return (
    <div className="flex flex-col gap-4 rounded-lg border border-accent/40 bg-surface p-4 text-xs">
      <div>
        <div className="text-[13px] font-semibold">{t("Guided setup")}</div>
        {recipe.summary && (
          <div className="mt-1 text-text-muted">{recipe.summary}</div>
        )}
      </div>

      {recipe.guide && recipe.guide.length > 0 && (
        <details className="rounded-lg border border-border bg-surface-2 p-3">
          <summary className="cursor-pointer text-text-muted">
            {t("How this install works")}
          </summary>
          <div className="mt-2 flex flex-col gap-2 text-text-muted">
            {recipe.guide.map((p, i) => (
              <p key={i}>{p}</p>
            ))}
          </div>
        </details>
      )}

      {recipe.sections.map((section) => {
        const option =
          section.options.find((o) => o.id === selected[section.id]) ??
          section.options[0];
        const isApplied = applied[section.id]?.optionId === option.id;
        const f = fieldsOf(section, option);
        const err = fieldsError(option, f);
        return (
          <div key={section.id} className="flex flex-col gap-2">
            <div className="font-semibold">{section.title}</div>
            {section.help && (
              <div className="text-text-faint">{section.help}</div>
            )}
            <div className="flex flex-col gap-1.5">
              {section.options.map((o) => (
                <label
                  key={o.id}
                  className="flex items-start gap-2 rounded-lg border border-border bg-surface-2 px-3 py-2"
                >
                  <input
                    type="radio"
                    name={`recipe-${section.id}`}
                    checked={option.id === o.id}
                    onChange={() => choose(section, o)}
                    className="mt-0.5"
                  />
                  <span className="flex flex-col gap-0.5">
                    <span>{o.title}</span>
                    {o.note && (
                      <span className="text-text-faint">{o.note}</span>
                    )}
                  </span>
                </label>
              ))}
            </div>

            {(option.fields ?? []).length > 0 && (
              <div className="flex flex-col gap-2 rounded-lg border border-border p-3">
                {section.discover === "postgres" && (
                  <PostgresFinder
                    installNamespace={installNamespace}
                    secretName={`${release || "app"}-database`}
                    onPick={(host, port) =>
                      setFields({
                        ...fields,
                        [`${section.id}/${option.id}`]: {
                          ...f,
                          host,
                          ...(port ? { port } : {}),
                        },
                      })
                    }
                    onFill={(extra) =>
                      setFields({
                        ...fields,
                        [`${section.id}/${option.id}`]: { ...f, ...extra },
                      })
                    }
                  />
                )}
                {option.fields!.map((fld) => (
                  <FieldInput
                    key={fld.key}
                    field={fld}
                    value={f[fld.key] ?? ""}
                    namespace={installNamespace}
                    release={release}
                    fields={f}
                    onChange={(v) =>
                      setFields({
                        ...fields,
                        [`${section.id}/${option.id}`]: { ...f, [fld.key]: v },
                      })
                    }
                    onFill={(extra) =>
                      setFields({
                        ...fields,
                        [`${section.id}/${option.id}`]: { ...f, ...extra },
                      })
                    }
                  />
                ))}
                <FieldChecks
                  option={option}
                  fields={f}
                  namespace={installNamespace}
                />
                <div className="flex items-center gap-3">
                  <button
                    type="button"
                    disabled={!!err}
                    onClick={() => apply(section, option)}
                    className="rounded-lg bg-accent px-3 py-1.5 font-semibold text-white disabled:opacity-50"
                  >
                    {isApplied
                      ? t("Update the values")
                      : t("Put into the values")}
                  </button>
                  {err ? (
                    <span className="text-warn">{err}</span>
                  ) : isApplied ? (
                    <span className="text-ok">
                      {t("Applied to “Your values”.")}
                    </span>
                  ) : null}
                </div>
              </div>
            )}
          </div>
        );
      })}
    </div>
  );
}

function FieldInput({
  field,
  value,
  namespace,
  release,
  fields,
  onChange,
  onFill,
}: {
  field: RecipeField;
  value: string;
  namespace: string;
  release: string;
  fields: FieldValues;
  onChange: (v: string) => void;
  onFill: (extra: FieldValues) => void;
}) {
  const [genBusy, setGenBusy] = useState(false);
  const [genMsg, setGenMsg] = useState<{ ok: boolean; text: string } | null>(
    null,
  );

  // "create this Secret for me": a generated password, in the keys the recipe names
  async function generate() {
    const g = field.generate;
    if (!g || !namespace) return;
    const secretName = value.trim() || `${release || "app"}-admin`;
    if (g.envFile) {
      // MinIO's operator reads its root credentials from an environment file in the Secret
      setGenBusy(true);
      setGenMsg(null);
      try {
        await createSecret(namespace, secretName, {
          [g.envFile]: `export MINIO_ROOT_USER="${g.user ?? "admin"}"\nexport MINIO_ROOT_PASSWORD="${randomPassword()}"\n`,
        });
        onFill({ [field.key]: secretName });
        setGenMsg({
          ok: true,
          text: t(
            "Secret {name} created in {ns} with a generated password. You can read it later on the Secrets page (reading a value is audited).",
            { name: secretName, ns: namespace },
          ),
        });
      } catch (e) {
        setGenMsg({ ok: false, text: friendlyErrorMessage(e) });
      } finally {
        setGenBusy(false);
      }
      return;
    }
    if (g.acl && g.passwordKey && g.user) {
      // Redis ACL: the operator signs in as "default" with passwordKey's value; applications use the
      // named user. Both passwords are generated here and live only in this Secret.
      setGenBusy(true);
      setGenMsg(null);
      try {
        const adminPw = randomPassword();
        const userPw = randomPassword();
        await createSecret(namespace, secretName, {
          [g.passwordKey]: adminPw,
          [`${g.user}-password`]: userPw,
          [g.acl]: `user default on >${adminPw} ~* &* +@all
user ${g.user} on >${userPw} ~* &* +@all
`,
        });
        onFill({ [field.key]: secretName });
        setGenMsg({
          ok: true,
          text: t(
            "Secret {name} created in {ns} with a generated password. You can read it later on the Secrets page (reading a value is audited).",
            { name: secretName, ns: namespace },
          ),
        });
      } catch (e) {
        setGenMsg({ ok: false, text: friendlyErrorMessage(e) });
      } finally {
        setGenBusy(false);
      }
      return;
    }
    const pwKey = (
      g.passwordKey ??
      fields[g.passwordKeyField ?? ""] ??
      ""
    ).trim();
    const userKey = g.user
      ? (g.userKey ?? fields[g.userKeyField ?? ""] ?? "").trim()
      : "";
    if (!pwKey || (g.user && !userKey)) {
      setGenMsg({ ok: false, text: t("Fill in the key names first.") });
      return;
    }
    setGenBusy(true);
    setGenMsg(null);
    try {
      const data: Record<string, string> = { [pwKey]: randomPassword() };
      if (g.user) data[userKey] = g.user;
      await createSecret(namespace, secretName, data);
      onFill({ [field.key]: secretName });
      setGenMsg({
        ok: true,
        text: t(
          "Secret {name} created in {ns} with a generated password. You can read it later on the Secrets page (reading a value is audited).",
          { name: secretName, ns: namespace },
        ),
      });
    } catch (e) {
      setGenMsg({ ok: false, text: friendlyErrorMessage(e) });
    } finally {
      setGenBusy(false);
    }
  }

  // "create this Secret from what I paste" (a licence), or copy one found in another namespace
  const [pasted, setPasted] = useState("");
  const [pasteBusy, setPasteBusy] = useState(false);
  const [pasteMsg, setPasteMsg] = useState<{
    ok: boolean;
    text: string;
  } | null>(null);
  const everywhere = useResourceTable(
    field.paste ? "core/v1/secrets" : null,
    "",
  );
  const secretName = value.trim() || field.default || "";
  const elsewhere = field.paste
    ? (everywhere.data?.rows ?? []).filter(
        (r) => r.name === secretName && (r.namespace ?? "") !== namespace,
      )
    : [];

  async function storePasted() {
    const g = field.paste;
    if (!g || !namespace || !secretName) return;
    const text = pasted.trim();
    if (g.prefix && !text.startsWith(g.prefix)) {
      setPasteMsg({ ok: false, text: g.hint ?? t("That doesn't look right.") });
      return;
    }
    setPasteBusy(true);
    setPasteMsg(null);
    try {
      await createSecret(namespace, secretName, { [g.key]: text });
      setPasted("");
      onFill({ [field.key]: secretName });
      setPasteMsg({
        ok: true,
        text: t("Secret {name} created in {ns}.", {
          name: secretName,
          ns: namespace,
        }),
      });
    } catch (e) {
      setPasteMsg({ ok: false, text: friendlyErrorMessage(e) });
    } finally {
      setPasteBusy(false);
    }
  }

  // copy a Secret that already holds it, from another namespace: read through the audited reveal,
  // written straight into the new Secret, never shown
  async function copyFrom(ns: string) {
    const g = field.paste;
    if (!g || !namespace || !secretName) return;
    setPasteBusy(true);
    setPasteMsg(null);
    try {
      const text = await revealSecretKey(ns, secretName, g.key);
      await createSecret(namespace, secretName, { [g.key]: text });
      onFill({ [field.key]: secretName });
      setPasteMsg({
        ok: true,
        text: t("Secret {name} created in {ns}.", {
          name: secretName,
          ns: namespace,
        }),
      });
    } catch (e) {
      setPasteMsg({ ok: false, text: friendlyErrorMessage(e) });
    } finally {
      setPasteBusy(false);
    }
  }

  const secrets = useResourceTable(
    field.kind === "secret" && namespace ? "core/v1/secrets" : null,
    namespace,
  );
  const classes = useResourceTable(
    field.kind === "ingressclass"
      ? "networking.k8s.io/v1/ingressclasses"
      : null,
    "",
  );
  const listId = `recipe-list-${field.key}`;
  const options =
    field.kind === "secret"
      ? (secrets.data?.rows ?? []).map((r) => r.name)
      : field.kind === "ingressclass"
        ? (classes.data?.rows ?? []).map((r) => r.name)
        : [];
  return (
    <label className="flex flex-col gap-1">
      <span className="text-text-muted">
        {field.label}
        {field.required ? " *" : ""}
      </span>
      <input
        value={value}
        list={options.length > 0 ? listId : undefined}
        placeholder={field.placeholder}
        inputMode={field.kind === "number" ? "numeric" : undefined}
        onChange={(e) => onChange(e.target.value)}
        className={input}
      />
      {options.length > 0 && (
        <datalist id={listId}>
          {options.map((o) => (
            <option key={o} value={o} />
          ))}
        </datalist>
      )}
      {field.help && <span className="text-text-faint">{field.help}</span>}
      {field.paste && (
        <span className="flex flex-col gap-1.5">
          {elsewhere.length > 0 && (
            <span className="flex flex-wrap items-center gap-2">
              <span className="text-text-muted">
                {t("Found in this cluster")}:
              </span>
              {elsewhere.map((r) => (
                <button
                  key={`${r.namespace}/${r.name}`}
                  type="button"
                  disabled={pasteBusy || !namespace}
                  onClick={(e) => {
                    e.preventDefault();
                    void copyFrom(r.namespace ?? "");
                  }}
                  className="rounded-lg border border-border px-3 py-1.5 hover:bg-surface"
                >
                  {t("Copy {ns}/{name} here", {
                    ns: r.namespace ?? "",
                    name: r.name,
                  })}
                </button>
              ))}
            </span>
          )}
          <textarea
            value={pasted}
            onChange={(e) => setPasted(e.target.value)}
            placeholder={t("Paste the licence here")}
            rows={3}
            autoComplete="off"
            spellCheck={false}
            className={`${input} resize-y`}
          />
          <button
            type="button"
            disabled={pasteBusy || !namespace || pasted.trim() === ""}
            onClick={(e) => {
              e.preventDefault();
              void storePasted();
            }}
            className="w-fit rounded-lg border border-border px-3 py-1.5 hover:bg-surface"
          >
            {pasteBusy
              ? t("Creating…")
              : t("Create the Secret from the pasted text")}
          </button>
          {pasteMsg && (
            <span className={pasteMsg.ok ? "text-ok" : "text-err"}>
              {pasteMsg.text}
            </span>
          )}
        </span>
      )}
      {field.generate && (
        <span className="flex flex-col gap-1">
          <button
            type="button"
            disabled={genBusy || !namespace}
            onClick={(e) => {
              e.preventDefault();
              void generate();
            }}
            className="w-fit rounded-lg border border-border px-3 py-1.5 hover:bg-surface"
          >
            {genBusy
              ? t("Creating…")
              : t("Create this Secret with a generated password")}
          </button>
          {genMsg && (
            <span className={genMsg.ok ? "text-ok" : "text-err"}>
              {genMsg.text}
            </span>
          )}
        </span>
      )}
    </label>
  );
}

// "Found in this cluster": Services that look like a PostgreSQL (the common Helm chart and
// CloudNativePG labels). Read as the signed-in user; picking one fills the host and port.
function PostgresFinder({
  installNamespace,
  secretName,
  onPick,
  onFill,
}: {
  installNamespace: string;
  secretName: string;
  onPick: (host: string, port: string) => void;
  onFill: (extra: FieldValues) => void;
}) {
  const a = useResourceTable(
    "core/v1/services",
    "",
    "app.kubernetes.io/name=postgresql",
  );
  const b = useResourceTable("core/v1/services", "", "cnpg.io/cluster");
  const rows: TableRow[] = [...(a.data?.rows ?? []), ...(b.data?.rows ?? [])]
    // headless and read-only/replica Services aren't where an app should connect
    .filter((r) => !/(-headless|-hl|-ro|-r|-metrics)$/.test(r.name));
  const [picked, setPicked] = useState<{ ns: string; name: string } | null>(
    null,
  );
  if (rows.length === 0) return null;

  async function pick(value: string) {
    if (!value) return;
    const [namespace, name] = value.split("|");
    setPicked({ ns: namespace, name });
    let port = "";
    try {
      const obj = await getResourceObject("core/v1/services", namespace, name);
      port = String(backendFromService(obj.yaml)?.ports[0]?.port ?? "");
    } catch {
      // the host is still useful without the port
    }
    onPick(`${name}.${namespace}.svc.cluster.local`, port);
  }

  return (
    <div className="flex flex-col gap-2">
      <label className="flex flex-col gap-1">
        <span className="text-text-muted">{t("Found in this cluster")}</span>
        <select
          aria-label="PostgreSQL found in this cluster"
          defaultValue=""
          onChange={(e) => void pick(e.target.value)}
          className={input}
        >
          <option value="">{t("Choose a PostgreSQL Service…")}</option>
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
        <CredentialsOffer
          picked={picked}
          installNamespace={installNamespace}
          secretName={secretName}
          onFill={onFill}
        />
      )}
    </div>
  );
}

// After a database is picked: look beside it for the Secret that holds its credentials and offer to
// copy it into the install namespace (as a new Secret) and fill the form in. The password is read
// through the audited reveal and goes straight into the new Secret; it is not shown or kept.
function CredentialsOffer({
  picked,
  installNamespace,
  secretName,
  onFill,
}: {
  picked: { ns: string; name: string };
  installNamespace: string;
  secretName: string;
  onFill: (extra: FieldValues) => void;
}) {
  const secrets = useResourceTable("core/v1/secrets", picked.ns);
  const sources = useSWR(
    secrets.data ? ["pg-creds", picked.ns, picked.name] : null,
    () => findCredentialSources(picked.ns, picked.name, secrets.data!.rows),
    { revalidateOnFocus: false },
  );
  const [chosen, setChosen] = useState("");
  const [busy, setBusy] = useState(false);
  const [msg, setMsg] = useState<{ ok: boolean; text: string } | null>(null);
  const list: CredentialSource[] = sources.data ?? [];
  if (list.length === 0) return null;
  const source = list.find((c) => c.name === chosen) ?? list[0];

  async function copy() {
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
        passwordSecret: secretName,
        passwordKey: "password",
        ...(r.username ? { username: r.username } : {}),
        ...(r.database ? { database: r.database } : {}),
      });
      setMsg({
        ok: !r.userUnknown,
        text:
          t("Secret {name} created in {ns} and the form filled in.", {
            name: secretName,
            ns: installNamespace,
          }) +
          (r.superuser
            ? " " +
              t(
                "These are the database's administrator credentials — for anything beyond a trial, create a dedicated user and use its credentials instead.",
              )
            : "") +
          (r.userUnknown
            ? " ⚠ " +
              t(
                "The user name couldn't be told — enter the user this password belongs to (and check the database name) before installing, or the application won't be able to sign in and the install will time out.",
              )
            : ""),
      });
    } catch (e) {
      setMsg({ ok: false, text: friendlyErrorMessage(e) });
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="flex flex-col gap-1.5 rounded-lg border border-border bg-surface-2 p-2">
      <span className="text-text-muted">{t("Its credentials")}</span>
      <div className="flex flex-wrap items-center gap-2">
        {list.length > 1 && (
          <select
            aria-label="Credentials Secret"
            value={source.name}
            onChange={(e) => setChosen(e.target.value)}
            className={input}
          >
            {list.map((c) => (
              <option key={c.name} value={c.name}>
                {c.namespace}/{c.name}
              </option>
            ))}
          </select>
        )}
        {list.length === 1 && (
          <span className="font-mono">
            {source.namespace}/{source.name}
          </span>
        )}
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
      </div>
      {msg && (
        <span className={msg.ok ? "text-ok" : "text-err"}>{msg.text}</span>
      )}
    </div>
  );
}
