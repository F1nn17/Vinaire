import useSWR from "swr";
import { parseDocument, stringify } from "yaml";
import { useCurrentCluster } from "@/lib/clusterStore";
import { t } from "@/lib/i18n/translate";
import { apiFetch } from "./http";
import { getNamespace } from "./namespaces";
import { createResource, dryRunResource, updateResource } from "./resources";
import { createBinding, deleteRoleBinding } from "./rbacActions";

// Projects (Roadmap Phase 53). A project is a set of namespaces labelled
// vinaire.io/project=<name> — it lives in the cluster, not in Vinaire. Reading is the
// backend's /projects; every change below is made with calls that already exist and are
// already audited: YAML apply (Phase 25) for labels, namespaces and quotas, and the
// role-binding endpoints (Phase 44) for members. The cluster's RBAC decides each one.

export const PROJECT_LABEL = "vinaire.io/project";

export type QuotaRow = {
  resource: string;
  used: string;
  hard: string;
  pct: number;
  color: "ok" | "warn" | "err";
};

export type ProjectRow = {
  name: string;
  namespaces: string[];
  pods: number | null;
  quota: QuotaRow[];
  members: number;
  href: string;
};

export type ProjectGrant = {
  namespace: string;
  binding: string;
  roleKind: string;
  roleName: string;
};

export type ProjectMember = {
  kind: "User" | "Group" | "ServiceAccount";
  name: string;
  namespace?: string;
  grants: ProjectGrant[];
};

export type ProjectDetail = {
  name: string;
  namespaces: {
    name: string;
    pods: number | null;
    quota: QuotaRow[];
    age: string;
    href: string;
  }[];
  members: ProjectMember[];
  membersKnown: boolean;
};

export function useProjects() {
  const cluster = useCurrentCluster();
  return useSWR(["projects", cluster], () =>
    apiFetch<ProjectRow[]>("/api/v1/projects"),
  );
}

export function useProject(name: string) {
  const cluster = useCurrentCluster();
  return useSWR(name ? ["project", name, cluster] : null, () =>
    apiFetch<ProjectDetail>(`/api/v1/projects/${encodeURIComponent(name)}`),
  );
}

export const projectNameRe = /^[a-z0-9]([-a-z0-9]{0,40}[a-z0-9])?$/;
const nsNameRe = /^[a-z0-9]([-a-z0-9]{0,61}[a-z0-9])?$/;

export function validProjectName(n: string): boolean {
  return projectNameRe.test(n);
}
export function validNamespaceName(n: string): boolean {
  return nsNameRe.test(n);
}

// ---- quota presets -----------------------------------------------------------

export type QuotaPreset = "none" | "small" | "medium" | "large";

export const quotaPresets: Record<
  Exclude<QuotaPreset, "none">,
  Record<string, string>
> = {
  small: {
    pods: "20",
    "requests.cpu": "2",
    "requests.memory": "4Gi",
    "limits.cpu": "4",
    "limits.memory": "8Gi",
  },
  medium: {
    pods: "60",
    "requests.cpu": "8",
    "requests.memory": "16Gi",
    "limits.cpu": "16",
    "limits.memory": "32Gi",
  },
  large: {
    pods: "200",
    "requests.cpu": "32",
    "requests.memory": "64Gi",
    "limits.cpu": "64",
    "limits.memory": "128Gi",
  },
};

export const PROJECT_QUOTA_NAME = "project-quota";

function quotaYaml(namespace: string, preset: Exclude<QuotaPreset, "none">) {
  return stringify({
    apiVersion: "v1",
    kind: "ResourceQuota",
    metadata: {
      name: PROJECT_QUOTA_NAME,
      namespace,
      labels: { "app.kubernetes.io/managed-by": "vinaire" },
    },
    spec: { hard: quotaPresets[preset] },
  });
}

function namespaceYaml(name: string, project: string) {
  return stringify({
    apiVersion: "v1",
    kind: "Namespace",
    metadata: { name, labels: { [PROJECT_LABEL]: project } },
  });
}

// ---- changing a project (all through existing, audited endpoints) ----------------

// Put an existing namespace into a project, or take it out (project = null), by
// rewriting its label with the same reviewed-diff update the YAML tab uses. Only the
// label changes; the namespace and what is in it stay.
export async function setNamespaceProject(
  namespace: string,
  project: string | null,
): Promise<void> {
  const ns = await getNamespace(namespace);
  const doc = parseDocument(ns.yaml);
  if (project === null) {
    if (doc.hasIn(["metadata", "labels", PROJECT_LABEL])) {
      doc.deleteIn(["metadata", "labels", PROJECT_LABEL]);
    }
  } else {
    doc.setIn(["metadata", "labels", PROJECT_LABEL], project);
  }
  const yaml = doc.toString();
  const dry = await dryRunResource(yaml, "update");
  if (!dry.resourceVersion)
    throw new Error("The cluster gave no version to update against.");
  await updateResource(yaml, dry.resourceVersion);
}

export type CreateProjectRequest = {
  name: string;
  newNamespaces: string[];
  existingNamespaces: string[];
  quota: QuotaPreset;
};

export type StepResult = { what: string; ok: boolean; error?: string };

// Creates the project's namespaces (with the label and, if asked, a ResourceQuota) and
// puts existing ones under the label. Each step is its own call; the result says which
// worked, so a partial failure (for example, no right to create namespaces) is visible
// and can be retried.
export async function createProject(
  req: CreateProjectRequest,
  onStep?: (r: StepResult) => void,
): Promise<StepResult[]> {
  const results: StepResult[] = [];
  const run = async (what: string, fn: () => Promise<void>) => {
    let r: StepResult;
    try {
      await fn();
      r = { what, ok: true };
    } catch (e) {
      r = {
        what,
        ok: false,
        error: e instanceof Error ? e.message : String(e),
      };
    }
    results.push(r);
    onStep?.(r);
    return r.ok;
  };
  for (const ns of req.newNamespaces) {
    const created = await run(t("Create namespace {ns}", { ns }), () =>
      createResource(namespaceYaml(ns, req.name)),
    );
    if (created && req.quota !== "none") {
      await run(t("Quota in {ns}", { ns }), () =>
        createResource(
          quotaYaml(ns, req.quota as Exclude<QuotaPreset, "none">),
        ),
      );
    }
  }
  for (const ns of req.existingNamespaces) {
    const labelled = await run(t("Add {ns} to the project", { ns }), () =>
      setNamespaceProject(ns, req.name),
    );
    if (labelled && req.quota !== "none") {
      await run(t("Quota in {ns}", { ns }), () =>
        createResource(
          quotaYaml(ns, req.quota as Exclude<QuotaPreset, "none">),
        ),
      );
    }
  }
  return results;
}

export type ProjectRole = "view" | "edit" | "admin";

export const projectRoles: { id: ProjectRole; label: string; help: string }[] =
  [
    { id: "view", label: "Viewer", help: "read-only (ClusterRole view)" },
    {
      id: "edit",
      label: "Editor",
      help: "change workloads (ClusterRole edit)",
    },
    {
      id: "admin",
      label: "Admin",
      help: "manage the namespace, including roles (ClusterRole admin)",
    },
  ];

// Give a subject a role in every namespace of the project: one RoleBinding to a built-in
// ClusterRole per namespace. Kubernetes' own escalation check still applies to each.
export async function addProjectMember(
  namespaces: string[],
  subject: {
    kind: "User" | "Group" | "ServiceAccount";
    name: string;
    namespace?: string;
  },
  role: ProjectRole,
  onStep?: (r: StepResult) => void,
): Promise<StepResult[]> {
  const out: StepResult[] = [];
  for (const ns of namespaces) {
    let r: StepResult;
    try {
      await createBinding({
        scope: "namespace",
        namespace: ns,
        roleKind: "ClusterRole",
        roleName: role,
        subjects: [subject],
      });
      r = { what: t("{role} in {ns}", { role, ns }), ok: true };
    } catch (e) {
      r = {
        what: t("{role} in {ns}", { role, ns }),
        ok: false,
        error: e instanceof Error ? e.message : String(e),
      };
    }
    out.push(r);
    onStep?.(r);
  }
  return out;
}

export async function removeMemberGrants(
  grants: ProjectGrant[],
): Promise<StepResult[]> {
  const out: StepResult[] = [];
  for (const g of grants) {
    try {
      await deleteRoleBinding(g.namespace, g.binding);
      out.push({
        what: t("{role} in {ns}", { role: g.roleName, ns: g.namespace }),
        ok: true,
      });
    } catch (e) {
      out.push({
        what: t("{role} in {ns}", { role: g.roleName, ns: g.namespace }),
        ok: false,
        error: e instanceof Error ? e.message : String(e),
      });
    }
  }
  return out;
}
