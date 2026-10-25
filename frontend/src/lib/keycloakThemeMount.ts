import { parse, stringify } from "yaml";
import { THEME_CONFIGMAP, THEME_NAME, themeVolume } from "./keycloakTheme.ts";

// Making a Keycloak that is already running pick up the "vinaire" login theme (Roadmap Phase 91): the theme is a
// ConfigMap (themeConfigMapYaml) and the Keycloak pod mounts it at /opt/keycloak/themes/vinaire. A Keycloak that Vinaire
// installed before the theme existed has no such mount; this adds it to the workload's YAML, to be applied through the
// generic YAML update (Phase 25) as the signed-in user. Pure, so it can be tested without a cluster.

type Obj = Record<string, unknown>;
const isObj = (v: unknown): v is Obj =>
  !!v && typeof v === "object" && !Array.isArray(v);

export type MountResult =
  { ok: true; yaml: string; changed: boolean } | { ok: false; problem: string };

// themeMounted says whether the workload already mounts the theme.
export function themeMounted(yamlText: string): boolean {
  return mountedIn(podOf(parseDoc(yamlText)));
}

function mountedIn(pod: Obj | null): boolean {
  const vols = pod && Array.isArray(pod.volumes) ? pod.volumes : [];
  return vols.some(
    (v) =>
      isObj(v) && isObj(v.configMap) && v.configMap.name === THEME_CONFIGMAP,
  );
}

function parseDoc(yamlText: string): unknown {
  try {
    return parse(yamlText);
  } catch {
    return undefined;
  }
}

// the pod spec of a StatefulSet or a Deployment — the very object inside `doc`, so that changing it changes `doc`
function podOf(doc: unknown): Obj | null {
  if (!isObj(doc) || (doc.kind !== "StatefulSet" && doc.kind !== "Deployment"))
    return null;
  const spec = isObj(doc.spec) ? doc.spec : {};
  const tpl = isObj(spec.template) ? spec.template : {};
  return isObj(tpl.spec) ? tpl.spec : null;
}

// withThemeMounted returns the workload's YAML with the theme volume and mount added (unchanged if already there).
export function withThemeMounted(yamlText: string): MountResult {
  const doc = parseDoc(yamlText);
  if (doc === undefined) {
    return { ok: false, problem: "the workload's YAML could not be read" };
  }
  if (
    !isObj(doc) ||
    (doc.kind !== "StatefulSet" && doc.kind !== "Deployment")
  ) {
    return {
      ok: false,
      problem: "only a StatefulSet or a Deployment can be changed here",
    };
  }
  const pod = podOf(doc);
  const containers = pod && Array.isArray(pod.containers) ? pod.containers : [];
  if (!pod || containers.length === 0 || !containers.every(isObj)) {
    return {
      ok: false,
      problem: "the workload has no container to mount the theme into",
    };
  }
  // the container that runs Keycloak: by its image, or the only one
  const cs = containers as Obj[];
  const target =
    cs.find(
      (c) => typeof c.image === "string" && c.image.includes("keycloak"),
    ) ?? (cs.length === 1 ? cs[0] : undefined);
  if (!target) {
    return {
      ok: false,
      problem: "none of the containers runs a Keycloak image",
    };
  }
  if (mountedIn(pod)) return { ok: true, yaml: yamlText, changed: false };

  const { volume, mount } = themeVolume();
  pod.volumes = [...(Array.isArray(pod.volumes) ? pod.volumes : []), volume];
  target.volumeMounts = [
    ...(Array.isArray(target.volumeMounts) ? target.volumeMounts : []),
    mount,
  ];
  return { ok: true, yaml: stringify(doc), changed: true };
}

export { THEME_NAME };
