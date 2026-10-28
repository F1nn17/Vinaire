// Which of the Keycloaks found in the cluster is the one Vinaire signs people in with (Roadmap Phase 106)? The theme is
// mounted into a workload, which restarts it — so only the matching one may be touched, never every Keycloak found.

export type KeycloakInstance = {
  namespace: string;
  name: string;
  kind: string;
};

// the host of the saved issuer, lower case, without port; "" if it is not a URL
export function issuerHost(issuer: string | undefined): string {
  if (!issuer) return "";
  try {
    return new URL(issuer).hostname.toLowerCase();
  } catch {
    return "";
  }
}

// An in-cluster address names the instance: name, name.ns, name.ns.svc[.cluster.local]. A public host (an Ingress)
// cannot be matched to a workload from here, so it matches nothing.
export function matchesIssuer(f: KeycloakInstance, host: string): boolean {
  if (!host) return false;
  const base = `${f.name}.${f.namespace}`.toLowerCase();
  const name = f.name.toLowerCase();
  return (
    host === name ||
    host === base ||
    host === `${base}.svc` ||
    host.startsWith(`${base}.svc.`)
  );
}

export type Pick = {
  // the instance to update, or null when the person has to choose
  target: KeycloakInstance | null;
  // what the person may choose from (empty when target is certain)
  choices: KeycloakInstance[];
  // how the target was decided
  reason: "issuer" | "only-one" | "choose" | "none";
};

export function pickKeycloak(
  found: KeycloakInstance[],
  issuer: string | undefined,
): Pick {
  if (found.length === 0) return { target: null, choices: [], reason: "none" };
  const host = issuerHost(issuer);
  const matched = found.filter((f) => matchesIssuer(f, host));
  if (matched.length === 1)
    return { target: matched[0], choices: [], reason: "issuer" };
  if (matched.length === 0 && found.length === 1)
    return { target: found[0], choices: [], reason: "only-one" };
  return {
    target: null,
    choices: matched.length > 1 ? matched : found,
    reason: "choose",
  };
}
