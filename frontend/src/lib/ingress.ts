import { parse } from "yaml";

// What an Ingress says, read out of its YAML (Roadmap Phase 89): the rules, the TLS block, the address the
// controller gave it. Pure and defensive on purpose — the object is cluster data, i.e. untrusted: every field
// is coerced to a string or ignored, and the numbers are bounded.

export type IngressBackend = {
  // a Service backend: its name and port (a number or a name, as a string)
  service?: { name: string; port: string };
  // a non-Service backend (a CRD object): "group/kind/name" — shown, never linked
  resource?: string;
};

export type IngressRoute = {
  host: string; // "" = any host
  path: string;
  pathType: string;
  backend: IngressBackend;
};

export type IngressTLS = { hosts: string[]; secretName: string };

export type IngressInfo = {
  className: string;
  addresses: string[];
  routes: IngressRoute[];
  defaultBackend?: IngressBackend;
  tls: IngressTLS[];
  // annotations worth showing (the long kubectl bookkeeping one is left out)
  annotations: Record<string, string>;
};

const MAX_ROUTES = 200;
const MAX_HOSTS = 50;
const HIDDEN_ANNOTATIONS = new Set([
  "kubectl.kubernetes.io/last-applied-configuration",
]);

const str = (v: unknown): string =>
  typeof v === "string" ? v : typeof v === "number" ? String(v) : "";
const obj = (v: unknown): Record<string, unknown> =>
  v && typeof v === "object" && !Array.isArray(v)
    ? (v as Record<string, unknown>)
    : {};
const arr = (v: unknown): unknown[] => (Array.isArray(v) ? v : []);

function backendOf(v: unknown): IngressBackend {
  const b = obj(v);
  const svc = obj(b.service);
  if (str(svc.name)) {
    const port = obj(svc.port);
    return {
      service: {
        name: str(svc.name),
        port: str(port.number) || str(port.name),
      },
    };
  }
  const res = obj(b.resource);
  if (str(res.kind) || str(res.name)) {
    const group = str(res.apiGroup);
    return {
      resource: `${group ? group + "/" : ""}${str(res.kind)}/${str(res.name)}`,
    };
  }
  return {};
}

// describeIngress parses the YAML of an Ingress; null when it isn't one (or isn't YAML).
export function describeIngress(yamlText: string): IngressInfo | null {
  let doc: unknown;
  try {
    doc = parse(yamlText);
  } catch {
    return null;
  }
  const root = obj(doc);
  if (str(root.kind) !== "Ingress") return null;
  const meta = obj(root.metadata);
  const spec = obj(root.spec);
  const status = obj(obj(root.status).loadBalancer);

  const annotations: Record<string, string> = {};
  for (const [k, v] of Object.entries(obj(meta.annotations))) {
    if (!HIDDEN_ANNOTATIONS.has(k)) annotations[k] = str(v);
  }

  const routes: IngressRoute[] = [];
  for (const rule of arr(spec.rules)) {
    const r = obj(rule);
    const host = str(r.host);
    for (const p of arr(obj(r.http).paths)) {
      if (routes.length >= MAX_ROUTES) break;
      const path = obj(p);
      routes.push({
        host,
        path: str(path.path) || "/",
        pathType: str(path.pathType),
        backend: backendOf(path.backend),
      });
    }
  }

  const tls: IngressTLS[] = arr(spec.tls).map((t) => {
    const e = obj(t);
    return {
      hosts: arr(e.hosts).map(str).filter(Boolean).slice(0, MAX_HOSTS),
      secretName: str(e.secretName),
    };
  });

  const addresses = arr(status.ingress)
    .map((a) => str(obj(a).ip) || str(obj(a).hostname))
    .filter(Boolean);

  const def = backendOf(spec.defaultBackend);
  return {
    className:
      str(spec.ingressClassName) ||
      str(annotations["kubernetes.io/ingress.class"]),
    addresses,
    routes,
    defaultBackend: def.service || def.resource ? def : undefined,
    tls,
    annotations,
  };
}

// Is the host served over TLS? An entry with no hosts covers every host of the Ingress.
export function hostHasTLS(host: string, tls: IngressTLS[]): boolean {
  return tls.some((t) => t.hosts.length === 0 || t.hosts.includes(host));
}

// The address to open for a route: https when its host is in a TLS block, else http. "" when the rule has no host
// (it answers to any name, so there is nothing to build an address from).
export function routeUrl(route: IngressRoute, tls: IngressTLS[]): string {
  if (!route.host || route.host.includes("*")) return "";
  const scheme = hostHasTLS(route.host, tls) ? "https" : "http";
  const path =
    route.pathType === "Exact" || route.path !== "/" ? route.path : "";
  return `${scheme}://${route.host}${path}`;
}

// "name:port" — what is worth copying for a backend
export function backendLabel(b: IngressBackend): string {
  if (b.service)
    return b.service.port
      ? `${b.service.name}:${b.service.port}`
      : b.service.name;
  return b.resource ?? "";
}
