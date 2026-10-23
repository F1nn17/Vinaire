// Sets of rights to give a person who has just signed in through Keycloak (Roadmap Phase 81d). A new account
// has no rights at all; the first person needs someone with kubectl to give them some, and `cluster-admin`
// is too much to suggest by default on a cluster that isn't one's own sandbox. The roles named here are the
// ones the Helm chart ships (`<prefix>-viewer`, `-installer`, `-node-admin`, `-rbac-admin`, `-admin`); none
// of them is cluster-admin. Pure functions, so the commands are easy to read and to test.

export type RolePresetId =
  "vinaire-admin" | "viewer" | "installer" | "nodes" | "cluster-admin";

export type RolePreset = {
  id: RolePresetId;
  title: string;
  // what the person can do, in words
  detail: string;
  // the chart's roles bound by this preset (suffixes after the prefix); empty for cluster-admin
  roles: string[];
  // shown with a warning
  dangerous?: boolean;
};

export const rolePresets: RolePreset[] = [
  {
    id: "vinaire-admin",
    title: "Administrator of Vinaire (recommended for the first person)",
    detail:
      "Browse everything, manage Vinaire itself (clusters, sign-in, audit) and hand out the chart's roles to other people from the Access control page — without being a cluster administrator.",
    roles: ["viewer", "admin", "rbac-admin"],
  },
  {
    id: "viewer",
    title: "Read only",
    detail:
      "Browse workloads, nodes, network, storage and logs; change nothing.",
    roles: ["viewer"],
  },
  {
    id: "installer",
    title: "Read and install applications",
    detail:
      "Read only, plus install from the Marketplace and apply YAML in the namespaces the cluster allows.",
    roles: ["viewer", "installer"],
  },
  {
    id: "nodes",
    title: "Read and operate nodes",
    detail: "Read only, plus cordon, drain, label, taint and remove nodes.",
    roles: ["viewer", "node-admin"],
  },
  {
    id: "cluster-admin",
    title: "Full cluster administrator (cluster-admin)",
    detail:
      "Everything in the cluster, including reading every Secret and changing any role. Only for a cluster that is yours alone.",
    roles: [],
    dangerous: true,
  },
];

// The account name the cluster knows a person by: "oidc-" plus the user name, lower-cased with anything
// but letters and digits turned into single dashes (the backend's oidcbroker.AccountName).
export function accountNameFor(username: string): string {
  const core = username
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, "-")
    .replace(/^-+|-+$/g, "")
    .slice(0, 40)
    .replace(/-+$/g, "");
  return `oidc-${core || "user"}`;
}

// The kubectl commands that bind a preset to the account `account` in `namespace`. `prefix` is the chart's
// full name (the status endpoint's rolePrefix); without it (Vinaire not installed by the chart) the roles
// are assumed to be named "vinaire-…" as in AGENTS.md's example.
export function bindCommands(
  preset: RolePreset,
  account: string,
  namespace: string,
  prefix = "vinaire",
): string[] {
  const subject = `--serviceaccount=${namespace}:${account}`;
  if (preset.id === "cluster-admin") {
    return [
      `kubectl create clusterrolebinding ${account}-cluster-admin --clusterrole=cluster-admin ${subject}`,
    ];
  }
  return preset.roles.map(
    (r) =>
      `kubectl create clusterrolebinding ${account}-${r} --clusterrole=${prefix}-${r} ${subject}`,
  );
}
