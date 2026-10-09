import { parse } from "yaml";
import type { FormState, KV } from "./converters";

// Roadmap Phase 63: filling the Service / Ingress / HTTPRoute forms from an object that is
// already in the cluster — an application (Deployment, StatefulSet, DaemonSet) or a Service —
// instead of typing its name, labels and ports. Pure functions over the object's YAML, as read
// (as the signed-in user) through the All-resources API; the result is ordinary form state the
// person can still edit, and the write is the usual reviewed apply.

type Obj = Record<string, unknown>;

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

function load(yaml: string): Obj | null {
  try {
    const doc = parse(yaml);
    return doc && typeof doc === "object" && !Array.isArray(doc)
      ? (doc as Obj)
      : null;
  } catch {
    return null;
  }
}

function labelRows(v: unknown): KV[] {
  return v && typeof v === "object" && !Array.isArray(v)
    ? Object.entries(v as Record<string, unknown>).map(([key, value]) => ({
        key,
        value: asStr(value),
      }))
    : [];
}

// What a Service in front of an application needs: the labels its pods carry (the workload's
// own selector, which is what pins the pods) and the ports its containers declare.
export type ServiceFill = {
  name: string;
  namespace: string;
  selector: KV[];
  ports: KV[];
  // true when the workload declares no container port, so the form's default is kept
  noPorts: boolean;
};

export function serviceFromWorkload(yaml: string): ServiceFill | null {
  const o = load(yaml);
  if (!o) return null;
  const name = asStr(at(o, "metadata", "name"));
  if (!name) return null;
  // Deployments, StatefulSets and DaemonSets all select with spec.selector.matchLabels;
  // fall back to the pod template's labels if a manifest has no selector.
  let selector = labelRows(at(o, "spec", "selector", "matchLabels"));
  if (selector.length === 0) {
    selector = labelRows(at(o, "spec", "template", "metadata", "labels"));
  }
  const containers = at(o, "spec", "template", "spec", "containers");
  const seen = new Set<string>();
  const ports: KV[] = [];
  if (Array.isArray(containers)) {
    for (const c of containers) {
      const list = at(c, "ports");
      if (!Array.isArray(list)) continue;
      for (const p of list) {
        const n = asStr(at(p, "containerPort"));
        if (n && !seen.has(n)) {
          seen.add(n);
          ports.push({ key: n, value: n });
        }
      }
    }
  }
  return {
    name,
    namespace: asStr(at(o, "metadata", "namespace")),
    selector,
    ports,
    noPorts: ports.length === 0,
  };
}

// A Service as a backend: its name, namespace and the ports it serves.
export type BackendFill = {
  name: string;
  namespace: string;
  ports: { port: number; name: string }[];
};

export function backendFromService(yaml: string): BackendFill | null {
  const o = load(yaml);
  if (!o) return null;
  const name = asStr(at(o, "metadata", "name"));
  if (!name) return null;
  const list = at(o, "spec", "ports");
  const ports: BackendFill["ports"] = [];
  if (Array.isArray(list)) {
    for (const p of list) {
      const n = Number(asStr(at(p, "port")));
      if (Number.isInteger(n) && n > 0) {
        ports.push({ port: n, name: asStr(at(p, "name")) });
      }
    }
  }
  return { name, namespace: asStr(at(o, "metadata", "namespace")), ports };
}

// Merge a fill into the form's state, only touching what it knows.
export function applyServiceFill(state: FormState, f: ServiceFill): FormState {
  const next: FormState = {
    ...state,
    name: f.name,
    selector: f.selector,
  };
  if (f.namespace) next.namespace = f.namespace;
  if (!f.noPorts) next.ports = f.ports;
  return next;
}

export function applyBackendFill(
  state: FormState,
  f: BackendFill,
  port?: number,
): FormState {
  const next: FormState = { ...state, serviceName: f.name };
  if (f.namespace) next.namespace = f.namespace;
  const chosen = port ?? f.ports[0]?.port;
  if (chosen !== undefined) next.servicePort = String(chosen);
  // a route or ingress named after what it exposes, unless the person already named it
  if (!(typeof state.name === "string" && state.name.trim())) {
    next.name = f.name;
  }
  return next;
}
