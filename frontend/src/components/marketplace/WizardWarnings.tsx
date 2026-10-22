"use client";

import { useInstallEnvironment } from "@/lib/api/installEnvironment";
import { t } from "@/lib/i18n/translate";
import type { Form, Preset } from "@/lib/images/presets";

// Roadmap Phase 75b: what is likely to go wrong with the instance the wizard is about to create,
// said before the button is pressed — a volume that will never be bound, an address that will stay
// pending, and (for anything that keeps data) that nobody makes a backup of it. Reads only.
export function WizardWarnings({
  form,
  preset,
}: {
  form: Form;
  preset: Preset;
}) {
  const { data: env } = useInstallEnvironment();
  const keepsData =
    (preset.dataPath !== undefined ||
      (preset.free && !!form.values.mountPath)) &&
    form.persistent;
  const lost = preset.dataPath !== undefined && !form.persistent;

  const items: { tone: "warn" | "info"; text: string }[] = [];
  if (env?.storageClassesKnown && keepsData) {
    if (form.storageClass && !env.storageClasses.includes(form.storageClass)) {
      items.push({
        tone: "warn",
        text: t("The StorageClass “{c}” doesn't exist in this cluster.", {
          c: form.storageClass,
        }),
      });
    } else if (!form.storageClass && !env.defaultStorageClass) {
      items.push({
        tone: "warn",
        text: t(
          "This cluster has no default StorageClass, so the volume will stay Pending and the pod will not start. Name a storage class below, or mark one as the default.",
        ),
      });
    }
  }
  if (
    preset.tcp &&
    form.expose === "LoadBalancer" &&
    env?.loadBalancerKnown &&
    !env.loadBalancer
  ) {
    items.push({
      tone: "warn",
      text: t(
        "Nothing in this cluster answers a LoadBalancer Service (no cloud provider, MetalLB or k3s service load balancer), so its external address will stay pending. Use a node port instead, or install a load balancer such as MetalLB.",
      ),
    });
  }
  if (lost) {
    items.push({
      tone: "warn",
      text: t(
        "Without a volume the data is lost whenever the pod is restarted or moved.",
      ),
    });
  }
  if (keepsData) {
    items.push({
      tone: "info",
      text: t(
        "There is no backup: Vinaire keeps none of this instance's data. It lives on one volume; if the volume or its node is lost, so is the data. For data you cannot lose, take dumps or volume snapshots yourself, or use an operator that makes backups (CloudNativePG for PostgreSQL).",
      ),
    });
  }
  if (items.length === 0) return null;
  return (
    <div className="flex flex-col gap-2" data-testid="wizard-warnings">
      {items.map((i) => (
        <div
          key={i.text}
          className={`rounded-lg border bg-surface-2 p-3 text-[11.5px] ${
            i.tone === "warn"
              ? "border-warn/40 text-warn"
              : "border-border text-text-muted"
          }`}
        >
          {i.text}
        </div>
      ))}
    </div>
  );
}
