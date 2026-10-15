"use client";

import { useState } from "react";
import { friendlyErrorMessage } from "@/lib/api/errors";
import { useResourceTable } from "@/lib/api/allResources";
import { t } from "@/lib/i18n/translate";
import {
  canGenerateSecret,
  parseYamlValue,
  preferredNeed,
  setPathInValues,
  suggestSecretName,
  type RenderProblem,
  type ValueNeed,
} from "@/lib/values/required";
import { createSecret, randomPassword } from "@/lib/values/secretHelpers";

const input =
  "rounded-lg border border-border bg-surface px-3 py-1.75 font-mono text-xs outline-none placeholder:text-text-faint";

// Roadmap Phase 71: the chart itself refuses to render until a value is given (`required`, `fail`) —
// Helm's raw sentence used to appear only when the install failed. The preflight now says which of
// the chart's values it means, and this asks for them: the answer goes into the values, the
// preflight runs again, and the next missing value (if any) shows up here. Nothing is written to
// the cluster by this panel except, when asked, a Secret created through the generic YAML create.
export function RequiredValues({
  problem,
  hasRecipe,
  namespace,
  release,
  values,
  onValues,
}: {
  problem: RenderProblem;
  hasRecipe?: boolean;
  namespace: string;
  release: string;
  values: string;
  onValues: (v: string) => void;
}) {
  const needs = problem.needs ?? [];
  const [pick, setPick] = useState(() => preferredNeed(needs));
  // the chart can ask for a different value next time: start from its preferred one again
  const [forMessage, setForMessage] = useState(problem.message);
  if (forMessage !== problem.message) {
    setForMessage(problem.message);
    setPick(preferredNeed(needs));
  }
  const need = needs[Math.min(pick, needs.length - 1)];

  return (
    <div
      className="flex flex-col gap-3 rounded-lg border border-warn/50 bg-surface-2 p-4 text-xs"
      data-testid="required-values"
    >
      <div className="font-semibold text-warn">
        This chart needs a value before it can be installed
      </div>
      <div className="whitespace-pre-wrap text-text-muted">
        {problem.message}
      </div>
      {hasRecipe && (
        <div className="text-text-faint">
          The guided setup above covers the usual cases for this chart.
        </div>
      )}

      {needs.length === 0 ? (
        <div className="text-text-muted">
          The chart stopped with the message above. If it names a value, set it
          under &ldquo;Your values&rdquo; (the &ldquo;Chart defaults&rdquo; tab
          lists them); if it is about the release name or the namespace, change
          those above.
        </div>
      ) : (
        <>
          {needs.length > 1 && (
            <fieldset className="flex flex-col gap-1.5">
              <legend className="mb-1 text-text-muted">
                Give it in one of these ways:
              </legend>
              {needs.map((n, i) => (
                <label key={n.path} className="flex items-start gap-2">
                  <input
                    type="radio"
                    name="required-way"
                    checked={i === pick}
                    onChange={() => setPick(i)}
                    className="mt-0.5"
                  />
                  <span>
                    <span className="font-mono text-text">{n.path}</span>
                    <span className="text-text-faint"> — {describe(n)}</span>
                  </span>
                </label>
              ))}
            </fieldset>
          )}
          {need && (
            <NeedForm
              // a fresh form for each value, so nothing typed for one is kept for another
              key={need.path}
              need={need}
              namespace={namespace}
              release={release}
              values={values}
              onValues={onValues}
            />
          )}
        </>
      )}
    </div>
  );
}

function describe(n: ValueNeed): string {
  if (n.secretRef)
    return t("the name of a Secret (the password stays in the cluster)");
  if (n.sensitive)
    return t("a password typed here (kept in Helm's release record)");
  if (/(file|path)$/i.test(n.path))
    return t("a file path inside the container");
  return t("a value");
}

function NeedForm({
  need,
  namespace,
  release,
  values,
  onValues,
}: {
  need: ValueNeed;
  namespace: string;
  release: string;
  values: string;
  onValues: (v: string) => void;
}) {
  const kids = need.children ?? [];
  const isRef =
    need.kind === "object" && kids.includes("name") && kids.includes("key");
  const [text, setText] = useState("");
  const [secretName, setSecretName] = useState(() =>
    suggestSecretName(release, need.path),
  );
  const [secretKey, setSecretKey] = useState("password");
  const [busy, setBusy] = useState(false);
  const [msg, setMsg] = useState<{ ok: boolean; text: string } | null>(null);
  const [show, setShow] = useState(false);

  const secrets = useResourceTable(
    need.secretRef && namespace ? "core/v1/secrets" : null,
    namespace,
  );
  const secretNames = (secrets.data?.rows ?? []).map((r) => r.name);

  function put(value: unknown) {
    onValues(setPathInValues(values, need.path, value));
    setMsg({ ok: true, text: t("Put into the values — checking again…") });
  }

  async function createAndUse() {
    if (!namespace) return;
    setBusy(true);
    setMsg(null);
    try {
      await createSecret(namespace, secretName.trim(), {
        [secretKey.trim()]: randomPassword(),
      });
      put({ name: secretName.trim(), key: secretKey.trim() });
      setMsg({
        ok: true,
        text: t(
          "Secret {name} created in {ns} with a generated password and put into the values. You can read it later on the Secrets page (reading a value is audited).",
          { name: secretName.trim(), ns: namespace },
        ),
      });
    } catch (e) {
      setMsg({ ok: false, text: friendlyErrorMessage(e) });
    } finally {
      setBusy(false);
    }
  }

  const nameOk = /^[a-z0-9]([-a-z0-9.]{0,251}[a-z0-9])?$/.test(
    secretName.trim(),
  );
  const keyOk = /^[-._a-zA-Z0-9]{1,253}$/.test(secretKey.trim());

  let body: React.ReactNode;
  if (isRef) {
    body = (
      <>
        <div className="grid gap-2 md:grid-cols-2">
          <label className="flex flex-col gap-1 text-text-muted">
            Secret name
            <input
              aria-label="Secret name"
              list="required-secrets"
              value={secretName}
              onChange={(e) => setSecretName(e.target.value.trim())}
              className={input}
            />
            <datalist id="required-secrets">
              {secretNames.map((n) => (
                <option key={n} value={n} />
              ))}
            </datalist>
          </label>
          <label className="flex flex-col gap-1 text-text-muted">
            Key in the Secret
            <input
              aria-label="Secret key"
              value={secretKey}
              onChange={(e) => setSecretKey(e.target.value.trim())}
              className={input}
            />
          </label>
        </div>
        {need.guessed && (
          <div className="text-[11px] text-text-faint">
            The chart doesn&apos;t spell out the shape; this is the usual one
            for a Secret reference (name and key).
          </div>
        )}
        <div className="flex flex-wrap gap-2">
          {canGenerateSecret(need) && (
            <button
              type="button"
              disabled={busy || !nameOk || !keyOk || !namespace}
              onClick={() => void createAndUse()}
              className="rounded-lg bg-accent px-3 py-1.5 font-medium text-white disabled:opacity-50"
            >
              {busy
                ? "Creating…"
                : "Create the Secret with a generated password and use it"}
            </button>
          )}
          <button
            type="button"
            disabled={!nameOk || !keyOk}
            onClick={() =>
              put({ name: secretName.trim(), key: secretKey.trim() })
            }
            className="rounded-lg border border-border bg-surface px-3 py-1.5 disabled:opacity-50"
          >
            Use a Secret that already exists
          </button>
        </div>
        {canGenerateSecret(need) && (
          <div className="text-[11px] text-text-faint">
            A generated password is only right when nothing outside the cluster
            has to agree with it. If this is the password of a database that
            already runs, use the Secret that holds it.
          </div>
        )}
      </>
    );
  } else if (need.kind === "bool") {
    body = (
      <div className="flex gap-2">
        <button
          type="button"
          onClick={() => put(true)}
          className="rounded-lg border border-border bg-surface px-3 py-1.5"
        >
          Turn on
        </button>
        <button
          type="button"
          onClick={() => put(false)}
          className="rounded-lg border border-border bg-surface px-3 py-1.5"
        >
          Turn off
        </button>
      </div>
    );
  } else if (need.kind === "object" || need.kind === "list") {
    const parsed = text.trim() ? parseYamlValue(text) : null;
    body = (
      <>
        <textarea
          aria-label={`Value of ${need.path}`}
          value={text}
          rows={4}
          onChange={(e) => setText(e.target.value)}
          placeholder={need.kind === "list" ? "- item" : "key: value"}
          className={input}
        />
        {parsed && "error" in parsed && (
          <div className="text-err">{parsed.error}</div>
        )}
        <div>
          <button
            type="button"
            disabled={!parsed || "error" in parsed}
            onClick={() => parsed && "value" in parsed && put(parsed.value)}
            className="rounded-lg border border-border bg-surface px-3 py-1.5 disabled:opacity-50"
          >
            Put into the values
          </button>
        </div>
      </>
    );
  } else if (need.secretRef) {
    // a plain string that names a Secret
    body = (
      <>
        <label className="flex flex-col gap-1 text-text-muted">
          Secret name
          <input
            aria-label="Secret name"
            list="required-secrets"
            value={secretName}
            onChange={(e) => setSecretName(e.target.value.trim())}
            className={input}
          />
          <datalist id="required-secrets">
            {secretNames.map((n) => (
              <option key={n} value={n} />
            ))}
          </datalist>
        </label>
        <div>
          <button
            type="button"
            disabled={!nameOk}
            onClick={() => put(secretName.trim())}
            className="rounded-lg border border-border bg-surface px-3 py-1.5 disabled:opacity-50"
          >
            Use this Secret
          </button>
        </div>
      </>
    );
  } else {
    body = (
      <>
        <div className="flex gap-2">
          <input
            aria-label={`Value of ${need.path}`}
            type={need.sensitive && !show ? "password" : "text"}
            autoComplete="new-password"
            value={text}
            onChange={(e) => setText(e.target.value)}
            inputMode={need.kind === "number" ? "numeric" : undefined}
            className={`${input} flex-1`}
          />
          {need.sensitive && (
            <>
              <button
                type="button"
                onClick={() => {
                  setText(randomPassword());
                  setShow(true);
                }}
                className="rounded-lg border border-border bg-surface px-3 py-1.5"
              >
                Generate
              </button>
            </>
          )}
        </div>
        {need.sensitive && (
          <div className="text-[11px] text-text-faint">
            A password typed here is written into the values and so into
            Helm&apos;s release record in the cluster. Where the chart can read
            it from a Secret instead, that is the better way.
          </div>
        )}
        <div>
          <button
            type="button"
            disabled={!text.trim()}
            onClick={() =>
              put(
                need.kind === "number" && !Number.isNaN(Number(text))
                  ? Number(text)
                  : text,
              )
            }
            className="rounded-lg border border-border bg-surface px-3 py-1.5 disabled:opacity-50"
          >
            Put into the values
          </button>
        </div>
      </>
    );
  }

  return (
    <div className="flex flex-col gap-2">
      {body}
      {msg && <div className={msg.ok ? "text-ok" : "text-err"}>{msg.text}</div>}
    </div>
  );
}
