"use client";

import { useState } from "react";
import { friendlyErrorMessage } from "@/lib/api/errors";
import {
  useClusterRoleBindings,
  useClusterRoles,
  useRBACSummary,
  useRoleBindings,
  useRoles,
  useServiceAccounts,
} from "@/lib/hooks/useRBAC";
import type {
  ClusterRoleBindingRow,
  ClusterRoleRow,
  RoleBindingRow,
  RoleRow,
  ServiceAccountRow,
} from "@/lib/types";
import { DataTable, type DataTableColumn } from "@/components/ui/DataTable";
import { ListSkeleton } from "@/components/ui/Skeleton";
import { PillTabs } from "@/components/ui/Tabs";

type AccessControlTab =
  | "roles"
  | "clusterroles"
  | "rolebindings"
  | "clusterrolebindings"
  | "serviceaccounts"
  | "summary";

const tabs: { key: AccessControlTab; label: string }[] = [
  { key: "roles", label: "Roles" },
  { key: "clusterroles", label: "ClusterRoles" },
  { key: "rolebindings", label: "RoleBindings" },
  { key: "clusterrolebindings", label: "ClusterRoleBindings" },
  { key: "serviceaccounts", label: "ServiceAccounts" },
  { key: "summary", label: "Who can do what" },
];

// These tabs show namespaced resources — the namespace filter only makes
// sense for them (ClusterRole/ClusterRoleBinding are cluster-scoped).
const namespacedTabs: AccessControlTab[] = [
  "roles",
  "rolebindings",
  "serviceaccounts",
  "summary",
];

const roleColumns: DataTableColumn<RoleRow>[] = [
  {
    header: "Name",
    width: "2fr",
    render: (r) => (
      <span className="truncate pr-3 font-mono text-[12.5px]">{r.name}</span>
    ),
  },
  {
    header: "Namespace",
    width: "1fr",
    render: (r) => (
      <span className="font-mono text-xs text-text-muted">{r.namespace}</span>
    ),
  },
  {
    header: "Rules",
    width: ".6fr",
    render: (r) => <span className="font-mono text-xs">{r.rules}</span>,
  },
  {
    header: "Age",
    width: ".7fr",
    render: (r) => (
      <span className="font-mono text-xs text-text-muted">{r.age}</span>
    ),
  },
];

const clusterRoleColumns: DataTableColumn<ClusterRoleRow>[] = [
  {
    header: "Name",
    width: "3fr",
    render: (r) => (
      <span className="truncate pr-3 font-mono text-[12.5px]">{r.name}</span>
    ),
  },
  {
    header: "Rules",
    width: ".6fr",
    render: (r) => <span className="font-mono text-xs">{r.rules}</span>,
  },
  {
    header: "Age",
    width: ".7fr",
    render: (r) => (
      <span className="font-mono text-xs text-text-muted">{r.age}</span>
    ),
  },
];

const roleBindingColumns: DataTableColumn<RoleBindingRow>[] = [
  {
    header: "Name",
    width: "1.6fr",
    render: (r) => (
      <span className="truncate pr-3 font-mono text-[12.5px]">{r.name}</span>
    ),
  },
  {
    header: "Namespace",
    width: ".9fr",
    render: (r) => (
      <span className="font-mono text-xs text-text-muted">{r.namespace}</span>
    ),
  },
  {
    header: "Role",
    width: "1.4fr",
    render: (r) => (
      <span className="font-mono text-xs text-text-muted">
        {r.roleKind}:{r.roleName}
      </span>
    ),
  },
  {
    header: "Subjects",
    width: "2fr",
    render: (r) => (
      <span className="truncate pr-3 font-mono text-xs text-text-muted">
        {r.subjects}
      </span>
    ),
  },
  {
    header: "Age",
    width: ".7fr",
    render: (r) => (
      <span className="font-mono text-xs text-text-muted">{r.age}</span>
    ),
  },
];

const clusterRoleBindingColumns: DataTableColumn<ClusterRoleBindingRow>[] = [
  {
    header: "Name",
    width: "1.8fr",
    render: (r) => (
      <span className="truncate pr-3 font-mono text-[12.5px]">{r.name}</span>
    ),
  },
  {
    header: "Role",
    width: "1.4fr",
    render: (r) => (
      <span className="font-mono text-xs text-text-muted">
        {r.roleKind}:{r.roleName}
      </span>
    ),
  },
  {
    header: "Subjects",
    width: "2.4fr",
    render: (r) => (
      <span className="truncate pr-3 font-mono text-xs text-text-muted">
        {r.subjects}
      </span>
    ),
  },
  {
    header: "Age",
    width: ".7fr",
    render: (r) => (
      <span className="font-mono text-xs text-text-muted">{r.age}</span>
    ),
  },
];

const serviceAccountColumns: DataTableColumn<ServiceAccountRow>[] = [
  {
    header: "Name",
    width: "2fr",
    render: (r) => (
      <span className="truncate pr-3 font-mono text-[12.5px]">{r.name}</span>
    ),
  },
  {
    header: "Namespace",
    width: "1.2fr",
    render: (r) => (
      <span className="font-mono text-xs text-text-muted">{r.namespace}</span>
    ),
  },
  {
    header: "Secrets",
    width: ".7fr",
    render: (r) => <span className="font-mono text-xs">{r.secrets}</span>,
  },
  {
    header: "Age",
    width: ".7fr",
    render: (r) => (
      <span className="font-mono text-xs text-text-muted">{r.age}</span>
    ),
  },
];

export default function AccessControlPage() {
  const [tab, setTab] = useState<AccessControlTab>("roles");
  const [namespace, setNamespace] = useState("");

  const roles = useRoles(namespace, tab === "roles");
  const clusterRoles = useClusterRoles(tab === "clusterroles");
  const roleBindings = useRoleBindings(namespace, tab === "rolebindings");
  const clusterRoleBindings = useClusterRoleBindings(
    tab === "clusterrolebindings",
  );
  const serviceAccounts = useServiceAccounts(
    namespace,
    tab === "serviceaccounts",
  );
  const summary = useRBACSummary(namespace, tab === "summary");

  const active =
    tab === "roles"
      ? roles
      : tab === "clusterroles"
        ? clusterRoles
        : tab === "rolebindings"
          ? roleBindings
          : tab === "clusterrolebindings"
            ? clusterRoleBindings
            : tab === "serviceaccounts"
              ? serviceAccounts
              : summary;

  return (
    <div className="flex flex-col gap-[18px] px-8 py-7">
      <div className="text-[15px] font-semibold">Access control</div>

      <div className="flex items-center justify-between gap-3">
        <PillTabs items={tabs} active={tab} onChange={setTab} />

        {namespacedTabs.includes(tab) && (
          <label className="flex items-center gap-2">
            <span className="sr-only">Filter by namespace</span>
            <input
              type="text"
              value={namespace}
              onChange={(e) => setNamespace(e.target.value)}
              placeholder="All namespaces"
              className="rounded-lg border border-border bg-surface px-3 py-1.75 font-mono text-xs outline-none placeholder:text-text-faint"
            />
          </label>
        )}
      </div>

      {active.error ? (
        <div className="text-sm text-err">
          Failed to load {tab}: {friendlyErrorMessage(active.error)}
        </div>
      ) : tab === "roles" ? (
        <DataTable
          columns={roleColumns}
          rows={roles.data ?? []}
          rowKey={(r) => `${r.namespace}/${r.name}`}
          isLoading={roles.isLoading || !roles.data}
          emptyLabel={`No Roles found${namespace ? ` in namespace "${namespace}"` : ""}.`}
        />
      ) : tab === "clusterroles" ? (
        <DataTable
          columns={clusterRoleColumns}
          rows={clusterRoles.data ?? []}
          rowKey={(r) => r.name}
          isLoading={clusterRoles.isLoading || !clusterRoles.data}
          emptyLabel="No ClusterRoles found."
        />
      ) : tab === "rolebindings" ? (
        <DataTable
          columns={roleBindingColumns}
          rows={roleBindings.data ?? []}
          rowKey={(r) => `${r.namespace}/${r.name}`}
          isLoading={roleBindings.isLoading || !roleBindings.data}
          emptyLabel={`No RoleBindings found${namespace ? ` in namespace "${namespace}"` : ""}.`}
        />
      ) : tab === "clusterrolebindings" ? (
        <DataTable
          columns={clusterRoleBindingColumns}
          rows={clusterRoleBindings.data ?? []}
          rowKey={(r) => r.name}
          isLoading={clusterRoleBindings.isLoading || !clusterRoleBindings.data}
          emptyLabel="No ClusterRoleBindings found."
        />
      ) : tab === "serviceaccounts" ? (
        <DataTable
          columns={serviceAccountColumns}
          rows={serviceAccounts.data ?? []}
          rowKey={(r) => `${r.namespace}/${r.name}`}
          isLoading={serviceAccounts.isLoading || !serviceAccounts.data}
          emptyLabel={`No ServiceAccounts found${namespace ? ` in namespace "${namespace}"` : ""}.`}
        />
      ) : (
        <SummaryView
          data={summary.data ?? []}
          isLoading={summary.isLoading || !summary.data}
        />
      )}
    </div>
  );
}

function SummaryView({
  data,
  isLoading,
}: {
  data: NonNullable<ReturnType<typeof useRBACSummary>["data"]>;
  isLoading: boolean;
}) {
  if (isLoading) {
    return <ListSkeleton />;
  }

  if (data.length === 0) {
    return (
      <div className="text-sm text-text-muted">
        No RoleBindings found — nothing to summarize.
      </div>
    );
  }

  return (
    <div className="flex flex-col gap-3">
      <div className="text-xs text-text-faint">
        Shows what&rsquo;s configured via RoleBindings — not a live permission
        check. ClusterRoleBindings are listed on their own tab.
      </div>
      {data.map((s) => (
        <div
          key={`${s.namespace}/${s.bindingName}`}
          className="rounded-[10px] border border-border bg-surface p-4"
        >
          <div className="flex items-center justify-between">
            <div className="font-mono text-[13px] font-semibold">
              {s.bindingName}
            </div>
            <div className="font-mono text-[11px] text-text-faint">
              ns/{s.namespace} · {s.roleKind}:{s.roleName}
            </div>
          </div>
          <div className="mt-2 flex flex-wrap gap-1.5">
            {s.subjects.map((subj, i) => (
              <span
                key={i}
                className="rounded-md border border-border bg-surface-2 px-2 py-1 font-mono text-[11px]"
              >
                {subj.kind}:{subj.namespace ? `${subj.namespace}/` : ""}
                {subj.name}
              </span>
            ))}
          </div>
          {s.rules.length > 0 && (
            <div className="mt-3 flex flex-col gap-1 border-t border-border pt-3">
              {s.rules.map((rule, i) => (
                <div key={i} className="font-mono text-[11px] text-text-muted">
                  <span className="text-text-faint">{rule.apiGroups}</span>{" "}
                  {rule.resources} → {rule.verbs}
                </div>
              ))}
            </div>
          )}
        </div>
      ))}
    </div>
  );
}
