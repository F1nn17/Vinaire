import { parse, stringify } from "yaml";
import { apiFetch } from "@/lib/api/http";
import { getResourceObject, type ResourceTable } from "@/lib/api/allResources";
import { dryRunResource, updateResource } from "@/lib/api/resources";
import { revealSecretKey } from "@/lib/api/secrets";

// A password Secret doesn't always say whose password it is: Helm charts and older installs keep the
// login in the environment of the workload that uses the Secret (POSTGRES_USER=app …), not in the
// Secret. This finds it there — read-only, as the signed-in user — and can copy it into the Secret
// so that everything needed to connect is in one place. Nothing here is a write path of its own:
// the Secret is updated through the generic YAML update (Phase 25).

type Env = {
  name?: string;
  value?: string;
  valueFrom?: { secretKeyRef?: { name?: string } };
};
type Container = {
  env?: Env[];
  envFrom?: { secretRef?: { name?: string } }[];
};
type Workload = {
  metadata?: { name?: string };
  spec?: { template?: { spec?: { containers?: Container[] } } };
};

export type LoginHint = {
  kind: "StatefulSet" | "Deployment" | "DaemonSet";
  workload: string;
  username?: { env: string; value: string };
  database?: { env: string; value: string };
};

const KINDS: [LoginHint["kind"], string][] = [
  ["StatefulSet", "apps/v1/statefulsets"],
  ["Deployment", "apps/v1/deployments"],
  ["DaemonSet", "apps/v1/daemonsets"],
];

const USER = /(^|_)(USER|USERNAME|LOGIN)(_|$)/;
const DB = /(^|_)(DB|DBNAME|DATABASE)(_|$)|_DB$/;
const NOT_A_NAME = /PASS|SECRET|TOKEN|HOST|PORT|URL|URI|KEY/;

// Does this Secret hold a password but no login? (Then the question "where is the login?" is real.)
export function lacksLogin(keys: { label: string }[]): boolean {
  const has = (re: RegExp) => keys.some((k) => re.test(k.label));
  return has(/pass/i) && !has(/^(user(name)?|login|admin-user|root-user)$/i);
}

function inspect(
  w: Workload,
  secret: string,
): Omit<LoginHint, "kind" | "workload"> | null {
  const containers = w.spec?.template?.spec?.containers ?? [];
  const uses = containers.some(
    (c) =>
      (c.env ?? []).some((e) => e.valueFrom?.secretKeyRef?.name === secret) ||
      (c.envFrom ?? []).some((f) => f.secretRef?.name === secret),
  );
  if (!uses) return null;
  const plain = containers
    .flatMap((c) => c.env ?? [])
    .filter((e) => e.name && e.value && !NOT_A_NAME.test(e.name));
  const user = plain.find((e) => USER.test(e.name!));
  const db = plain.find((e) => DB.test(e.name!));
  return {
    username: user ? { env: user.name!, value: user.value! } : undefined,
    database: db ? { env: db.name!, value: db.value! } : undefined,
  };
}

// Look where the Secret is used: the workload that owns it (a Secret made by the image wizard is
// owned by its StatefulSet), else the workloads of its namespace that refer to it.
export async function findLoginHint(
  namespace: string,
  secret: string,
): Promise<LoginHint | null> {
  const obj = await getResourceObject("core/v1/secrets", namespace, secret);
  const doc = parse(obj.yaml) as {
    metadata?: { ownerReferences?: { kind?: string; name?: string }[] };
  } | null;
  const candidates: [LoginHint["kind"], string, string][] = [];
  for (const o of doc?.metadata?.ownerReferences ?? []) {
    const k = KINDS.find(([kind]) => kind === o.kind);
    if (k && o.name) candidates.push([k[0], k[1], o.name]);
  }
  if (candidates.length === 0) {
    for (const [kind, key] of KINDS) {
      try {
        const table = await apiFetch<ResourceTable>(
          `/api/v1/api-resources/${key.split("/").map(encodeURIComponent).join("/")}/objects?namespace=${encodeURIComponent(namespace)}`,
        );
        for (const r of table.rows.slice(0, 15))
          candidates.push([kind, key, r.name]);
      } catch {
        // a kind the caller can't list is just not searched
      }
    }
  }
  for (const [kind, key, name] of candidates) {
    try {
      const w = parse(
        (await getResourceObject(key, namespace, name)).yaml,
      ) as Workload | null;
      const found = w && inspect(w, secret);
      if (found && (found.username || found.database)) {
        return { kind, workload: name, ...found };
      }
    } catch {
      // unreadable: skip it
    }
  }
  return null;
}

// Copy the login found into the Secret as `username` (and `dbname`). Every existing value is read
// through the audited reveal and written back unchanged, because an update replaces the object.
export async function addLoginToSecret(
  namespace: string,
  secret: string,
  hint: LoginHint,
): Promise<void> {
  const obj = await getResourceObject("core/v1/secrets", namespace, secret);
  const doc = parse(obj.yaml) as {
    type?: string;
    data?: Record<string, unknown>;
    metadata?: {
      labels?: Record<string, string>;
      annotations?: Record<string, string>;
      ownerReferences?: unknown[];
    };
  } | null;
  const keys = Object.keys(doc?.data ?? {});
  const values: Record<string, string> = {};
  for (const k of keys) values[k] = await revealSecretKey(namespace, secret, k);
  if (hint.username && !("username" in values))
    values.username = hint.username.value;
  if (hint.database && !("dbname" in values))
    values.dbname = hint.database.value;

  const annotations = { ...(doc?.metadata?.annotations ?? {}) };
  delete annotations["kubectl.kubernetes.io/last-applied-configuration"];
  const yaml = stringify(
    {
      apiVersion: "v1",
      kind: "Secret",
      metadata: {
        name: secret,
        namespace,
        ...(doc?.metadata?.labels ? { labels: doc.metadata.labels } : {}),
        ...(Object.keys(annotations).length ? { annotations } : {}),
        ...(doc?.metadata?.ownerReferences?.length
          ? { ownerReferences: doc.metadata.ownerReferences }
          : {}),
      },
      type: doc?.type ?? "Opaque",
      stringData: values,
    },
    { version: "1.1" },
  );
  const dry = await dryRunResource(yaml, "update");
  await updateResource(yaml, dry.resourceVersion ?? "");
}
