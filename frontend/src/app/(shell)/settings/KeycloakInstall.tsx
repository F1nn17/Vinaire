"use client";

import { useEffect, useRef, useState } from "react";
import useSWR from "swr";
import { applyErrorMessage } from "@/components/editor/useYamlApply";
import { useResourceTable } from "@/lib/api/allResources";
import { createResource } from "@/lib/api/resources";
import { getImageTags } from "@/lib/api/imageTags";
import { useKeycloakStatus } from "@/lib/api/keycloak";
import { applyInstance, readyReplicas, withBackoff } from "@/lib/images/apply";
import {
  DB_NAME,
  KC_NAME,
  databaseForm,
  ingressYaml,
  keycloakForm,
  publicUrl,
  validateInstall,
  type KeycloakInstallInput,
} from "@/lib/keycloakInstall";
import { ensureNamespace, randomPassword } from "@/lib/values/secretHelpers";
import { t } from "@/lib/i18n/translate";
import { themeConfigMapYaml, themeVolume } from "@/lib/keycloakTheme";

const input =
  "rounded-lg border border-border bg-surface px-3 py-1.75 font-mono text-xs outline-none placeholder:text-text-faint";

export type InstalledKeycloak = {
  url: string;
  adminUser: string;
  adminPassword: string;
  https: boolean;
};

type StepState = "todo" | "doing" | "done" | "error";
const stepNames = [
  "Namespace",
  "Database",
  "Keycloak",
  "Address (Ingress)",
  "Start",
] as const;
type Step = (typeof stepNames)[number];

// Roadmap Phase 69 (notes item 7): "I don't have Keycloak yet" — a PostgreSQL and a Keycloak of their
// own in this cluster, reachable on an address, then the sign-in wizard below is filled in. Everything
// is created through the generic YAML create (Phase 25) as the signed-in user; the generated passwords
// live in this component's memory and in the instances' Secrets, nowhere else.
export function KeycloakInstall({
  onReady,
}: {
  onReady: (k: InstalledKeycloak) => void;
}) {
  // the operator may have allowed plain http on a private network (a home server without a certificate)
  const { data: kcStatus } = useKeycloakStatus();
  const httpOk = !!kcStatus?.allowHttpPrivateNetwork;
  const [f, setF] = useState({
    namespace: "keycloak",
    host: "",
    ingressClass: "",
    tls: "none" as KeycloakInstallInput["tls"],
    tlsSecret: "",
    tag: "",
    dbSize: "5Gi",
  });
  const [steps, setSteps] = useState<Record<Step, StepState>>(
    () =>
      Object.fromEntries(stepNames.map((s) => [s, "todo"])) as Record<
        Step,
        StepState
      >,
  );
  const [running, setRunning] = useState(false);
  const [failure, setFailure] = useState<string | null>(null);
  const [done, setDone] = useState(false);
  const mounted = useRef(true);
  useEffect(() => {
    mounted.current = true;
    return () => {
      mounted.current = false;
    };
  }, []);

  const tags = useSWR(["kc-tags"], () =>
    getImageTags("quay.io/keycloak/keycloak"),
  );
  // plain versions only (26.2.5), newest first: the registry also holds nightlies and variants
  const versions = (tags.data?.tags ?? [])
    .map((x) => x.name)
    .filter((n) => /^\d+\.\d+\.\d+$/.test(n))
    .slice(0, 20);
  const [otherVersion, setOtherVersion] = useState(false);
  const tag = f.tag || versions[0] || tags.data?.tags?.[0]?.name || "";
  const input_: KeycloakInstallInput = { ...f, tag };
  const problems = validateInstall(input_);
  const valid = Object.keys(problems).length === 0;

  const classes = useResourceTable("networking.k8s.io/v1/ingressclasses", "");
  const classNames = (classes.data?.rows ?? []).map((r) => r.name);
  const set = (patch: Partial<typeof f>) => setF((x) => ({ ...x, ...patch }));
  const mark = (s: Step, v: StepState) =>
    mounted.current && setSteps((x) => ({ ...x, [s]: v }));

  async function install() {
    setRunning(true);
    setFailure(null);
    const dbPw = randomPassword();
    const adminPw = randomPassword();
    let current: Step = "Namespace";
    try {
      mark("Namespace", "doing");
      await ensureNamespace(f.namespace);
      mark("Namespace", "done");

      current = "Database";
      mark("Database", "doing");
      await applyInstance(databaseForm(input_), { password: dbPw });
      mark("Database", "done");

      current = "Keycloak";
      mark("Keycloak", "doing");
      // Keycloak's sign-in page in Vinaire's look (Roadmap Phase 85b): the theme is a ConfigMap mounted into the pod
      await withBackoff(() => createResource(themeConfigMapYaml(f.namespace)));
      const uid = await applyInstance(
        keycloakForm(input_),
        { "admin-password": adminPw },
        (sts) => {
          const { volume, mount } = themeVolume();
          const pod = sts.spec.template.spec;
          pod.volumes = [...(pod.volumes ?? []), volume];
          pod.containers[0].volumeMounts = [
            ...(pod.containers[0].volumeMounts ?? []),
            mount,
          ];
        },
      );
      mark("Keycloak", "done");

      current = "Address (Ingress)";
      mark("Address (Ingress)", "doing");
      await withBackoff(() => createResource(ingressYaml(input_, uid)));
      mark("Address (Ingress)", "done");

      current = "Start";
      mark("Start", "doing");
      // the first start builds Keycloak's tables: a few minutes at most
      const until = Date.now() + 6 * 60_000;
      while (mounted.current && Date.now() < until) {
        if ((await readyReplicas(f.namespace, KC_NAME)) >= 1) break;
        await new Promise((r) => setTimeout(r, 4000));
      }
      if ((await readyReplicas(f.namespace, KC_NAME)) < 1) {
        throw new Error(
          t(
            "Keycloak is not ready yet. It keeps starting; check the pod in {ns}, then set up the sign-in below.",
            { ns: f.namespace },
          ),
        );
      }
      mark("Start", "done");
      setDone(true);
      onReady({
        url: publicUrl(input_),
        adminUser: "admin",
        adminPassword: adminPw,
        https: f.tls !== "none",
      });
    } catch (e) {
      mark(current, "error");
      setFailure(`${current}: ${applyErrorMessage(e)}`);
    } finally {
      if (mounted.current) setRunning(false);
    }
  }

  return (
    <details
      className="rounded-[10px] border border-border bg-surface p-5"
      open
    >
      <summary className="cursor-pointer text-[13px] font-semibold">
        I don&apos;t have Keycloak yet — install one in this cluster
      </summary>
      <div className="mt-2 text-xs text-text-muted">
        Creates a PostgreSQL and a Keycloak (official images, one instance each)
        in the namespace you choose, publishes Keycloak on the address you give
        through an Ingress, and fills in the form below with the generated
        administrator. The passwords live only in the instances&apos; Secrets{" "}
        <span className="font-mono">
          {DB_NAME}, {KC_NAME}
        </span>
        .{" "}
        {httpOk
          ? "On the internet give Keycloak an address with https. On a private network (home server, NetBird) http works: this Vinaire is set up to allow it."
          : "For real use give Keycloak an address with https: Vinaire only talks to a Keycloak over https."}
      </div>

      <div className="mt-4 grid gap-3 md:grid-cols-2">
        <Field label="Namespace" error={problems.namespace}>
          <input
            aria-label="Keycloak namespace"
            value={f.namespace}
            onChange={(e) => set({ namespace: e.target.value.trim() })}
            className={input}
          />
        </Field>
        <Field
          label="Address Keycloak will be reached on"
          error={f.host ? problems.host : undefined}
        >
          <input
            aria-label="Keycloak address"
            value={f.host}
            placeholder="keycloak.example.com"
            onChange={(e) => set({ host: e.target.value.trim().toLowerCase() })}
            className={input}
          />
        </Field>
        <Field label="Ingress class (optional)" error={problems.ingressClass}>
          <input
            aria-label="Ingress class"
            value={f.ingressClass}
            list="kc-classes"
            placeholder={classNames[0] ?? "the cluster's default"}
            onChange={(e) => set({ ingressClass: e.target.value.trim() })}
            className={input}
          />
          <datalist id="kc-classes">
            {classNames.map((c) => (
              <option key={c} value={c} />
            ))}
          </datalist>
        </Field>
        <Field label="Keycloak version" error={problems.tag}>
          {versions.length > 0 && !otherVersion ? (
            <select
              aria-label="Keycloak version"
              value={tag}
              onChange={(e) =>
                e.target.value === "__other"
                  ? setOtherVersion(true)
                  : set({ tag: e.target.value })
              }
              className={input}
            >
              {versions.map((v, i) => (
                <option key={v} value={v}>
                  {v}
                  {i === 0 ? " (latest)" : ""}
                </option>
              ))}
              <option value="__other">Another version…</option>
            </select>
          ) : (
            <input
              aria-label="Keycloak version"
              value={tag}
              placeholder="26.2.5"
              onChange={(e) => set({ tag: e.target.value.trim() })}
              className={input}
            />
          )}
          {tags.isLoading && (
            <span className="text-[11px] text-text-faint">
              Loading the list of versions…
            </span>
          )}
          {!tags.isLoading && versions.length === 0 && (
            <span className="text-[11px] text-text-faint">
              The list of versions could not be loaded; type one, e.g. 26.2.5.
            </span>
          )}
        </Field>
        <Field label="https">
          <select
            aria-label="Keycloak https"
            value={f.tls}
            onChange={(e) =>
              set({ tls: e.target.value as KeycloakInstallInput["tls"] })
            }
            className={input}
          >
            <option value="none">No — plain http (trial only)</option>
            <option value="default">
              Yes — the ingress controller&apos;s own certificate
            </option>
            <option value="secret">Yes — a certificate Secret I made</option>
          </select>
        </Field>
        {f.tls === "secret" && (
          <Field
            label="Certificate Secret (type kubernetes.io/tls)"
            error={problems.tlsSecret}
          >
            <input
              aria-label="Certificate Secret"
              value={f.tlsSecret}
              onChange={(e) => set({ tlsSecret: e.target.value.trim() })}
              className={input}
            />
          </Field>
        )}
        <Field label="Database volume" error={problems.dbSize}>
          <input
            aria-label="Database volume"
            value={f.dbSize}
            onChange={(e) => set({ dbSize: e.target.value.trim() })}
            className={input}
          />
        </Field>
      </div>

      {f.tls === "none" && (
        <div className="mt-3 text-xs text-warn">
          {httpOk
            ? "Without https the passwords cross the network in the clear. Vinaire accepts that only for an address on a private network (192.168.x, 10.x, 172.16-31.x, 100.64-127.x); never use it across the internet."
            : "Without https Keycloak will run, but Vinaire will refuse to use it for sign-in (and passwords would cross the network in the clear). On a home server without a certificate the operator can allow http on a private network: chart value keycloak.allowHttpPrivateNetwork=true."}
        </div>
      )}

      <ol className="mt-4 flex flex-wrap gap-x-5 gap-y-1 text-xs">
        {stepNames.map((s) => (
          <li
            key={s}
            className={
              steps[s] === "done"
                ? "text-ok"
                : steps[s] === "error"
                  ? "text-err"
                  : steps[s] === "doing"
                    ? "text-accent-text"
                    : "text-text-faint"
            }
          >
            {steps[s] === "done"
              ? "✓"
              : steps[s] === "doing"
                ? "…"
                : steps[s] === "error"
                  ? "✗"
                  : "○"}{" "}
            {s}
          </li>
        ))}
      </ol>
      {failure && <div className="mt-3 text-xs text-err">{failure}</div>}
      {done && (
        <div className="mt-3 text-xs text-ok">
          Keycloak is running. The sign-in form below is filled in with its
          address and the generated administrator — check it and press “Set up
          Keycloak sign-in”.
        </div>
      )}
      <div className="mt-4 flex justify-end">
        <button
          type="button"
          disabled={!valid || running || done}
          onClick={() => void install()}
          className="rounded-lg bg-accent px-4 py-2 text-[12.5px] font-semibold text-white disabled:opacity-50"
        >
          {running ? "Installing…" : "Install Keycloak"}
        </button>
      </div>
    </details>
  );
}

function Field({
  label,
  error,
  children,
}: {
  label: string;
  error?: string;
  children: React.ReactNode;
}) {
  return (
    <label className="flex flex-col gap-1 text-xs text-text-muted">
      {label}
      {children}
      {error && <span className="text-[11px] text-err">{t(error)}</span>}
    </label>
  );
}
