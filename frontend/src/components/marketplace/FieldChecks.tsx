"use client";

import useSWR from "swr";
import { parse } from "yaml";
import { t } from "@/lib/i18n/translate";
import { getResourceObject } from "@/lib/api/allResources";
import { ApiError } from "@/lib/api/http";
import type { FieldValues, RecipeOption } from "@/lib/values/recipe";

// Roadmap Phase 59: quiet reads that tell, before the install, whether what the person typed is
// there — the database Service and its ready pods (when the host is an in-cluster name), and the
// Secret with its keys. Read as the signed-in user through the All-resources API (a Secret's
// values are redacted there; only the key names are used). A read that is refused or fails shows
// nothing: "can't tell" is not "missing".

type Result = { tone: "ok" | "warn"; text: string } | null;

const svcHost = /^([a-z0-9-]+)\.([a-z0-9-]+)\.svc(\.cluster\.local)?$/;

function isNotFound(e: unknown) {
  return e instanceof ApiError && e.status === 404;
}

async function checkService(host: string): Promise<Result> {
  const m = svcHost.exec(host);
  if (!m) return null;
  const [, name, ns] = m;
  try {
    await getResourceObject("core/v1/services", ns, name);
  } catch (e) {
    return isNotFound(e)
      ? {
          tone: "warn",
          text: t("No Service “{s}” in namespace {ns}.", { s: name, ns }),
        }
      : null;
  }
  try {
    const ep = await getResourceObject("core/v1/endpoints", ns, name);
    const doc = parse(ep.yaml) as {
      subsets?: { addresses?: unknown[] }[];
    } | null;
    const ready = (doc?.subsets ?? []).reduce(
      (n, s) => n + (s.addresses?.length ?? 0),
      0,
    );
    return ready > 0
      ? {
          tone: "ok",
          text: t("Service found, with {n} ready pod(s) behind it.", {
            n: ready,
          }),
        }
      : {
          tone: "warn",
          text: t("The Service exists but no pod is ready behind it."),
        };
  } catch {
    return { tone: "ok", text: t("Service found.") };
  }
}

async function checkSecret(
  namespace: string,
  name: string,
  keys: string[],
): Promise<Result> {
  try {
    const obj = await getResourceObject("core/v1/secrets", namespace, name);
    const doc = parse(obj.yaml) as { data?: Record<string, unknown> } | null;
    const have = Object.keys(doc?.data ?? {});
    const missing = keys.filter((k) => k && !have.includes(k));
    return missing.length === 0
      ? { tone: "ok", text: t("Secret found, with the key(s) needed.") }
      : {
          tone: "warn",
          text: t("The Secret has no key {k} (it has: {have}).", {
            k: missing.join(", "),
            have: have.join(", ") || "—",
          }),
        };
  } catch (e) {
    return isNotFound(e)
      ? {
          tone: "warn",
          text: t("No Secret “{s}” in namespace {ns} — create it first.", {
            s: name,
            ns: namespace,
          }),
        }
      : null;
  }
}

export function FieldChecks({
  option,
  fields,
  namespace,
}: {
  option: RecipeOption;
  fields: FieldValues;
  namespace: string;
}) {
  const host = (fields.host ?? "").trim();
  const hostCheck = option.fields?.some((f) => f.key === "host") ?? false;
  const secretField = option.fields?.find(
    (f) => f.kind === "secret" && f.required,
  );
  const secret = secretField ? (fields[secretField.key] ?? "").trim() : "";
  // the key fields next to a Secret field name the keys that must be in it
  const keyNames = (option.fields ?? [])
    .filter((f) => /Key$/.test(f.key))
    .map((f) => (fields[f.key] ?? "").trim());

  const svc = useSWR(
    hostCheck && svcHost.test(host) ? ["recipe-svc", host] : null,
    () => checkService(host),
    { revalidateOnFocus: false },
  );
  const sec = useSWR(
    secret && namespace
      ? ["recipe-secret", namespace, secret, keyNames.join(",")]
      : null,
    () => checkSecret(namespace, secret, keyNames),
    { revalidateOnFocus: false },
  );
  const lines = [svc.data, sec.data].filter(Boolean) as NonNullable<Result>[];
  if (lines.length === 0) return null;
  return (
    <div className="flex flex-col gap-1">
      {lines.map((l, i) => (
        <div key={i} className={l.tone === "ok" ? "text-ok" : "text-warn"}>
          {l.tone === "ok" ? "✓" : "⚠"} {l.text}
        </div>
      ))}
    </div>
  );
}
