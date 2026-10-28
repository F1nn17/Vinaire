"use client";

import { useState } from "react";
import useSWR from "swr";
import { ApiError } from "@/lib/api/http";
import { friendlyErrorMessage } from "@/lib/api/errors";
import { getDetectedApps } from "@/lib/api/apps";
import { getResourceObject } from "@/lib/api/allResources";
import { revealSecretKey } from "@/lib/api/secrets";
import {
  applyKeycloakLoginTheme,
  getKeycloakThemeCheck,
  useKeycloakStatus,
  useKeycloakThemeCheck,
} from "@/lib/api/keycloak";
import { pickKeycloak } from "@/lib/keycloakPick";
import { themeMounted } from "@/lib/keycloakThemeMount";
import {
  KEYS,
  mountTheme,
  writeConfigMap,
  type Found,
} from "./KeycloakThemeInstall";

// "Update the look of Keycloak's sign-in page" in one place (Roadmap Phase 99). The page asks Vinaire's own check which theme
// the sign-in page really serves; when it is not Vinaire's, one button does what used to be three separate steps: put the
// theme next to a Keycloak that runs in this cluster, wait until it is back, and switch the realm to the theme with the
// administrator's login (used for one request, never kept). Every write is the generic YAML apply as the signed-in user, or
// the audited realm switch.

const sleep = (ms: number) => new Promise((r) => setTimeout(r, ms));

export function KeycloakLook() {
  const check = useKeycloakThemeCheck(true);
  const apps = useSWR("detected-apps-for-look", getDetectedApps, {
    revalidateOnFocus: false,
  });
  const found: Found[] = (apps.data?.apps ?? [])
    .filter((a) => a.id === "keycloak")
    .flatMap((a) => a.instances)
    .filter((i) => KEYS[i.kind])
    .map((i) => ({ namespace: i.namespace, name: i.name, kind: i.kind }));

  const status = useKeycloakStatus();
  // Only the Keycloak Vinaire signs in with is touched (Phase 106): matched by the saved issuer, or chosen by the person
  const pick = pickKeycloak(found, status.data?.issuer);
  const [chosenKey, setChosenKey] = useState("");
  const chosen = pick.choices.find(
    (f) => `${f.namespace}/${f.name}` === chosenKey,
  );
  const target = pick.target ?? chosen ?? null;
  // "outside" = the person says Vinaire's Keycloak is not one of these: only the realm is switched, nothing is mounted
  const needsChoice = found.length > 0 && !target && chosenKey !== "outside";

  const [user, setUser] = useState("admin");
  const [pass, setPass] = useState("");
  const [busy, setBusy] = useState(false);
  const [step, setStep] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [done, setDone] = useState<string | null>(null);

  const c = check.data;
  if (check.isLoading || !c) return null;

  async function fillGeneratedPassword() {
    setError(null);
    try {
      const f = target;
      if (!f) return;
      setPass(await revealSecretKey(f.namespace, f.name, "admin-password"));
    } catch (e) {
      setError(
        "Couldn't read the generated password: " + friendlyErrorMessage(e),
      );
    }
  }

  async function update() {
    setBusy(true);
    setError(null);
    setDone(null);
    try {
      // 1. the theme next to a Keycloak that runs here and does not have it yet
      for (const f of target ? [target] : []) {
        const obj = await getResourceObject(KEYS[f.kind], f.namespace, f.name);
        if (themeMounted(obj.yaml)) continue;
        setStep(`Adding the theme to ${f.namespace}/${f.name}…`);
        await writeConfigMap(f.namespace);
        await mountTheme(f);
        await sleep(15000); // the pod is replaced; give the rollout a moment to start
      }
      // 2. switch the realm — repeated while Keycloak restarts or does not list the theme yet
      let status = "";
      for (let i = 0; i < 30; i++) {
        setStep(
          i === 0
            ? "Switching the sign-in page to the Vinaire look…"
            : "Waiting for Keycloak to come back with the theme…",
        );
        try {
          const r = await applyKeycloakLoginTheme({
            adminUser: user,
            adminPassword: pass,
          });
          status = r.status;
          if (!status.startsWith("skipped")) break;
        } catch (e) {
          // a wrong login or a refusal is final; a Keycloak that is restarting is not
          if (e instanceof ApiError && e.status !== 502 && e.status !== 504)
            throw e;
        }
        await sleep(10000);
      }
      if (!status || status.startsWith("skipped")) {
        throw new Error(
          "Keycloak did not come back with the theme in time. Wait a minute and press the button again.",
        );
      }
      // 3. look at the page again (Keycloak may cache it for a moment)
      setStep("Checking the sign-in page…");
      let ok = false;
      for (let i = 0; i < 6 && !ok; i++) {
        ok = (await getKeycloakThemeCheck()).active;
        if (!ok) await sleep(5000);
      }
      setDone(
        ok
          ? "Done — the sign-in page now uses the Vinaire look."
          : "The realm is switched, but the page still shows another theme: the Keycloak client may have a theme of its own (Clients → vinaire → Settings → Login theme: empty).",
      );
      setPass("");
      await check.mutate();
    } catch (e) {
      setError(friendlyErrorMessage(e));
    } finally {
      setBusy(false);
      setStep(null);
    }
  }

  if (c.active) {
    return (
      <div className="rounded-[10px] border border-border bg-surface p-4 text-xs text-ok">
        {done ?? "The Keycloak sign-in page uses the Vinaire look."}
      </div>
    );
  }
  if (!c.checked && !done) {
    return (
      <div className="rounded-[10px] border border-border bg-surface p-4 text-xs text-text-muted">
        Vinaire could not read Keycloak&apos;s sign-in page to see its look
        {c.error ? `: ${c.error}` : "."}
      </div>
    );
  }
  return (
    <div className="flex flex-col gap-3 rounded-[10px] border border-border bg-surface p-5">
      <div className="text-[13px] font-semibold text-warn">
        The Keycloak sign-in page still has Keycloak&apos;s own look
        {c.themes?.length ? ` (${c.themes.join(", ")})` : ""}
      </div>
      <div className="text-xs text-text-muted">
        One button brings the Vinaire look to it: the theme is put next to
        Keycloak (it restarts once), then the realm{" "}
        <span className="font-mono">{c.realm}</span> is switched to it. The
        administrator&apos;s password is used for that one request and not kept.
        {found.length === 0 &&
          " Keycloak does not run in this cluster, so the theme has to be installed there by hand (zip or ConfigMap in the setup form below) — then this button switches the realm."}
      </div>
      {pick.choices.length > 0 && (
        <label className="flex flex-col gap-1 text-[11px] text-text-faint">
          Which Keycloak does Vinaire sign in with? It will be restarted once.
          <select
            value={chosenKey}
            onChange={(e) => setChosenKey(e.target.value)}
            disabled={busy}
            aria-label="Keycloak to update"
            className="w-72 rounded-md border border-border bg-surface-2 px-2 py-1.5 text-xs"
          >
            <option value="">Choose…</option>
            <option value="outside">
              None of these — my Keycloak is outside this cluster
            </option>
            {pick.choices.map((f) => (
              <option
                key={`${f.namespace}/${f.name}`}
                value={`${f.namespace}/${f.name}`}
              >
                {f.namespace}/{f.name}
              </option>
            ))}
          </select>
        </label>
      )}
      {target && (
        <div className="text-xs text-text-muted">
          Will be changed:{" "}
          <span className="font-mono">
            {target.namespace}/{target.name}
          </span>{" "}
          (restarts once). Other Keycloaks in the cluster are left alone.
        </div>
      )}
      <div className="flex flex-wrap items-end gap-2">
        <label className="flex flex-col gap-1 text-[11px] text-text-faint">
          Keycloak administrator
          <input
            value={user}
            onChange={(e) => setUser(e.target.value)}
            disabled={busy}
            aria-label="Keycloak administrator"
            className="w-40 rounded-md border border-border bg-surface-2 px-2 py-1.5 text-xs"
          />
        </label>
        <label className="flex flex-col gap-1 text-[11px] text-text-faint">
          Password
          <input
            type="password"
            value={pass}
            onChange={(e) => setPass(e.target.value)}
            disabled={busy}
            autoComplete="off"
            aria-label="Keycloak administrator password"
            className="w-56 rounded-md border border-border bg-surface-2 px-2 py-1.5 text-xs"
          />
        </label>
        {target && (
          <button
            type="button"
            onClick={() => void fillGeneratedPassword()}
            disabled={busy}
            className="rounded-md border border-border px-3 py-1.5 text-xs disabled:opacity-50"
          >
            Use the password Vinaire generated
          </button>
        )}
        <button
          type="button"
          onClick={() => void update()}
          disabled={busy || !user || !pass || needsChoice}
          className="rounded-md bg-accent px-3 py-1.5 text-xs font-semibold text-white disabled:opacity-50"
        >
          {busy ? "Working…" : "Update the look of the sign-in page"}
        </button>
      </div>
      {step && <div className="text-xs text-text-muted">{step}</div>}
      {done && <div className="text-xs text-ok">{done}</div>}
      {error && <div className="text-xs text-err">{error}</div>}
    </div>
  );
}
