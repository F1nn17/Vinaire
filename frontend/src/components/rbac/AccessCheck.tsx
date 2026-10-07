"use client";

import { useState } from "react";
import { friendlyErrorMessage } from "@/lib/api/errors";
import {
  checkAccess,
  getSubjectBindings,
  type AccessAnswer,
  type Subject,
  type SubjectBinding,
  type SubjectKind,
} from "@/lib/api/rbacActions";

const input =
  "rounded-lg border border-border bg-surface px-3 py-1.75 font-mono text-xs outline-none placeholder:text-text-faint";

const verbs = [
  "get",
  "list",
  "watch",
  "create",
  "update",
  "patch",
  "delete",
  "*",
];

// "What can X do?" — the cluster's own answer (a SubjectAccessReview) for one
// action, and the bindings that give X anything at all, with the rules each grants.
export function AccessCheck() {
  const [subject, setSubject] = useState<Subject>({
    kind: "ServiceAccount",
    name: "",
    namespace: "",
  });
  const [verb, setVerb] = useState("list");
  const [group, setGroup] = useState("");
  const [resource, setResource] = useState("pods");
  const [namespace, setNamespace] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [answer, setAnswer] = useState<AccessAnswer | null>(null);
  const [bindings, setBindings] = useState<SubjectBinding[] | null>(null);

  const ready =
    subject.name.trim() !== "" &&
    (subject.kind !== "ServiceAccount" ||
      (subject.namespace ?? "").trim() !== "");

  const who: Subject = {
    kind: subject.kind,
    name: subject.name.trim(),
    namespace:
      subject.kind === "ServiceAccount"
        ? (subject.namespace ?? "").trim()
        : undefined,
  };

  async function run(both: boolean) {
    setBusy(true);
    setError(null);
    try {
      if (both) {
        setAnswer(
          await checkAccess({
            subject: who,
            verb,
            group: group.trim(),
            resource: resource.trim(),
            namespace: namespace.trim() || undefined,
          }),
        );
      }
      setBindings(await getSubjectBindings(who));
    } catch (err) {
      setError(friendlyErrorMessage(err));
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="flex max-w-4xl flex-col gap-4">
      <div className="text-xs text-text-muted">
        Ask the cluster whether an account may do something — the real answer,
        including every binding and group, not a guess from the lists. Needs the
        right to create SubjectAccessReviews (the chart&apos;s{" "}
        <span className="font-mono">-rbac-admin</span> role).
      </div>

      <div className="flex flex-col gap-3 rounded-[10px] border border-border bg-surface p-4">
        <div className="text-xs font-semibold">Who</div>
        <div className="flex flex-wrap gap-2">
          <select
            aria-label="Subject kind"
            value={subject.kind}
            onChange={(e) =>
              setSubject({ ...subject, kind: e.target.value as SubjectKind })
            }
            className={input}
          >
            <option value="ServiceAccount">ServiceAccount</option>
            <option value="User">User</option>
            <option value="Group">Group</option>
          </select>
          <input
            aria-label="Subject name"
            value={subject.name}
            onChange={(e) => setSubject({ ...subject, name: e.target.value })}
            placeholder="name"
            className={`${input} w-56`}
          />
          {subject.kind === "ServiceAccount" && (
            <input
              aria-label="Subject namespace"
              value={subject.namespace ?? ""}
              onChange={(e) =>
                setSubject({ ...subject, namespace: e.target.value })
              }
              placeholder="namespace"
              className={`${input} w-40`}
            />
          )}
        </div>
        <div className="text-xs font-semibold">May they…</div>
        <div className="flex flex-wrap items-center gap-2">
          <select
            aria-label="Verb"
            value={verb}
            onChange={(e) => setVerb(e.target.value)}
            className={input}
          >
            {verbs.map((v) => (
              <option key={v}>{v}</option>
            ))}
          </select>
          <input
            aria-label="Resource"
            value={resource}
            onChange={(e) => setResource(e.target.value)}
            placeholder="pods, deployments/scale…"
            className={`${input} w-52`}
          />
          <input
            aria-label="API group"
            value={group}
            onChange={(e) => setGroup(e.target.value)}
            placeholder="API group (empty = core)"
            className={`${input} w-52`}
          />
          <input
            aria-label="In namespace"
            value={namespace}
            onChange={(e) => setNamespace(e.target.value)}
            placeholder="in namespace (empty = anywhere)"
            className={`${input} w-60`}
          />
        </div>
        <div className="flex gap-2">
          <button
            type="button"
            onClick={() => void run(true)}
            disabled={!ready || busy || resource.trim() === ""}
            className="rounded-lg bg-accent px-3.5 py-1.75 text-xs font-semibold text-white disabled:opacity-50"
          >
            {busy ? "Asking…" : "Check"}
          </button>
          <button
            type="button"
            onClick={() => void run(false)}
            disabled={!ready || busy}
            className="rounded-lg border border-border bg-surface px-3.5 py-1.75 text-xs text-text-muted disabled:opacity-50"
          >
            Only list their bindings
          </button>
        </div>
      </div>

      {error && (
        <div className="rounded-lg border border-err/30 bg-surface-2 p-3 text-xs text-err">
          {error}
        </div>
      )}

      {answer && (
        <div
          role="status"
          className={`rounded-[10px] border p-4 ${answer.allowed ? "border-ok/40" : "border-err/40"} bg-surface`}
        >
          <div
            className={`text-sm font-semibold ${answer.allowed ? "text-ok" : "text-err"}`}
          >
            {answer.allowed ? "✓ Allowed" : "✕ Not allowed"}
          </div>
          {answer.reason && (
            <div className="mt-1 font-mono text-[11px] text-text-muted">
              {answer.reason}
            </div>
          )}
          {answer.evaluationError && (
            <div className="mt-1 text-[11px] text-warn">
              {answer.evaluationError}
            </div>
          )}
        </div>
      )}

      {bindings && (
        <div className="flex flex-col gap-2">
          <div className="text-xs font-semibold">
            Bindings that apply ({bindings.length})
          </div>
          {bindings.length === 0 ? (
            <div className="text-xs text-text-faint">
              None — this account has no permissions beyond what every
              authenticated account gets.
            </div>
          ) : (
            bindings.map((b) => (
              <details
                key={`${b.kind}/${b.namespace ?? ""}/${b.name}`}
                className="rounded-lg border border-border bg-surface"
              >
                <summary className="flex cursor-pointer flex-wrap items-center gap-x-3 px-3 py-2 text-xs">
                  <span className="font-mono font-semibold">{b.name}</span>
                  <span className="text-text-faint">
                    {b.kind === "ClusterRoleBinding"
                      ? "cluster-wide"
                      : `namespace ${b.namespace}`}
                  </span>
                  <span className="font-mono text-text-muted">
                    {b.roleKind}:{b.roleName}
                  </span>
                  {b.via !== "direct" && (
                    <span className="text-text-faint">via {b.via}</span>
                  )}
                </summary>
                <div className="flex flex-col gap-1 border-t border-border px-3 py-2">
                  {b.rules.length === 0 && (
                    <div className="text-[11px] text-text-faint">
                      No rules (or the role isn&apos;t readable by you).
                    </div>
                  )}
                  {b.rules.map((r, i) => (
                    <div
                      key={i}
                      className="font-mono text-[11px] text-text-muted"
                    >
                      <span className="text-text-faint">{r.apiGroups}</span>{" "}
                      {r.resources} → {r.verbs}
                    </div>
                  ))}
                </div>
              </details>
            ))
          )}
        </div>
      )}
    </div>
  );
}
