"use client";

import { useState } from "react";
import useSWR from "swr";
import { ApiError } from "@/lib/api/http";
import { friendlyErrorMessage } from "@/lib/api/errors";
import { getDetectedApps } from "@/lib/api/apps";
import { getResourceObject } from "@/lib/api/allResources";
import {
  createResource,
  dryRunResource,
  updateResource,
} from "@/lib/api/resources";
import { withBackoff } from "@/lib/images/apply";
import { THEME_CONFIGMAP, themeConfigMapYaml } from "@/lib/keycloakTheme";
import { themeMounted, withThemeMounted } from "@/lib/keycloakThemeMount";

// A Keycloak that is already running in the cluster picks up Vinaire's sign-in theme (Roadmap Phase 91): the theme goes
// into a ConfigMap next to it and the Keycloak workload mounts it (which restarts it once). Both steps are the generic
// YAML create/update of Phase 25, as the signed-in user — no write path of its own. Keycloaks Vinaire installs after the
// theme existed already have it.

export const KEYS: Record<string, string> = {
  StatefulSet: "apps/v1/statefulsets",
  Deployment: "apps/v1/deployments",
};

export type Found = { namespace: string; name: string; kind: string };

export async function writeConfigMap(namespace: string) {
  const yaml = themeConfigMapYaml(namespace);
  try {
    await withBackoff(() => createResource(yaml));
  } catch (e) {
    if (!(e instanceof ApiError) || e.status !== 409) throw e;
    // it is there already (an older copy of the theme): bring it up to date
    const dry = await dryRunResource(yaml, "update");
    await withBackoff(() => updateResource(yaml, dry.resourceVersion ?? ""));
  }
}

export async function mountTheme(f: Found): Promise<boolean> {
  const key = KEYS[f.kind];
  const obj = await getResourceObject(key, f.namespace, f.name);
  const r = withThemeMounted(obj.yaml);
  if (!r.ok) throw new Error(r.problem);
  if (!r.changed) return false;
  const dry = await dryRunResource(r.yaml, "update");
  await withBackoff(() => updateResource(r.yaml, dry.resourceVersion ?? ""));
  return true;
}

function Instance({ f }: { f: Found }) {
  const state = useSWR(["kc-theme", f.namespace, f.name], async () => {
    const obj = await getResourceObject(KEYS[f.kind], f.namespace, f.name);
    return themeMounted(obj.yaml);
  });
  const [busy, setBusy] = useState(false);
  const [done, setDone] = useState(false);
  const [error, setError] = useState<string | null>(null);

  async function add() {
    setBusy(true);
    setError(null);
    try {
      await writeConfigMap(f.namespace);
      await mountTheme(f);
      setDone(true);
      await state.mutate();
    } catch (e) {
      setError(friendlyErrorMessage(e));
    } finally {
      setBusy(false);
    }
  }

  const has = state.data === true || done;
  return (
    <div className="flex flex-col gap-1 rounded-lg border border-border bg-surface-2 p-3">
      <div className="flex items-center justify-between gap-3">
        <span className="font-mono text-xs">
          {f.namespace}/{f.name}
        </span>
        {has ? (
          <span className="text-xs text-ok">The theme is mounted</span>
        ) : (
          <button
            type="button"
            onClick={add}
            disabled={busy || state.isLoading}
            className="rounded-md bg-accent px-3 py-1.5 text-xs font-semibold text-white disabled:opacity-50"
          >
            {busy ? "Adding…" : "Add the theme to this Keycloak"}
          </button>
        )}
      </div>
      {done && (
        <div className="text-xs text-text-muted">
          Keycloak restarts once to pick it up (a minute or two). When it is
          back, run “Set up Keycloak sign-in” below again with the
          administrator&apos;s password: that step switches the realm to the
          theme.
        </div>
      )}
      {error && <div className="text-xs text-err">{error}</div>}
    </div>
  );
}

export function KeycloakThemeInstall() {
  const apps = useSWR("detected-apps-for-theme", getDetectedApps, {
    revalidateOnFocus: false,
  });
  const found: Found[] = (apps.data?.apps ?? [])
    .filter((a) => a.id === "keycloak")
    .flatMap((a) => a.instances)
    .filter((i) => KEYS[i.kind])
    .map((i) => ({ namespace: i.namespace, name: i.name, kind: i.kind }));
  if (found.length === 0) return null;
  return (
    <div className="mt-1 flex flex-col gap-2">
      <div className="text-[11px] text-text-faint">
        Keycloak found in this cluster — it needs the theme (the ConfigMap{" "}
        <span className="font-mono">{THEME_CONFIGMAP}</span>) to show it:
      </div>
      {found.map((f) => (
        <Instance key={`${f.namespace}/${f.name}`} f={f} />
      ))}
    </div>
  );
}
