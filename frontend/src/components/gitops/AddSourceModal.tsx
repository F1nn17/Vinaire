"use client";

import { useState } from "react";
import { addGitSource } from "@/lib/api/gitops";
import { applyErrorMessage } from "@/components/editor/useYamlApply";

const inputClass =
  "rounded-lg border border-border bg-surface-2 px-3 py-1.75 text-xs outline-none";

const intervals: { seconds: number; label: string }[] = [
  { seconds: 30, label: "30 seconds" },
  { seconds: 60, label: "1 minute" },
  { seconds: 180, label: "3 minutes (default)" },
  { seconds: 600, label: "10 minutes" },
  { seconds: 1800, label: "30 minutes" },
];

// Add a native GitOps source (Roadmap Phase 29). The manifests are applied as a
// ServiceAccount whose token is entered here — Vinaire's own identity is never
// used — so whatever the repository says, it can only do what that account may.
// Both tokens are sealed at rest and never shown again.
export function AddSourceModal({
  onClose,
  onAdded,
}: {
  onClose: () => void;
  onAdded: () => void;
}) {
  const [name, setName] = useState("");
  const [repoURL, setRepoURL] = useState("");
  const [revision, setRevision] = useState("");
  const [path, setPath] = useState("");
  const [recurse, setRecurse] = useState(false);
  const [namespace, setNamespace] = useState("default");
  const [interval, setInterval] = useState(180);
  const [autoSync, setAutoSync] = useState(true);
  const [force, setForce] = useState(false);
  const [gitToken, setGitToken] = useState("");
  const [gitUser, setGitUser] = useState("");
  const [applyToken, setApplyToken] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const ready =
    name.trim() !== "" && repoURL.trim() !== "" && applyToken.trim() !== "";

  async function submit() {
    setBusy(true);
    setError(null);
    try {
      await addGitSource({
        name: name.trim(),
        repoURL: repoURL.trim(),
        revision: revision.trim() || undefined,
        path: path.trim() || undefined,
        recurse,
        namespace: namespace.trim() || undefined,
        intervalSeconds: interval,
        autoSync,
        force,
        gitUsername: gitUser.trim() || undefined,
        gitToken: gitToken || undefined,
        applyToken: applyToken.trim(),
      });
      // the tokens leave this component's state as soon as they're sent
      setGitToken("");
      setApplyToken("");
      onAdded();
      onClose();
    } catch (err) {
      setError(applyErrorMessage(err));
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 p-4">
      <div className="flex max-h-[92vh] w-full max-w-2xl flex-col rounded-xl border border-border bg-surface p-5">
        <div className="text-sm font-semibold">Add a Git source</div>
        <div className="mt-1 text-xs text-text-muted">
          Vinaire reads the manifests in the repository and applies them to this
          cluster, and keeps them in sync.
        </div>

        <div className="mt-4 flex flex-col gap-4 overflow-auto pr-1">
          <div className="grid grid-cols-2 gap-3">
            <label className="flex flex-col gap-1.5 text-xs text-text-muted">
              Name
              <input
                aria-label="Name"
                value={name}
                onChange={(e) => setName(e.target.value)}
                placeholder="my-app"
                className={inputClass}
              />
            </label>
            <label className="flex flex-col gap-1.5 text-xs text-text-muted">
              Branch or tag
              <input
                aria-label="Branch or tag"
                value={revision}
                onChange={(e) => setRevision(e.target.value)}
                placeholder="default branch"
                className={`${inputClass} font-mono`}
              />
            </label>
          </div>

          <label className="flex flex-col gap-1.5 text-xs text-text-muted">
            Repository URL (https)
            <input
              aria-label="Repository URL"
              value={repoURL}
              onChange={(e) => setRepoURL(e.target.value)}
              placeholder="https://github.com/org/repo"
              className={`${inputClass} font-mono`}
            />
          </label>

          <div className="grid grid-cols-2 gap-3">
            <label className="flex flex-col gap-1.5 text-xs text-text-muted">
              Path in the repository
              <input
                aria-label="Path"
                value={path}
                onChange={(e) => setPath(e.target.value)}
                placeholder="k8s (empty = the root)"
                className={`${inputClass} font-mono`}
              />
            </label>
            <label className="flex flex-col gap-1.5 text-xs text-text-muted">
              Default namespace
              <input
                aria-label="Default namespace"
                value={namespace}
                onChange={(e) => setNamespace(e.target.value)}
                className={`${inputClass} font-mono`}
              />
              <span className="text-[11px] text-text-faint">
                for objects whose manifest names none
              </span>
            </label>
          </div>

          <div className="grid grid-cols-2 gap-3">
            <label className="flex flex-col gap-1.5 text-xs text-text-muted">
              Check the repository every
              <select
                aria-label="Poll interval"
                value={interval}
                onChange={(e) => setInterval(Number(e.target.value))}
                className={inputClass}
              >
                {intervals.map((i) => (
                  <option key={i.seconds} value={i.seconds}>
                    {i.label}
                  </option>
                ))}
              </select>
            </label>
            <label className="mt-5 flex items-center gap-2 text-xs text-text-muted">
              <input
                type="checkbox"
                checked={recurse}
                onChange={(e) => setRecurse(e.target.checked)}
              />
              Also read sub-directories
            </label>
          </div>

          <div className="flex flex-col gap-2 rounded-lg border border-border bg-surface-2 p-3 text-xs text-text-muted">
            <label className="flex items-start gap-2">
              <input
                type="checkbox"
                checked={autoSync}
                onChange={(e) => setAutoSync(e.target.checked)}
                className="mt-0.5"
              />
              <span>
                <span className="text-text">Apply automatically</span> — every
                change in git is applied, and hand edits to what git manages are
                put back. Off: Vinaire only reports, and applies when you press
                Sync now.
              </span>
            </label>
            <label className="flex items-start gap-2">
              <input
                type="checkbox"
                checked={force}
                onChange={(e) => setForce(e.target.checked)}
                className="mt-0.5"
              />
              <span>
                <span className="text-text">
                  Take over fields other tools set
                </span>{" "}
                — without it, a field changed by kubectl or another controller
                is reported as a conflict instead of being overwritten. Turn it
                on for git to be the single source of truth.
              </span>
            </label>
            <div className="text-[11px] text-text-faint">
              Objects you delete from git are <b>not</b> deleted from the
              cluster — they are listed as &ldquo;no longer in git&rdquo;.
            </div>
          </div>

          <label className="flex flex-col gap-1.5 text-xs text-text-muted">
            ServiceAccount token to apply the manifests as
            <input
              aria-label="ServiceAccount token"
              type="password"
              autoComplete="off"
              value={applyToken}
              onChange={(e) => setApplyToken(e.target.value)}
              className={`${inputClass} font-mono`}
            />
            <span className="text-[11px] text-text-faint">
              Nobody is logged in when a background sync runs, so it can&apos;t
              use your login. Give it an account that may manage only what this
              repository should (it is checked against the cluster when you add
              the source):
            </span>
            <pre className="overflow-auto rounded-md bg-surface p-2.5 font-mono text-[11px] text-text">
              {`kubectl create serviceaccount gitops -n <ns>
kubectl create role gitops -n <ns> --verb=get,list,create,patch,update \\
  --resource=configmaps,services,deployments.apps
kubectl create rolebinding gitops -n <ns> --role=gitops --serviceaccount=<ns>:gitops
kubectl create token gitops -n <ns> --duration=8760h`}
            </pre>
            <span className="text-[11px] text-text-faint">
              A token that expires stops the sync until you replace the source.
            </span>
          </label>

          <details className="rounded-lg border border-border p-3 text-xs text-text-muted">
            <summary className="cursor-pointer">
              Private repository — access token
            </summary>
            <div className="mt-3 grid grid-cols-2 gap-3">
              <label className="flex flex-col gap-1.5">
                Token (read-only)
                <input
                  aria-label="Git token"
                  type="password"
                  autoComplete="off"
                  value={gitToken}
                  onChange={(e) => setGitToken(e.target.value)}
                  className={`${inputClass} font-mono`}
                />
              </label>
              <label className="flex flex-col gap-1.5">
                Username (optional)
                <input
                  aria-label="Git username"
                  value={gitUser}
                  onChange={(e) => setGitUser(e.target.value)}
                  placeholder="git"
                  className={inputClass}
                />
              </label>
            </div>
          </details>
        </div>

        {error && (
          <div className="mt-3 whitespace-pre-wrap rounded-lg border border-err/30 bg-surface-2 p-3 text-xs text-err">
            {error}
          </div>
        )}

        <div className="mt-4 flex justify-end gap-2.5">
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
            onClick={() => void submit()}
            disabled={!ready || busy}
            className="rounded-lg bg-accent px-3.5 py-2 text-[12.5px] font-semibold text-white disabled:opacity-60"
          >
            {busy ? "Checking…" : "Add source"}
          </button>
        </div>
      </div>
    </div>
  );
}
