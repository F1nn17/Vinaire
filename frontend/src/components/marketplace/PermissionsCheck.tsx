"use client";

import { describeMissing, type MissingPermission } from "@/lib/api/catalog";

// The result of asking "may you install this?" — a checklist of what the chart
// needs that the signed-in identity can't do, with the way to fix it.
export function PermissionsCheck({
  checking,
  missing,
  unchecked,
}: {
  checking: boolean;
  missing: MissingPermission[];
  unchecked?: string[];
}) {
  if (missing.length === 0) {
    return (
      <div className="text-[11px] text-text-faint">
        {checking
          ? "Checking your permissions…"
          : "✓ You have the permissions this chart needs."}
        {!checking && unchecked && unchecked.length > 0 && (
          <span>
            {" "}
            Not checked (created by the chart itself): {unchecked.join(", ")}.
          </span>
        )}
      </div>
    );
  }
  return (
    <div className="rounded-lg border border-err/30 bg-surface-2 p-3 text-xs">
      <div className="font-semibold text-err">
        You don&apos;t have the permissions this chart needs
      </div>
      <ul className="mt-2 list-disc pl-5 font-mono text-[11px] text-text-muted">
        {missing.map((m) => (
          <li key={`${m.group}/${m.resource}/${m.namespace ?? ""}`}>
            {describeMissing(m)}
          </li>
        ))}
      </ul>
      <div className="mt-2 text-[11px] text-text-muted">
        Installing runs as <em>you</em>, so the cluster&apos;s RBAC decides.
        Bind the account you signed in with to the chart&apos;s <b>installer</b>{" "}
        role (in addition to the viewer role) — or, on a throwaway dev cluster,
        to <span className="font-mono">cluster-admin</span>. Note that
        Vinaire&apos;s <span className="font-mono">-admin</span> role only lets
        you change Vinaire&apos;s own settings; it grants nothing inside
        Kubernetes.
      </div>
    </div>
  );
}
