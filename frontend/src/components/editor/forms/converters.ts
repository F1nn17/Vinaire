import { parse, stringify } from "yaml";

// Roadmap Phase 31: form state <-> manifest, entirely client-side. The form is
// only a friendlier way to assemble the same YAML Phase 25's dry-run/apply
// already handles.
//
// Going YAML -> form is only allowed when the form can represent the manifest
// *exactly*: read the YAML into form state, serialize that state back, and
// require the result to equal the original. Anything the form doesn't cover
// (extra fields, sidecar containers, valueFrom env, ...) fails that round
// trip, so switching back is refused instead of silently dropping data.

export type KV = { key: string; value: string };
export type FormValue = string | KV[];
export type FormState = Record<string, FormValue>;
type Manifest = Record<string, unknown>;

type Converter = {
  defaults: () => FormState;
  toManifest: (s: FormState) => Manifest;
  fromManifest: (m: Manifest) => FormState;
  validate: (s: FormState) => string | null;
};

const str = (s: FormState, k: string): string => {
  const v = s[k];
  return typeof v === "string" ? v.trim() : "";
};
const rows = (s: FormState, k: string): KV[] => {
  const v = s[k];
  return Array.isArray(v) ? v.filter((r) => r.key.trim() !== "") : [];
};
const toMap = (list: KV[]): Record<string, string> =>
  Object.fromEntries(list.map((r) => [r.key.trim(), r.value]));

function at(obj: unknown, ...path: (string | number)[]): unknown {
  let cur: unknown = obj;
  for (const p of path) {
    if (cur === null || typeof cur !== "object") return undefined;
    cur = (cur as Record<string | number, unknown>)[p];
  }
  return cur;
}
const asStr = (v: unknown): string =>
  typeof v === "string" || typeof v === "number" ? String(v) : "";
const asMap = (v: unknown): KV[] =>
  v && typeof v === "object" && !Array.isArray(v)
    ? Object.entries(v as Record<string, unknown>).map(([key, value]) => ({
        key,
        value: asStr(value),
      }))
    : [];

function nonNegativeInt(label: string, v: string): string | null {
  if (v === "") return null;
  return /^\d+$/.test(v) ? null : `${label} must be a whole number`;
}

const deployment: Converter = {
  defaults: () => ({
    name: "",
    namespace: "default",
    image: "",
    replicas: "1",
    containerPort: "",
    env: [],
    cpuRequest: "",
    memoryRequest: "",
    cpuLimit: "",
    memoryLimit: "",
    node: "",
  }),
  toManifest: (s) => {
    const name = str(s, "name");
    const container: Manifest = { name, image: str(s, "image") };
    if (str(s, "containerPort") !== "") {
      container.ports = [{ containerPort: Number(str(s, "containerPort")) }];
    }
    const env = rows(s, "env");
    if (env.length > 0) {
      container.env = env.map((r) => ({ name: r.key.trim(), value: r.value }));
    }
    const res: Manifest = {};
    const requests: Record<string, string> = {};
    const limits: Record<string, string> = {};
    if (str(s, "cpuRequest")) requests.cpu = str(s, "cpuRequest");
    if (str(s, "memoryRequest")) requests.memory = str(s, "memoryRequest");
    if (str(s, "cpuLimit")) limits.cpu = str(s, "cpuLimit");
    if (str(s, "memoryLimit")) limits.memory = str(s, "memoryLimit");
    if (Object.keys(requests).length) res.requests = requests;
    if (Object.keys(limits).length) res.limits = limits;
    if (Object.keys(res).length) container.resources = res;

    const podSpec: Manifest = { containers: [container] };
    if (str(s, "node")) {
      podSpec.nodeSelector = { "kubernetes.io/hostname": str(s, "node") };
    }
    const spec: Manifest = {
      selector: { matchLabels: { app: name } },
      template: { metadata: { labels: { app: name } }, spec: podSpec },
    };
    if (str(s, "replicas") !== "") spec.replicas = Number(str(s, "replicas"));
    return {
      apiVersion: "apps/v1",
      kind: "Deployment",
      metadata: { name, namespace: str(s, "namespace") },
      spec,
    };
  },
  fromManifest: (m) => {
    const c = ["spec", "template", "spec", "containers", 0] as const;
    const env = at(m, ...c, "env");
    return {
      name: asStr(at(m, "metadata", "name")),
      namespace: asStr(at(m, "metadata", "namespace")),
      image: asStr(at(m, ...c, "image")),
      replicas: asStr(at(m, "spec", "replicas")),
      containerPort: asStr(at(m, ...c, "ports", 0, "containerPort")),
      env: Array.isArray(env)
        ? env.map((e) => ({
            key: asStr(at(e, "name")),
            value: asStr(at(e, "value")),
          }))
        : [],
      cpuRequest: asStr(at(m, ...c, "resources", "requests", "cpu")),
      memoryRequest: asStr(at(m, ...c, "resources", "requests", "memory")),
      cpuLimit: asStr(at(m, ...c, "resources", "limits", "cpu")),
      memoryLimit: asStr(at(m, ...c, "resources", "limits", "memory")),
      node: asStr(
        at(
          m,
          "spec",
          "template",
          "spec",
          "nodeSelector",
          "kubernetes.io/hostname",
        ),
      ),
    };
  },
  validate: (s) =>
    (!str(s, "name") && "Name is required") ||
    (!str(s, "namespace") && "Namespace is required") ||
    (!str(s, "image") && "Image is required") ||
    nonNegativeInt("Replicas", str(s, "replicas")) ||
    nonNegativeInt("Container port", str(s, "containerPort")) ||
    null,
};

function keyValueKind(
  kind: "ConfigMap" | "Secret",
  dataField: "data" | "stringData",
): Converter {
  return {
    defaults: () => ({ name: "", namespace: "default", data: [] }),
    toManifest: (s) => {
      const m: Manifest = {
        apiVersion: "v1",
        kind,
        metadata: { name: str(s, "name"), namespace: str(s, "namespace") },
      };
      if (kind === "Secret") m.type = "Opaque";
      const data = rows(s, "data");
      if (data.length > 0) m[dataField] = toMap(data);
      return m;
    },
    fromManifest: (m) => ({
      name: asStr(at(m, "metadata", "name")),
      namespace: asStr(at(m, "metadata", "namespace")),
      data: asMap(at(m, dataField)),
    }),
    validate: (s) =>
      (!str(s, "name") && "Name is required") ||
      (!str(s, "namespace") && "Namespace is required") ||
      null,
  };
}

const namespace: Converter = {
  defaults: () => ({ name: "" }),
  toManifest: (s) => ({
    apiVersion: "v1",
    kind: "Namespace",
    metadata: { name: str(s, "name") },
  }),
  fromManifest: (m) => ({ name: asStr(at(m, "metadata", "name")) }),
  validate: (s) => (!str(s, "name") ? "Name is required" : null),
};

const converters: Record<string, Converter> = {
  deployment,
  configmap: keyValueKind("ConfigMap", "data"),
  secret: keyValueKind("Secret", "stringData"),
  namespace,
};

export function hasForm(resourceType: string): boolean {
  return resourceType in converters;
}

export function defaultFormState(resourceType: string): FormState {
  return converters[resourceType].defaults();
}

export function validateForm(
  resourceType: string,
  state: FormState,
): string | null {
  return converters[resourceType].validate(state);
}

export function formToYaml(resourceType: string, state: FormState): string {
  return stringify(converters[resourceType].toManifest(state));
}

function deepEqual(a: unknown, b: unknown): boolean {
  if (a === b) return true;
  if (typeof a !== "object" || typeof b !== "object" || !a || !b) return false;
  if (Array.isArray(a) !== Array.isArray(b)) return false;
  const ka = Object.keys(a);
  const kb = Object.keys(b);
  if (ka.length !== kb.length) return false;
  return ka.every((k) =>
    deepEqual(
      (a as Record<string, unknown>)[k],
      (b as Record<string, unknown>)[k],
    ),
  );
}

export type YamlToFormResult =
  { ok: true; state: FormState } | { ok: false; reason: string };

export function yamlToForm(
  resourceType: string,
  text: string,
): YamlToFormResult {
  const conv = converters[resourceType];
  let doc: unknown;
  try {
    doc = parse(text);
  } catch {
    return { ok: false, reason: "The YAML isn't valid — fix it first." };
  }
  if (!doc || typeof doc !== "object" || Array.isArray(doc)) {
    return { ok: false, reason: "The YAML isn't a single resource." };
  }
  const state = conv.fromManifest(doc as Manifest);
  if (!deepEqual(conv.toManifest(state), doc)) {
    return {
      ok: false,
      reason:
        "This YAML is too complex to edit as a form — it has fields the form doesn't cover.",
    };
  }
  return { ok: true, state };
}
