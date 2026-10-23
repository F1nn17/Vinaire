"use client";

import { useState } from "react";
import { useKeycloakStatus } from "@/lib/api/keycloak";
import {
  bindCommands,
  rolePresets,
  type RolePresetId,
} from "@/lib/rolePresets";

// The commands that give a person who signed in through Keycloak some rights (Roadmap Phase 81d). The
// account has none, and `cluster-admin` is too much to suggest by default — so the choice is a set of
// the chart's roles, with the recommended one first and cluster-admin last, behind a warning.
export function FirstAdminCommands({
  account,
  namespace,
}: {
  account: string;
  namespace: string;
}) {
  const { data: status } = useKeycloakStatus();
  const [id, setId] = useState<RolePresetId>("vinaire-admin");
  const preset = rolePresets.find((p) => p.id === id) ?? rolePresets[0];
  const commands = bindCommands(preset, account, namespace, status?.rolePrefix);

  return (
    <div className="mt-2 flex flex-col gap-2">
      <label className="flex flex-col gap-1 text-text-muted">
        What the first person may do
        <select
          aria-label="What the first person may do"
          value={id}
          onChange={(e) => setId(e.target.value as RolePresetId)}
          className="rounded-lg border border-border bg-surface px-3 py-1.75 text-xs outline-none"
        >
          {rolePresets.map((p) => (
            <option key={p.id} value={p.id}>
              {p.title}
            </option>
          ))}
        </select>
      </label>
      <div className={preset.dangerous ? "text-warn" : "text-text-muted"}>
        {preset.detail}
      </div>
      <pre className="overflow-x-auto rounded bg-bg p-2 font-mono text-[11px]">
        {commands.join("\n")}
      </pre>
    </div>
  );
}
