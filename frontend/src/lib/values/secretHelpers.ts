import { parse, stringify } from "yaml";
import { getResourceObject, type TableRow } from "@/lib/api/allResources";
import { createResource } from "@/lib/api/resources";
import { revealSecretKey } from "@/lib/api/secrets";
import { ApiError } from "@/lib/api/http";

// Roadmap Phase 59: doing by a click what the install page used to ask people to do by hand —
// create the Secret an install refers to. Nothing here is a new write: a Secret (and, if the
// install namespace doesn't exist yet, a Namespace) is created through the generic YAML create
// that has existed since Phase 25, as the signed-in user; a source Secret's value is read through
// the explicit, audited reveal. Values live only in these function calls, never in a cache.

// A random alphanumeric password (no characters that would need quoting in YAML, a URL or a shell).
export function randomPassword(length = 24): string {
  const alphabet = "ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz23456789";
  const bytes = new Uint8Array(length);
  crypto.getRandomValues(bytes);
  // 256 isn't a multiple of the alphabet's size; drop the few values that would bias the result
  const limit = 256 - (256 % alphabet.length);
  let out = "";
  for (const b of bytes) if (b < limit) out += alphabet[b % alphabet.length];
  return out.length === length ? out : randomPassword(length);
}

function secretYaml(
  namespace: string,
  name: string,
  data: Record<string, string>,
): string {
  return stringify({
    apiVersion: "v1",
    kind: "Secret",
    metadata: {
      name,
      namespace,
      labels: { "app.kubernetes.io/managed-by": "vinaire" },
    },
    type: "Opaque",
    stringData: data,
  });
}

// Make sure the namespace exists (an install can create it, but a Secret has to go in first).
export async function ensureNamespace(namespace: string): Promise<boolean> {
  try {
    await getResourceObject("core/v1/namespaces", "", namespace);
    return false;
  } catch (e) {
    if (!(e instanceof ApiError && e.status === 404)) throw e;
  }
  await createResource(
    stringify({
      apiVersion: "v1",
      kind: "Namespace",
      metadata: { name: namespace },
    }),
  );
  return true;
}

// Create the Secret. Refuses (rather than overwrites) one that already exists.
export async function createSecret(
  namespace: string,
  name: string,
  data: Record<string, string>,
): Promise<{ createdNamespace: boolean }> {
  const createdNamespace = await ensureNamespace(namespace);
  await createResource(secretYaml(namespace, name, data));
  return { createdNamespace };
}

// ---- credentials of a PostgreSQL that already runs ---------------------------------------

export type CredentialSource = {
  namespace: string;
  name: string;
  keys: string[];
  // what the source says about itself
  hasUsername: boolean;
  passwordKey: "password" | "postgres-password";
};

// The Secrets beside a PostgreSQL Service that look like its credentials: CloudNativePG's
// `<cluster>-app` (username, password, dbname…) and the Bitnami family's `<name>` /
// `<name>-postgresql` (password, postgres-password). Only key NAMES are read (a Secret's values
// are redacted by this API), and only in the Service's own namespace.
export async function findCredentialSources(
  serviceNamespace: string,
  serviceName: string,
  secrets: TableRow[],
): Promise<CredentialSource[]> {
  const base = serviceName.replace(
    /-(rw|ro|r|primary|pooler-rw|headless|hl|postgresql)$/,
    "",
  );
  const wanted = secrets
    .filter((r) => r.namespace === serviceNamespace || !r.namespace)
    .filter((r) => r.name === base || r.name.startsWith(`${base}-`))
    .slice(0, 8);
  const out: CredentialSource[] = [];
  for (const r of wanted) {
    try {
      const obj = await getResourceObject(
        "core/v1/secrets",
        serviceNamespace,
        r.name,
      );
      const doc = parse(obj.yaml) as { data?: Record<string, unknown> } | null;
      const keys = Object.keys(doc?.data ?? {});
      const passwordKey = keys.includes("password")
        ? "password"
        : keys.includes("postgres-password")
          ? "postgres-password"
          : null;
      if (passwordKey) {
        out.push({
          namespace: serviceNamespace,
          name: r.name,
          keys,
          hasUsername: keys.includes("username"),
          passwordKey,
        });
      }
    } catch {
      // unreadable: just not offered
    }
  }
  // the application account (CloudNativePG's -app, one with a username) before a superuser
  out.sort(
    (a, b) =>
      Number(b.hasUsername) - Number(a.hasUsername) ||
      Number(a.passwordKey === "postgres-password") -
        Number(b.passwordKey === "postgres-password"),
  );
  return out;
}

// What the database itself says about its account: the environment of the workload that runs it
// (plain values only; a key the password comes from is named, its value never read here). It tells
// the user name and database a Bitnami-style PostgreSQL was started with, and which Secret key holds
// that user's password — which a Secret alone doesn't say.
export type DatabaseHints = {
  username?: string;
  database?: string;
  passwordKey?: string;
};

type EnvVar = {
  name: string;
  value?: string;
  valueFrom?: { secretKeyRef?: { name?: string; key?: string } };
};

export async function databaseHints(
  namespace: string,
  serviceName: string,
  secretName: string,
): Promise<DatabaseHints> {
  const base = serviceName.replace(
    /-(rw|ro|r|primary|pooler-rw|headless|hl|postgresql)$/,
    "",
  );
  const candidates: [string, string][] = [
    ["apps/v1/statefulsets", base],
    ["apps/v1/statefulsets", `${base}-postgresql`],
    ["apps/v1/deployments", base],
    ["apps/v1/deployments", `${base}-postgresql`],
  ];
  for (const [kind, name] of candidates) {
    try {
      const obj = await getResourceObject(kind, namespace, name);
      const doc = parse(obj.yaml) as {
        spec?: { template?: { spec?: { containers?: { env?: EnvVar[] }[] } } };
      } | null;
      const env = (doc?.spec?.template?.spec?.containers ?? []).flatMap(
        (c) => c.env ?? [],
      );
      const val = (...names: string[]) =>
        env.find((e) => names.includes(e.name) && e.value)?.value;
      const pw = env.find(
        (e) =>
          ["POSTGRES_PASSWORD", "POSTGRESQL_PASSWORD"].includes(e.name) &&
          e.valueFrom?.secretKeyRef?.name === secretName,
      );
      const out: DatabaseHints = {
        username: val("POSTGRES_USER", "POSTGRESQL_USERNAME"),
        database: val("POSTGRES_DB", "POSTGRESQL_DATABASE"),
        passwordKey: pw?.valueFrom?.secretKeyRef?.key,
      };
      if (out.username || out.database || out.passwordKey) return out;
    } catch {
      // not that workload
    }
  }
  return {};
}

// Copy the source's credentials into a new Secret in the install namespace (revealing each value
// through the audited reveal, then creating the new Secret) and report what the form can fill in.
export async function copyCredentials(
  source: CredentialSource,
  serviceName: string,
  target: { namespace: string; name: string },
): Promise<{
  username: string;
  database: string;
  createdNamespace: boolean;
  superuser: boolean;
  // the account could not be told from the Secret or from the database's own settings
  userUnknown: boolean;
}> {
  const hints = source.hasUsername
    ? {}
    : await databaseHints(source.namespace, serviceName, source.name);
  const passwordKey =
    hints.passwordKey && source.keys.includes(hints.passwordKey)
      ? hints.passwordKey
      : source.passwordKey;
  const password = await revealSecretKey(
    source.namespace,
    source.name,
    passwordKey,
  );
  const superuser = passwordKey === "postgres-password";
  let username = superuser ? "postgres" : (hints.username ?? "");
  let database = hints.database ?? "";
  if (source.hasUsername)
    username = await revealSecretKey(source.namespace, source.name, "username");
  if (source.keys.includes("dbname"))
    database = await revealSecretKey(source.namespace, source.name, "dbname");
  const data: Record<string, string> = { password };
  if (username) data.username = username;
  const { createdNamespace } = await createSecret(
    target.namespace,
    target.name,
    data,
  );
  return {
    username,
    database,
    createdNamespace,
    superuser,
    userUnknown: !username,
  };
}

// What a credentials Secret says about its account, without creating anything: the user and the
// database (from the Secret's own keys, else from the settings of the workload that runs the
// database) and which key holds the password. Only `username` and `dbname` are revealed (audited);
// the password is never read here.
export async function accountOf(
  source: CredentialSource,
  serviceName: string,
): Promise<{
  username: string;
  database: string;
  passwordKey: string;
  superuser: boolean;
  userUnknown: boolean;
}> {
  const hints = source.hasUsername
    ? {}
    : await databaseHints(source.namespace, serviceName, source.name);
  const passwordKey =
    hints.passwordKey && source.keys.includes(hints.passwordKey)
      ? hints.passwordKey
      : source.passwordKey;
  const superuser = passwordKey === "postgres-password";
  let username = superuser ? "postgres" : (hints.username ?? "");
  let database = hints.database ?? "";
  if (source.hasUsername)
    username = await revealSecretKey(source.namespace, source.name, "username");
  if (source.keys.includes("dbname"))
    database = await revealSecretKey(source.namespace, source.name, "dbname");
  return {
    username,
    database,
    passwordKey,
    superuser,
    userUnknown: !username,
  };
}
