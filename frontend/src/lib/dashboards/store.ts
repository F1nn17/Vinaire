import {
  createResource,
  dryRunResource,
  updateResource,
} from "@/lib/api/resources";
import { getConfigMaps } from "@/lib/api/configmaps";
import { type Dashboard, dashboardIdRe, validateDashboard } from "./model";
import { toGrafana } from "./grafana";

// A person's own dashboards are ConfigMaps (the key below holds the dashboard as JSON), written
// through Phase 25's generic YAML create/update as the signed-in user — there is no endpoint of its
// own, and the cluster's RBAC decides who may save. The same goes for "Add to Grafana": a ConfigMap
// with the label Grafana's dashboard sidecar looks for.

export const DASHBOARD_KEY = "vinaire-dashboard.json";
const NAME_PREFIX = "vinaire-dashboard-";
const NS_RE = /^[a-z0-9]([-a-z0-9]{0,61}[a-z0-9])?$/;

export type StoredDashboard = Dashboard & { namespace: string };

export async function listCustomDashboards(): Promise<StoredDashboard[]> {
  const all = await getConfigMaps();
  const out: StoredDashboard[] = [];
  for (const cm of all) {
    const key = cm.keys.find((k) => k.label === DASHBOARD_KEY);
    if (!key) continue;
    try {
      const raw = JSON.parse(key.value) as Dashboard;
      const d: Dashboard = {
        ...raw,
        id: cm.name.startsWith(NAME_PREFIX)
          ? cm.name.slice(NAME_PREFIX.length)
          : raw.id,
        group: "Custom",
      };
      if (validateDashboard(d) === "") out.push({ ...d, namespace: cm.namespace });
    } catch {
      // not ours, or damaged: leave it out
    }
  }
  return out.sort((a, b) => a.title.localeCompare(b.title));
}

function configMapYaml(
  name: string,
  namespace: string,
  labels: Record<string, string>,
  data: Record<string, unknown>,
): string {
  // JSON is valid YAML, so every value is written as a JSON string and nothing typed can alter
  // the structure of the document
  const lines = [
    "apiVersion: v1",
    "kind: ConfigMap",
    "metadata:",
    `  name: ${JSON.stringify(name)}`,
    `  namespace: ${JSON.stringify(namespace)}`,
    "  labels:",
    ...Object.entries(labels).map(
      ([k, v]) => `    ${JSON.stringify(k)}: ${JSON.stringify(v)}`,
    ),
    "data:",
    ...Object.entries(data).map(
      ([k, v]) =>
        `  ${JSON.stringify(k)}: ${JSON.stringify(JSON.stringify(v, null, 2))}`,
    ),
  ];
  return lines.join("\n") + "\n";
}

// create, or replace one that is already there (named by the same id) under the version just read
async function createOrUpdate(yaml: string): Promise<void> {
  try {
    await createResource(yaml);
  } catch (e) {
    if (!/already exists/i.test(e instanceof Error ? e.message : String(e)))
      throw e;
    const dry = await dryRunResource(yaml, "update");
    await updateResource(yaml, dry.resourceVersion ?? "");
  }
}

function checkNamespace(namespace: string) {
  if (!NS_RE.test(namespace))
    throw new Error("That is not a valid namespace name.");
}

export async function saveCustomDashboard(
  namespace: string,
  d: Dashboard,
): Promise<void> {
  checkNamespace(namespace);
  const bad = validateDashboard(d);
  if (bad) throw new Error(bad);
  const { id, title, description, variables, panels, requires } = d;
  const body = { id, title, description, variables, panels, requires };
  await createOrUpdate(
    configMapYaml(
      `${NAME_PREFIX}${id}`,
      namespace,
      {
        "vinaire.io/dashboard": "true",
        "app.kubernetes.io/managed-by": "vinaire",
      },
      { [DASHBOARD_KEY]: body },
    ),
  );
}

export async function addToGrafana(
  namespace: string,
  d: Dashboard,
): Promise<string> {
  checkNamespace(namespace);
  if (!dashboardIdRe.test(d.id))
    throw new Error("The dashboard's name is not valid.");
  const name = `grafana-dashboard-${d.id}`.slice(0, 63).replace(/-+$/, "");
  await createOrUpdate(
    configMapYaml(
      name,
      namespace,
      { grafana_dashboard: "1", "app.kubernetes.io/managed-by": "vinaire" },
      { [`${d.id}.json`]: toGrafana(d) },
    ),
  );
  return name;
}
