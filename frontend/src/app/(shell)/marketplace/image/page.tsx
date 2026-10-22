"use client";

import Link from "next/link";
import { useEffect, useMemo, useRef, useState } from "react";
import useSWR from "swr";
import { getImageTags } from "@/lib/api/imageTags";
import { t } from "@/lib/i18n/translate";
import { createResource } from "@/lib/api/resources";
import { applyErrorMessage } from "@/components/editor/useYamlApply";
import { CodeBlock } from "@/components/ui/CodeBlock";
import { PillTabs } from "@/components/ui/Tabs";
import { InstanceWatch } from "@/components/marketplace/InstanceWatch";
import { WizardWarnings } from "@/components/marketplace/WizardWarnings";
import { PostgresPicker } from "@/components/marketplace/PostgresPicker";
import { parse } from "yaml";
import { ApiError } from "@/lib/api/http";
import { getResourceObject } from "@/lib/api/allResources";
import { ensureNamespace, randomPassword } from "@/lib/values/secretHelpers";
import {
  build,
  imageOf,
  newForm,
  presets,
  validate,
  type Doc,
  type Form,
  type Preset,
} from "@/lib/images/presets";

const inputClass =
  "w-full rounded-lg border border-border bg-surface-2 px-3 py-1.75 text-xs outline-none";

// Roadmap Phase 67: one instance from the publisher's own image, with every value explained. The
// objects are built here and applied one by one through the generic YAML create of Phase 25 — as the
// signed-in user, so the cluster's RBAC decides; this page has no write path of its own.
export default function ImageInstallPage() {
  const [preset, setPreset] = useState<Preset | null>(null);
  return (
    <div className="flex flex-col gap-[18px] px-8 py-7">
      <div className="text-xs text-text-muted">
        <Link href="/marketplace" className="text-accent-text">
          Marketplace
        </Link>{" "}
        / {t("From an image")}
      </div>
      <div className="text-[15px] font-semibold">
        {t("Install one instance from an image")}
      </div>
      <div className="max-w-3xl text-xs text-text-muted">
        {t(
          "For when you need a single instance and there is no maintained chart. Vinaire builds a Secret with a generated password, a Service and a StatefulSet from the publisher's image; you can read every object before it is created. It keeps no replicas and makes no backups.",
        )}
      </div>

      <div className="grid grid-cols-2 gap-3 md:grid-cols-4">
        {presets.map((p) => (
          <button
            key={p.id}
            type="button"
            onClick={() => setPreset(p)}
            className={`flex flex-col gap-1 rounded-xl border p-3 text-left ${
              preset?.id === p.id
                ? "border-accent bg-surface-2"
                : "border-border bg-surface hover:border-accent"
            }`}
          >
            <span className="font-mono text-[13px] font-semibold">
              {t(p.title)}
            </span>
            <span className="text-[11px] text-text-muted">{t(p.summary)}</span>
          </button>
        ))}
      </div>

      {preset && <ImageForm key={preset.id} preset={preset} />}
    </div>
  );
}

type StepState = "todo" | "doing" | "done" | "error";

function Field({
  label,
  hint,
  error,
  children,
}: {
  label: string;
  hint?: string;
  error?: string;
  children: React.ReactNode;
}) {
  return (
    <label className="flex flex-col gap-1">
      <span className="text-xs font-semibold">{t(label)}</span>
      {children}
      {error ? (
        <span className="text-[11px] text-err">{t(error)}</span>
      ) : hint ? (
        <span className="text-[11px] text-text-muted">{t(hint)}</span>
      ) : null}
    </label>
  );
}

function ImageForm({ preset }: { preset: Preset }) {
  const [form, setForm] = useState<Form>(() => newForm(preset));
  const [view, setView] = useState("");
  const [steps, setSteps] = useState<Record<string, StepState>>({});
  const [failure, setFailure] = useState<string | null>(null);
  const [running, setRunning] = useState(false);
  // generated once per page, kept only in memory until the Secret has been created
  const passwords = useRef<Record<string, string>>({});
  const doneSteps = useRef(new Set<string>());

  // the tags the registry has, so the version is chosen from a list instead of guessed
  const repo = preset.free ? (form.values.image ?? "") : preset.image;
  const [lookupRepo, setLookupRepo] = useState(repo);
  useEffect(() => {
    const id = setTimeout(() => setLookupRepo(repo), 500);
    return () => clearTimeout(id);
  }, [repo]);
  const tagList = useSWR(
    lookupRepo && /^[a-z0-9][a-z0-9._/:-]*$/.test(lookupRepo)
      ? ["image-tags", lookupRepo]
      : null,
    () => getImageTags(lookupRepo),
    { revalidateOnFocus: false, shouldRetryOnError: false },
  );
  const tags = useMemo(() => tagList.data?.tags ?? [], [tagList.data]);
  // a preset without a safe default takes the newest version the registry lists
  const eff: Form = form.tag
    ? form
    : { ...form, tag: tags.length > 0 ? tags[0].name : "" };

  const errors = validate(eff);
  const valid = Object.keys(errors).length === 0;
  const masked = {
    ...Object.fromEntries(preset.secrets.map((k) => [k, "<generated>"])),
    ...(preset.licenseFile
      ? { [preset.licenseFile.key]: "<your licence>" }
      : {}),
  };
  const docs: Doc[] = useMemo(
    () => (valid ? build(eff, masked) : []),
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [eff, valid],
  );
  const shown = docs.find((d) => d.kind === view) ?? docs[0];
  const finished =
    docs.length > 0 &&
    steps.StatefulSet === "done" &&
    (steps.Secret === "done" || !preset.secrets.length) &&
    (steps.Service === "done" || !docs.some((d) => d.kind === "Service"));

  const set = (patch: Partial<Form>) => setForm((f) => ({ ...f, ...patch }));
  const setValue = (id: string, v: string) =>
    setForm((f) => ({ ...f, values: { ...f.values, [id]: v } }));

  async function install() {
    setRunning(true);
    setFailure(null);
    for (const k of preset.secrets) {
      passwords.current[k] ||= randomPassword();
    }
    // a pasted licence goes into the Secret as it is typed here; it is in no environment value
    if (preset.licenseFile) {
      passwords.current[preset.licenseFile.key] = (
        eff.values.license ?? ""
      ).trim();
    }
    // The StatefulSet goes first: the Secret and the Service are made its dependents, so that
    // deleting the StatefulSet removes them too. (Until the Secret exists the pod waits for it.)
    const uid = async () => {
      const obj = await getResourceObject(
        "apps/v1/statefulsets",
        form.namespace,
        form.name,
      );
      const id = (parse(obj.yaml) as { metadata?: { uid?: string } })?.metadata
        ?.uid;
      if (!id) throw new Error("the StatefulSet has no uid yet");
      return id;
    };
    const first = build(eff, passwords.current).find(
      (d) => d.kind === "StatefulSet",
    )!;
    const plan: { key: string; run: () => Promise<unknown> }[] = [
      { key: "Namespace", run: () => ensureNamespace(form.namespace) },
      { key: "StatefulSet", run: () => createResource(first.yaml) },
      ...(["Secret", "Service"] as const).map((kind) => ({
        key: kind,
        run: async () => {
          const d = build(eff, passwords.current, await uid()).find(
            (x) => x.kind === kind,
          );
          if (d) await createResource(d.yaml);
        },
      })),
    ];
    for (const step of plan) {
      if (doneSteps.current.has(step.key)) continue;
      setSteps((s) => ({ ...s, [step.key]: "doing" }));
      try {
        // the write limiter answers 429 when a burst is spent: wait it out, as the bulk runs do
        for (let attempt = 0; ; attempt++) {
          try {
            await step.run();
            break;
          } catch (e) {
            if (e instanceof ApiError && e.status === 429 && attempt < 8) {
              await new Promise((r) => setTimeout(r, 1200));
              continue;
            }
            throw e;
          }
        }
        doneSteps.current.add(step.key);
        setSteps((s) => ({ ...s, [step.key]: "done" }));
      } catch (e) {
        setSteps((s) => ({ ...s, [step.key]: "error" }));
        setFailure(`${step.key}: ${applyErrorMessage(e)}`);
        break;
      }
    }
    setRunning(false);
  }

  const showStorage =
    (preset.dataPath !== undefined && form.persistent) ||
    (preset.free && !!form.values.mountPath);
  const host = `${form.name}.${form.namespace}.svc`;

  return (
    <div className="grid grid-cols-1 gap-6 xl:grid-cols-2">
      <div className="flex flex-col gap-4">
        {preset.note && (
          <div className="rounded-lg border border-border bg-surface-2 p-3 text-xs text-text-muted">
            {t(preset.note)}
          </div>
        )}
        <div className="grid grid-cols-2 gap-3">
          <Field
            label="Name"
            hint="Names the Secret, the Service and the StatefulSet. Also the address inside the cluster."
            error={errors.name}
          >
            <input
              className={inputClass}
              value={form.name}
              onChange={(e) => set({ name: e.target.value })}
            />
          </Field>
          <Field
            label="Namespace"
            hint="Created if it doesn't exist. Put it next to the application that will use it."
            error={errors.namespace}
          >
            <input
              className={inputClass}
              value={form.namespace}
              onChange={(e) => set({ namespace: e.target.value })}
            />
          </Field>
        </div>

        {preset.free &&
          preset.fields
            .filter((f) => f.id === "image")
            .map((f) => (
              <Field
                key={f.id}
                label={f.label}
                hint={f.hint}
                error={errors[f.id]}
              >
                <input
                  className={inputClass}
                  value={form.values[f.id] ?? ""}
                  onChange={(e) => setValue(f.id, e.target.value)}
                />
              </Field>
            ))}
        <Field label="Tag" hint={preset.tagHint} error={errors.tag}>
          <input
            className={inputClass}
            value={eff.tag}
            list="image-tags"
            onChange={(e) => set({ tag: e.target.value })}
          />
        </Field>
        <datalist id="image-tags">
          {tags.map((x) => (
            <option key={x.name} value={x.name} />
          ))}
        </datalist>
        {tags.length > 0 && (
          <div className="flex flex-wrap items-center gap-1.5 text-[11px]">
            <span className="text-text-muted">{t("Newest:")}</span>
            {tags.slice(0, 10).map((x) => (
              <button
                key={x.name}
                type="button"
                onClick={() => set({ tag: x.name })}
                className={`rounded-md border px-1.5 py-0.5 font-mono ${
                  eff.tag === x.name
                    ? "border-accent text-accent-text"
                    : "border-border text-text-muted hover:border-accent"
                }`}
              >
                {x.name}
              </button>
            ))}
          </div>
        )}
        {tags.length > 0 &&
          eff.tag &&
          !tags.some((x) => x.name === eff.tag) && (
            <div className="text-[11px] text-warn">
              {t(
                "This tag is not among the newest the registry lists — check that it exists, or the pod won't start (ImagePullBackOff).",
              )}
            </div>
          )}
        {tagList.error && (
          <div className="text-[11px] text-text-muted">
            {t(
              "The registry's tag list could not be read; type the tag yourself.",
            )}
          </div>
        )}
        {eff.tag === "latest" && (
          <div className="text-[11px] text-warn">
            {t(
              "latest moves without warning: the next restart may run another version.",
            )}
          </div>
        )}

        {preset.id === "keycloak" && (
          <PostgresPicker
            installNamespace={form.namespace}
            secretName={`${form.name || "keycloak"}-db`}
            onFill={(v) =>
              setForm((f) => ({ ...f, values: { ...f.values, ...v } }))
            }
          />
        )}

        {preset.fields
          .filter((f) => !(preset.free && f.id === "image"))
          .filter((f) => !f.showIf || f.showIf(form.values))
          .map((f) => (
            <Field
              key={f.id}
              label={f.label}
              hint={f.hint}
              error={errors[f.id]}
            >
              {f.secretText ? (
                <textarea
                  className={`${inputClass} min-h-20 font-mono`}
                  value={form.values[f.id] ?? ""}
                  autoComplete="off"
                  spellCheck={false}
                  onChange={(e) => setValue(f.id, e.target.value)}
                />
              ) : f.checkbox ? (
                <input
                  type="checkbox"
                  className="h-4 w-4 self-start"
                  checked={form.values[f.id] === "true"}
                  onChange={(e) =>
                    setValue(f.id, e.target.checked ? "true" : "")
                  }
                />
              ) : f.multiline ? (
                <textarea
                  className={`${inputClass} min-h-20 font-mono`}
                  value={form.values[f.id] ?? ""}
                  onChange={(e) => setValue(f.id, e.target.value)}
                />
              ) : (
                <input
                  className={inputClass}
                  value={form.values[f.id] ?? ""}
                  onChange={(e) => setValue(f.id, e.target.value)}
                />
              )}
            </Field>
          ))}

        {preset.tcp && (
          <div className="flex flex-col gap-2 rounded-lg border border-border bg-surface-2 p-3">
            <Field
              label="Reachable from outside the cluster"
              hint="This is plain TCP, not HTTP, so an Ingress can't carry it. Inside the cluster is safest; the other two open a port that anyone who can reach your nodes can try."
            >
              <select
                className={inputClass}
                value={form.expose}
                onChange={(e) =>
                  set({ expose: e.target.value as Form["expose"] })
                }
              >
                <option value="ClusterIP">
                  {t("No — only from inside the cluster")}
                </option>
                <option value="NodePort">
                  {t("On a port of every node (NodePort)")}
                </option>
                <option value="LoadBalancer">
                  {t("Through a load balancer (LoadBalancer)")}
                </option>
              </select>
            </Field>
            {form.expose === "NodePort" && (
              <Field
                label="Node port"
                hint="30000–32767. Empty: the cluster picks one; it is shown after the install."
                error={errors.nodePort}
              >
                <input
                  className={inputClass}
                  value={form.nodePort}
                  onChange={(e) => set({ nodePort: e.target.value })}
                />
              </Field>
            )}
            {form.expose !== "ClusterIP" && (
              <div className="text-[11px] text-warn">
                {t(
                  "The database will accept connections from outside with just its password. Close the port with a firewall to the addresses that need it.",
                )}
              </div>
            )}
          </div>
        )}

        {preset.dataPath !== undefined && (
          <label className="flex items-center gap-2 text-xs">
            <input
              type="checkbox"
              checked={form.persistent}
              onChange={(e) => set({ persistent: e.target.checked })}
            />
            {t(
              "Keep the data on a volume (without it everything is lost on restart)",
            )}
          </label>
        )}
        {showStorage && (
          <div className="grid grid-cols-2 gap-3">
            <Field
              label="Volume size"
              hint="Can't be shrunk later; growing depends on the storage class."
              error={errors.size}
            >
              <input
                className={inputClass}
                value={form.size}
                onChange={(e) => set({ size: e.target.value })}
              />
            </Field>
            <Field
              label="Storage class"
              hint="Empty: the cluster's default."
              error={errors.storageClass}
            >
              <input
                className={inputClass}
                value={form.storageClass}
                onChange={(e) => set({ storageClass: e.target.value })}
              />
            </Field>
          </div>
        )}

        <div className="grid grid-cols-3 gap-3">
          <Field
            label="CPU request"
            hint="What is reserved for it."
            error={errors.cpu}
          >
            <input
              className={inputClass}
              value={form.cpu}
              onChange={(e) => set({ cpu: e.target.value })}
            />
          </Field>
          <Field label="Memory request" hint="Reserved." error={errors.memory}>
            <input
              className={inputClass}
              value={form.memory}
              onChange={(e) => set({ memory: e.target.value })}
            />
          </Field>
          <Field
            label="Memory limit"
            hint="Above it the program is killed."
            error={errors.memoryLimit}
          >
            <input
              className={inputClass}
              value={form.memoryLimit}
              onChange={(e) => set({ memoryLimit: e.target.value })}
            />
          </Field>
        </div>

        <WizardWarnings form={form} preset={preset} />

        <div className="flex flex-col gap-2">
          <button
            type="button"
            disabled={!valid || running || finished}
            onClick={() => void install()}
            className="w-fit rounded-lg bg-accent px-4 py-2 text-[12.5px] font-semibold text-white disabled:opacity-50"
          >
            {running ? t("Installing…") : t("Install")}
          </button>
          {Object.keys(steps).length > 0 && (
            <ul className="flex flex-col gap-1 text-xs">
              {Object.entries(steps).map(([k, s]) => (
                <li key={k} className="flex items-center gap-2">
                  <span
                    className={
                      s === "done"
                        ? "text-ok"
                        : s === "error"
                          ? "text-err"
                          : "text-text-muted"
                    }
                  >
                    {s === "done" ? "✓" : s === "error" ? "✕" : "…"}
                  </span>
                  {t("Create")} {k}
                </li>
              ))}
            </ul>
          )}
          {failure && (
            <div className="text-xs text-err">
              {failure}
              <div className="mt-1 text-text-muted">
                {t(
                  "What was created stays; press Install again to continue with the rest.",
                )}
              </div>
            </div>
          )}
        </div>

        {steps.StatefulSet === "done" && (
          <InstanceWatch
            namespace={eff.namespace}
            name={eff.name}
            expose={preset.tcp ? eff.expose : "ClusterIP"}
          />
        )}

        {finished && (
          <div className="flex flex-col gap-2 rounded-lg border border-ok/40 bg-surface-2 p-3 text-xs">
            <div className="font-semibold text-ok">
              {t("Created. How to connect:")}
            </div>
            <table className="text-left">
              <tbody>
                {preset.connect(form.values, host, form.name).map(([k, v]) => (
                  <tr key={k}>
                    <td className="pr-4 text-text-muted">{k}</td>
                    <td className="font-mono">{v}</td>
                  </tr>
                ))}
              </tbody>
            </table>
            <div className="flex gap-4">
              <Link
                href={`/workloads/statefulsets/detail?namespace=${encodeURIComponent(form.namespace)}&name=${encodeURIComponent(form.name)}`}
                className="text-accent-text underline"
              >
                {t("Open the StatefulSet")}
              </Link>
              {preset.secrets.length > 0 && (
                <Link href="/secrets" className="text-accent-text underline">
                  {t("Show the password (Secrets)")}
                </Link>
              )}
            </div>
            <div className="text-text-muted">
              {t(
                "To remove it later: delete the StatefulSet — the Secret and the Service go with it. Its volume claim stays; delete it on the Storage page.",
              )}
            </div>
          </div>
        )}
      </div>

      <div className="flex min-w-0 flex-col gap-2">
        <div className="text-xs text-text-muted">
          {t("What will be created")} ·{" "}
          {valid ? imageOf(eff) : t("fix the fields marked in red")}
        </div>
        {docs.length > 0 && shown && (
          <>
            <PillTabs
              items={docs.map((d) => ({ key: d.kind, label: d.kind }))}
              active={shown.kind}
              onChange={setView}
            />
            <CodeBlock code={shown.yaml} />
          </>
        )}
      </div>
    </div>
  );
}
