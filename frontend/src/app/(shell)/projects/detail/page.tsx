"use client";

import Link from "next/link";
import { useRouter, useSearchParams } from "next/navigation";
import { Suspense, useState } from "react";
import { t } from "@/lib/i18n/translate";
import { friendlyErrorMessage } from "@/lib/api/errors";
import {
  addProjectMember,
  projectRoles,
  removeMemberGrants,
  setNamespaceProject,
  useProject,
  type ProjectMember,
  type ProjectRole,
  type StepResult,
} from "@/lib/api/projects";
import { useNamespaces } from "@/lib/hooks/useNamespaces";
import { QuotaBar } from "@/components/ui/QuotaBar";
import { ListSkeleton } from "@/components/ui/Skeleton";
import { TypeToConfirmModal } from "@/components/ui/TypeToConfirmModal";

const input =
  "rounded-lg border border-border bg-surface px-3 py-1.75 font-mono text-xs outline-none placeholder:text-text-faint";

export default function ProjectDetailRoute() {
  return (
    <Suspense
      fallback={<div className="p-8 text-sm text-text-muted">Loading…</div>}
    >
      <ProjectDetailPage />
    </Suspense>
  );
}

function ProjectDetailPage() {
  const router = useRouter();
  const name = useSearchParams().get("name") ?? "";
  const { data, error, isLoading, mutate } = useProject(name);
  const { data: allNamespaces } = useNamespaces();
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<string | null>(null);
  const [adding, setAdding] = useState(false);
  const [leaving, setLeaving] = useState<string | null>(null);
  const [deleting, setDeleting] = useState(false);

  async function addNamespace(ns: string) {
    setBusy(true);
    setProblem(null);
    try {
      await setNamespaceProject(ns, name);
      await mutate();
    } catch (e) {
      setProblem(friendlyErrorMessage(e));
    } finally {
      setBusy(false);
    }
  }

  async function removeNamespace(ns: string) {
    setBusy(true);
    setProblem(null);
    try {
      await setNamespaceProject(ns, null);
      setLeaving(null);
      if ((data?.namespaces.length ?? 0) <= 1) {
        router.replace("/projects");
        return;
      }
      await mutate();
    } catch (e) {
      setProblem(friendlyErrorMessage(e));
    } finally {
      setBusy(false);
    }
  }

  async function deleteProject() {
    if (!data) return;
    setBusy(true);
    setProblem(null);
    const failed: string[] = [];
    for (const ns of data.namespaces) {
      try {
        await setNamespaceProject(ns.name, null);
      } catch (e) {
        failed.push(`${ns.name}: ${friendlyErrorMessage(e)}`);
      }
    }
    setBusy(false);
    if (failed.length > 0) {
      setProblem(failed.join(" · "));
      setDeleting(false);
      await mutate();
      return;
    }
    router.replace("/projects");
  }

  if (!name) {
    return (
      <div className="p-8 text-sm text-text-muted">No project chosen.</div>
    );
  }
  const candidates = (allNamespaces ?? []).filter(
    (n) =>
      !n.project &&
      !["kube-system", "kube-public", "kube-node-lease"].includes(n.name),
  );

  return (
    <div className="flex max-w-5xl flex-col gap-[18px] px-8 py-7">
      <div>
        <Link
          href="/projects"
          className="text-xs text-text-faint hover:underline"
        >
          ← Projects
        </Link>
        <div className="mt-1 flex items-center justify-between">
          <div className="font-mono text-[15px] font-semibold">{name}</div>
          <button
            type="button"
            onClick={() => setDeleting(true)}
            className="rounded-lg border border-border px-3 py-1.5 text-xs text-err hover:border-err"
          >
            Delete project
          </button>
        </div>
      </div>

      {error ? (
        <div className="text-sm text-err">
          Failed to load the project: {friendlyErrorMessage(error)}
        </div>
      ) : isLoading || !data ? (
        <ListSkeleton rows={4} />
      ) : (
        <>
          <section className="rounded-[10px] border border-border bg-surface p-5">
            <div className="flex items-center justify-between">
              <div className="text-[13px] font-semibold">Namespaces</div>
              <button
                type="button"
                onClick={() => setAdding(!adding)}
                className="rounded-lg border border-border px-3 py-1.5 text-xs hover:border-accent"
              >
                Add a namespace
              </button>
            </div>
            {adding && (
              <div className="mt-3 flex flex-wrap gap-1.5 rounded-lg border border-border p-2">
                {candidates.length === 0 ? (
                  <span className="text-xs text-text-faint">
                    Every other namespace is already in a project.
                  </span>
                ) : (
                  candidates.map((n) => (
                    <button
                      key={n.name}
                      type="button"
                      disabled={busy}
                      onClick={() => void addNamespace(n.name)}
                      className="rounded bg-surface-2 px-2 py-1 font-mono text-[11px] hover:text-accent-text"
                    >
                      + {n.name}
                    </button>
                  ))
                )}
              </div>
            )}
            <table className="mt-3 w-full text-xs">
              <tbody>
                {data.namespaces.map((n) => (
                  <tr key={n.name} className="border-t border-border align-top">
                    <td className="py-2 pr-3 font-mono">
                      <Link
                        href={n.href}
                        className="text-accent-text hover:underline"
                      >
                        {n.name}
                      </Link>
                      <div className="text-[11px] text-text-faint">
                        {n.pods ?? "—"} pods · {n.age}
                      </div>
                    </td>
                    <td className="py-2">
                      {n.quota.length === 0 ? (
                        <span className="text-text-faint">No quota</span>
                      ) : (
                        <div className="flex flex-wrap gap-x-4 gap-y-1.5">
                          {n.quota.slice(0, 3).map((q) => (
                            <QuotaBar key={q.resource} row={q} />
                          ))}
                        </div>
                      )}
                    </td>
                    <td className="py-2 text-right">
                      <button
                        type="button"
                        disabled={busy}
                        onClick={() => setLeaving(n.name)}
                        className="rounded-lg border border-border px-2.5 py-1 text-[11px] hover:border-err hover:text-err"
                      >
                        Remove from project
                      </button>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </section>

          <Members
            project={name}
            namespaces={data.namespaces.map((n) => n.name)}
            members={data.members}
            known={data.membersKnown}
            onChanged={() => void mutate()}
          />
        </>
      )}

      {problem && <div className="text-xs text-err">{problem}</div>}

      {leaving && (
        <TypeToConfirmModal
          title={t("Take {ns} out of the project?", { ns: leaving })}
          message="Only the label is removed. The namespace, what runs in it, its quota and the roles in it stay as they are."
          expected={leaving}
          confirmLabel="Remove"
          busy={busy}
          error={null}
          onConfirm={() => void removeNamespace(leaving)}
          onCancel={() => setLeaving(null)}
        />
      )}
      {deleting && (
        <TypeToConfirmModal
          title="Delete the project?"
          message="This removes the project label from all its namespaces. The namespaces themselves, their workloads, quotas and roles are not touched."
          expected={name}
          confirmLabel="Delete project"
          busy={busy}
          error={null}
          onConfirm={() => void deleteProject()}
          onCancel={() => setDeleting(false)}
        />
      )}
    </div>
  );
}

function Members({
  project,
  namespaces,
  members,
  known,
  onChanged,
}: {
  project: string;
  namespaces: string[];
  members: ProjectMember[];
  known: boolean;
  onChanged: () => void;
}) {
  const [open, setOpen] = useState(false);
  const [kind, setKind] = useState<"User" | "Group" | "ServiceAccount">("User");
  const [subject, setSubject] = useState("");
  const [saNs, setSaNs] = useState("default");
  const [role, setRole] = useState<ProjectRole>("edit");
  const [busy, setBusy] = useState(false);
  const [steps, setSteps] = useState<StepResult[]>([]);

  async function add() {
    setBusy(true);
    setSteps([]);
    const res = await addProjectMember(
      namespaces,
      {
        kind,
        name: subject.trim(),
        namespace: kind === "ServiceAccount" ? saNs.trim() : undefined,
      },
      role,
      (r) => setSteps((s) => [...s, r]),
    );
    setBusy(false);
    onChanged();
    if (res.every((r) => r.ok)) {
      setOpen(false);
      setSubject("");
    }
  }

  async function remove(m: ProjectMember) {
    setBusy(true);
    const res = await removeMemberGrants(m.grants);
    setBusy(false);
    setSteps(res);
    onChanged();
  }

  return (
    <section className="rounded-[10px] border border-border bg-surface p-5">
      <div className="flex items-center justify-between">
        <div className="text-[13px] font-semibold">Members</div>
        <button
          type="button"
          onClick={() => setOpen(!open)}
          className="rounded-lg border border-border px-3 py-1.5 text-xs hover:border-accent"
        >
          Add a member
        </button>
      </div>
      <div className="mt-1 text-[11px] text-text-faint">
        A member holds a role in every namespace of the project: one RoleBinding
        to a built-in role per namespace, made as you — Kubernetes&apos; own
        escalation check applies.
      </div>

      {open && (
        <div className="mt-3 grid grid-cols-4 gap-3 rounded-lg border border-border p-3">
          <label className="flex flex-col gap-1.5 text-xs text-text-muted">
            Kind
            <select
              aria-label="Member kind"
              value={kind}
              onChange={(e) => setKind(e.target.value as typeof kind)}
              className={input}
            >
              <option value="User">User</option>
              <option value="Group">Group</option>
              <option value="ServiceAccount">ServiceAccount</option>
            </select>
          </label>
          <label className="flex flex-col gap-1.5 text-xs text-text-muted">
            Name
            <input
              aria-label="Member name"
              value={subject}
              onChange={(e) => setSubject(e.target.value)}
              placeholder={kind === "User" ? "oidc:alice" : "name"}
              className={input}
            />
          </label>
          {kind === "ServiceAccount" ? (
            <label className="flex flex-col gap-1.5 text-xs text-text-muted">
              Namespace of the ServiceAccount
              <input
                aria-label="ServiceAccount namespace"
                value={saNs}
                onChange={(e) => setSaNs(e.target.value)}
                className={input}
              />
            </label>
          ) : (
            <span />
          )}
          <label className="flex flex-col gap-1.5 text-xs text-text-muted">
            Role
            <select
              aria-label="Member role"
              value={role}
              onChange={(e) => setRole(e.target.value as ProjectRole)}
              className={input}
            >
              {projectRoles.map((r) => (
                <option key={r.id} value={r.id}>
                  {r.label} — {r.help}
                </option>
              ))}
            </select>
          </label>
          <div className="col-span-4 flex justify-end">
            <button
              type="button"
              disabled={busy || !subject.trim()}
              onClick={() => void add()}
              className="rounded-lg bg-accent px-4 py-1.5 text-[12.5px] font-semibold text-white disabled:opacity-50"
            >
              {busy ? "Adding…" : t("Add to {project}", { project })}
            </button>
          </div>
        </div>
      )}

      {!known ? (
        <div className="mt-3 text-xs text-text-muted">
          You aren&apos;t allowed to list role bindings, so members can&apos;t
          be shown.
        </div>
      ) : members.length === 0 ? (
        <div className="mt-3 text-xs text-text-muted">
          No one has a role in this project yet.
        </div>
      ) : (
        <table className="mt-3 w-full text-xs">
          <tbody>
            {members.map((m) => (
              <tr
                key={`${m.kind}/${m.namespace}/${m.name}`}
                className="border-t border-border align-top"
              >
                <td className="py-2 pr-3 font-mono">
                  {m.name}
                  <div className="text-[11px] text-text-faint">
                    {m.kind}
                    {m.namespace ? ` · ${m.namespace}` : ""}
                  </div>
                </td>
                <td className="py-2 text-text-muted">
                  {m.grants.map((g) => (
                    <span
                      key={g.binding + g.namespace}
                      className="mr-2 inline-block rounded bg-surface-2 px-1.5 py-0.5 font-mono text-[11px]"
                    >
                      {g.roleName} · {g.namespace}
                    </span>
                  ))}
                </td>
                <td className="py-2 text-right">
                  <button
                    type="button"
                    disabled={busy}
                    onClick={() => void remove(m)}
                    className="rounded-lg border border-border px-2.5 py-1 text-[11px] text-err hover:border-err"
                  >
                    Remove access
                  </button>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      )}

      {steps.length > 0 && (
        <ul className="mt-3 flex flex-col gap-1 text-xs">
          {steps.map((s, i) => (
            <li key={i} className={s.ok ? "text-ok" : "text-err"}>
              {s.ok ? "✓" : "✗"} {s.what}
              {s.error && <span className="text-text-muted"> — {s.error}</span>}
            </li>
          ))}
        </ul>
      )}
    </section>
  );
}
