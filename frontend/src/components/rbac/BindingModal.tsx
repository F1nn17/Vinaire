"use client";

import { useState } from "react";
import { friendlyErrorMessage } from "@/lib/api/errors";
import {
  createBinding,
  type BindingRequest,
  type Subject,
  type SubjectKind,
} from "@/lib/api/rbacActions";
import { useClusterRoles, useRoles } from "@/lib/hooks/useRBAC";

const input =
  "w-full rounded-lg border border-border bg-surface-2 px-3 py-1.75 font-mono text-xs outline-none placeholder:text-text-faint";

// Wide grants get an extra, typed confirmation.
const sensitive = new Set(["cluster-admin", "admin", "edit"]);

// Give an existing role to someone. Vinaire never writes roles — only the binding —
// and the cluster refuses a binding for a role carrying rights the signed-in user
// doesn't hold themselves, so this can't be used to climb.
export function BindingModal({
  defaultScope,
  defaultNamespace = "",
  onClose,
  onCreated,
}: {
  defaultScope: "cluster" | "namespace";
  defaultNamespace?: string;
  onClose: () => void;
  onCreated: () => void;
}) {
  const [scope, setScope] = useState<"cluster" | "namespace">(defaultScope);
  const [namespace, setNamespace] = useState(defaultNamespace);
  const [roleKind, setRoleKind] = useState<"ClusterRole" | "Role">(
    "ClusterRole",
  );
  const [roleName, setRoleName] = useState("");
  const [subjects, setSubjects] = useState<Subject[]>([
    { kind: "ServiceAccount", name: "", namespace: "" },
  ]);
  const [confirm, setConfirm] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const clusterRoles = useClusterRoles(true);
  const nsRoles = useRoles(
    scope === "namespace" && namespace ? namespace : "",
    scope === "namespace" && !!namespace,
  );
  const roleOptions =
    roleKind === "ClusterRole"
      ? (clusterRoles.data ?? []).map((r) => r.name)
      : (nsRoles.data ?? [])
          .filter((r) => r.namespace === namespace)
          .map((r) => r.name);

  const clusterWide = scope === "cluster";
  const needsConfirm = clusterWide;
  const warnWide = sensitive.has(roleName) || clusterWide;
  const valid =
    roleName.trim() !== "" &&
    (scope === "cluster" || namespace.trim() !== "") &&
    subjects.length > 0 &&
    subjects.every(
      (s) =>
        s.name.trim() !== "" &&
        (s.kind !== "ServiceAccount" || (s.namespace ?? "").trim() !== ""),
    ) &&
    (!needsConfirm || confirm === roleName);

  function setSubject(i: number, patch: Partial<Subject>) {
    setSubjects(subjects.map((s, j) => (j === i ? { ...s, ...patch } : s)));
  }

  async function save() {
    setBusy(true);
    setError(null);
    const req: BindingRequest = {
      scope,
      namespace: scope === "namespace" ? namespace.trim() : undefined,
      roleKind: scope === "cluster" ? "ClusterRole" : roleKind,
      roleName: roleName.trim(),
      subjects: subjects.map((s) => ({
        kind: s.kind,
        name: s.name.trim(),
        namespace:
          s.kind === "ServiceAccount" ? (s.namespace ?? "").trim() : undefined,
      })),
      confirmName: needsConfirm ? confirm : undefined,
    };
    try {
      await createBinding(req);
      onCreated();
      onClose();
    } catch (err) {
      setError(friendlyErrorMessage(err));
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 p-4">
      <div className="flex max-h-[92vh] w-full max-w-2xl flex-col rounded-xl border border-border bg-surface p-5">
        <div className="text-sm font-semibold">Bind a role</div>
        <div className="mt-1 text-xs text-text-muted">
          Gives an <b>existing</b> role to the people or accounts below. Vinaire
          never creates roles, and the cluster refuses a binding for a role with
          rights you don&apos;t hold yourself (unless it granted you{" "}
          <span className="font-mono">bind</span> on it).
        </div>

        <div className="mt-4 min-h-0 flex-1 overflow-auto pr-1">
          <div className="grid grid-cols-2 gap-3">
            <label className="flex flex-col gap-1.5 text-xs text-text-muted">
              Applies
              <select
                aria-label="Scope"
                value={scope}
                onChange={(e) => {
                  setScope(e.target.value as "cluster" | "namespace");
                  setRoleName("");
                  setConfirm("");
                }}
                className={input}
              >
                <option value="namespace">
                  In one namespace (RoleBinding)
                </option>
                <option value="cluster">
                  Cluster-wide (ClusterRoleBinding)
                </option>
              </select>
            </label>
            {scope === "namespace" ? (
              <label className="flex flex-col gap-1.5 text-xs text-text-muted">
                Namespace
                <input
                  aria-label="Namespace"
                  value={namespace}
                  onChange={(e) => setNamespace(e.target.value)}
                  placeholder="shop"
                  className={input}
                />
              </label>
            ) : (
              <div />
            )}
            {scope === "namespace" && (
              <label className="flex flex-col gap-1.5 text-xs text-text-muted">
                Role kind
                <select
                  aria-label="Role kind"
                  value={roleKind}
                  onChange={(e) => {
                    setRoleKind(e.target.value as "ClusterRole" | "Role");
                    setRoleName("");
                  }}
                  className={input}
                >
                  <option value="ClusterRole">
                    ClusterRole (reused in this namespace)
                  </option>
                  <option value="Role">Role of this namespace</option>
                </select>
              </label>
            )}
            <label className="flex flex-col gap-1.5 text-xs text-text-muted">
              Role
              <input
                aria-label="Role"
                list="rbac-role-options"
                value={roleName}
                onChange={(e) => setRoleName(e.target.value)}
                placeholder="view, edit, vinaire-viewer…"
                className={input}
              />
              <datalist id="rbac-role-options">
                {roleOptions.map((n) => (
                  <option key={n} value={n} />
                ))}
              </datalist>
            </label>
          </div>

          <div className="mt-4 text-xs text-text-muted">Who gets it</div>
          <div className="mt-1.5 flex flex-col gap-2">
            {subjects.map((s, i) => (
              <div key={i} className="flex gap-2">
                <select
                  aria-label="Subject kind"
                  value={s.kind}
                  onChange={(e) =>
                    setSubject(i, { kind: e.target.value as SubjectKind })
                  }
                  className={`${input} w-40`}
                >
                  <option value="ServiceAccount">ServiceAccount</option>
                  <option value="User">User</option>
                  <option value="Group">Group</option>
                </select>
                <input
                  aria-label="Subject name"
                  value={s.name}
                  onChange={(e) => setSubject(i, { name: e.target.value })}
                  placeholder={
                    s.kind === "ServiceAccount"
                      ? "me"
                      : s.kind === "User"
                        ? "alice@example.com"
                        : "developers"
                  }
                  className={input}
                />
                {s.kind === "ServiceAccount" && (
                  <input
                    aria-label="Subject namespace"
                    value={s.namespace ?? ""}
                    onChange={(e) =>
                      setSubject(i, { namespace: e.target.value })
                    }
                    placeholder="its namespace"
                    className={`${input} w-44`}
                  />
                )}
                {subjects.length > 1 && (
                  <button
                    type="button"
                    aria-label="Remove subject"
                    onClick={() =>
                      setSubjects(subjects.filter((_, j) => j !== i))
                    }
                    className="rounded-lg border border-border bg-surface-2 px-2.5 text-xs text-text-muted"
                  >
                    ✕
                  </button>
                )}
              </div>
            ))}
            <button
              type="button"
              onClick={() =>
                setSubjects([
                  ...subjects,
                  { kind: "ServiceAccount", name: "", namespace: "" },
                ])
              }
              className="w-fit rounded-lg border border-border bg-surface-2 px-3 py-1.5 text-xs"
            >
              + Add another
            </button>
          </div>

          {warnWide && roleName && (
            <div className="mt-4 rounded-lg border border-warn/40 bg-surface-2 p-3 text-xs text-warn">
              {clusterWide
                ? `This gives "${roleName}" in every namespace and on cluster-level resources.`
                : `"${roleName}" is a broad role.`}
            </div>
          )}
          {needsConfirm && (
            <label className="mt-3 flex flex-col gap-1.5 text-xs text-text-muted">
              Type{" "}
              <span className="font-mono text-text">
                {roleName || "the role's name"}
              </span>{" "}
              to confirm
              <input
                aria-label="Confirm role name"
                value={confirm}
                onChange={(e) => setConfirm(e.target.value)}
                className={input}
              />
            </label>
          )}
        </div>

        {error && (
          <div className="mt-3 rounded-lg border border-err/30 bg-surface-2 p-3 text-xs text-err">
            {error}
          </div>
        )}
        <div className="mt-5 flex justify-end gap-2.5">
          <button
            type="button"
            onClick={onClose}
            disabled={busy}
            className="rounded-lg border border-border bg-surface-2 px-3.5 py-2 text-[12.5px] disabled:opacity-60"
          >
            Cancel
          </button>
          <button
            type="button"
            onClick={() => void save()}
            disabled={busy || !valid}
            className="rounded-lg bg-accent px-3.5 py-2 text-[12.5px] font-semibold text-white disabled:opacity-50"
          >
            {busy ? "Binding…" : "Bind"}
          </button>
        </div>
      </div>
    </div>
  );
}
