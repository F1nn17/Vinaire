import { parse } from "yaml";
import { createResource } from "@/lib/api/resources";
import { ApiError } from "@/lib/api/http";
import { getResourceObject } from "@/lib/api/allResources";
import { build, type Form } from "./presets";

// Applies what an image preset describes (presets.ts `build`): the StatefulSet first, then its Secret
// and Service as its dependents, so that deleting the StatefulSet removes them. Every create is the
// generic YAML create of Phase 25, as the signed-in user — nothing here is a write path of its own.
// (The "Install from an image" page runs the same sequence step by step for its progress list; this is
// for flows that install more than one thing, such as Keycloak with its own database.)

// the write limiter answers 429 when a burst is spent: wait it out, as the bulk runs do
export async function withBackoff<T>(run: () => Promise<T>): Promise<T> {
  for (let attempt = 0; ; attempt++) {
    try {
      return await run();
    } catch (e) {
      if (e instanceof ApiError && e.status === 429 && attempt < 8) {
        await new Promise((r) => setTimeout(r, 1200));
        continue;
      }
      throw e;
    }
  }
}

export async function statefulSetUid(
  namespace: string,
  name: string,
): Promise<string> {
  const obj = await getResourceObject("apps/v1/statefulsets", namespace, name);
  const id = (parse(obj.yaml) as { metadata?: { uid?: string } })?.metadata
    ?.uid;
  if (!id) throw new Error("the StatefulSet has no uid yet");
  return id;
}

// Creates one instance; `passwords` holds the generated Secret values (key → value). Returns the
// StatefulSet's uid, for objects that should belong to it too.
export async function applyInstance(
  form: Form,
  passwords: Record<string, string>,
): Promise<string> {
  const sts = build(form, passwords).find((d) => d.kind === "StatefulSet");
  if (!sts) throw new Error("nothing to create");
  await withBackoff(() => createResource(sts.yaml));
  const uid = await statefulSetUid(form.namespace, form.name);
  for (const kind of ["Secret", "Service"] as const) {
    const d = build(form, passwords, uid).find((x) => x.kind === kind);
    if (d) await withBackoff(() => createResource(d.yaml));
  }
  return uid;
}

// How many replicas of a StatefulSet are ready (0 while it isn't there yet).
export async function readyReplicas(
  namespace: string,
  name: string,
): Promise<number> {
  try {
    const obj = await getResourceObject("apps/v1/statefulsets", namespace, name);
    const st = (parse(obj.yaml) as { status?: { readyReplicas?: number } })
      ?.status;
    return st?.readyReplicas ?? 0;
  } catch {
    return 0;
  }
}
