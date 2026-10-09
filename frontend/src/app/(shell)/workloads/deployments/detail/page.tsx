"use client";

import { t } from "@/lib/i18n/translate";
import Link from "next/link";
import { useSearchParams, useRouter } from "next/navigation";
import { Suspense, useState } from "react";
import { useDeployment } from "@/lib/hooks/useWorkloads";
import { friendlyErrorMessage } from "@/lib/api/errors";
import { StatusDot } from "@/components/ui/StatusDot";
import { WorkloadOverview } from "@/components/workloads/WorkloadOverview";
import { UnderlineTabs } from "@/components/ui/Tabs";
import { YamlPanel } from "@/components/editor/YamlPanel";
import { FillHeight } from "@/components/ui/FillHeight";
import { ConfirmModal } from "@/components/ui/ConfirmModal";
import { Toast, type ToastState } from "@/components/ui/Toast";
import { ActionBar, ActionButton } from "@/components/ui/ActionBar";
import { ScaleModal } from "@/components/ui/ScaleModal";
import {
  HistoryIcon,
  PauseIcon,
  PlayIcon,
  RestartIcon,
  ScaleIcon,
  TrashIcon,
} from "@/components/icons";
import {
  deleteDeployment,
  pauseDeploymentRollout,
  restartDeployment,
  resumeDeploymentRollout,
  rollbackDeployment,
  scaleDeployment,
} from "@/lib/api/deploymentActions";

type PendingAction =
  | { kind: "restart" }
  | { kind: "delete" }
  | { kind: "rollback"; revision: number };

type Tab = "overview" | "revisions" | "yaml";

const tabs: { key: Tab; label: string }[] = [
  { key: "overview", label: "Overview" },
  { key: "revisions", label: "Revisions" },
  { key: "yaml", label: "YAML" },
];

// A static export can't pre-render a page per deployment name (unknowable
// at build time) — this route is a single static page,
// `/workloads/deployments/detail`, that reads which deployment to show
// from `?namespace=&name=` instead of a `[namespace]/[name]` route
// segment. See AGENTS.md's "Rendering model".
export default function DeploymentDetailPageRoute() {
  return (
    <Suspense
      fallback={<div className="p-8 text-sm text-text-muted">Loading…</div>}
    >
      <DeploymentDetailPage />
    </Suspense>
  );
}

function DeploymentDetailPage() {
  const router = useRouter();
  const searchParams = useSearchParams();
  const deploymentNamespace = searchParams.get("namespace") ?? "";
  const deploymentName = searchParams.get("name") ?? "";
  const {
    data: deployment,
    isLoading,
    error,
    mutate,
  } = useDeployment(deploymentNamespace, deploymentName);
  const [tab, setTab] = useState<Tab>("overview");
  const [pending, setPending] = useState<PendingAction | null>(null);
  const [busy, setBusy] = useState(false);
  const [toast, setToast] = useState<ToastState>(null);
  const [scaleOpen, setScaleOpen] = useState(false);
  const [scaleBusy, setScaleBusy] = useState(false);

  async function handleOpenScale() {
    // Refetch *before* mounting the modal, so a stale number can't
    // silently overwrite a concurrent `kubectl scale` — ScaleModal seeds
    // its input once, from whatever `deployment.desiredReplicas` is at
    // mount time.
    await mutate();
    setScaleOpen(true);
  }

  async function handleScale(replicas: number) {
    setScaleBusy(true);
    try {
      await scaleDeployment(deploymentNamespace, deploymentName, replicas);
      setToast({
        type: "success",
        message:
          replicas === 1
            ? t("Scaled to 1 replica.")
            : t("Scaled to {n} replicas.", { n: replicas }),
      });
      setScaleOpen(false);
      await mutate();
    } catch (err) {
      setToast({ type: "error", message: friendlyErrorMessage(err) });
    } finally {
      setScaleBusy(false);
    }
  }

  async function handlePauseResume() {
    try {
      if (deployment?.paused) {
        await resumeDeploymentRollout(deploymentNamespace, deploymentName);
        setToast({ type: "success", message: "Rollout resumed." });
      } else {
        await pauseDeploymentRollout(deploymentNamespace, deploymentName);
        setToast({ type: "success", message: "Rollout paused." });
      }
      await mutate();
    } catch (err) {
      setToast({ type: "error", message: friendlyErrorMessage(err) });
    }
  }

  async function handleConfirm() {
    if (!pending) return;
    setBusy(true);
    try {
      if (pending.kind === "restart") {
        await restartDeployment(deploymentNamespace, deploymentName);
        setToast({ type: "success", message: "Rollout restarted." });
        await mutate();
      } else if (pending.kind === "delete") {
        await deleteDeployment(deploymentNamespace, deploymentName);
        setToast({ type: "success", message: "Deployment deleted." });
        router.push("/workloads");
      } else {
        await rollbackDeployment(
          deploymentNamespace,
          deploymentName,
          pending.revision,
        );
        setToast({
          type: "success",
          message: `Rolled back to revision ${pending.revision}.`,
        });
        await mutate();
      }
      setPending(null);
    } catch (err) {
      setToast({
        type: "error",
        message: friendlyErrorMessage(err),
      });
    } finally {
      setBusy(false);
    }
  }

  if (error) {
    return (
      <div className="p-8 text-sm text-err">
        Failed to load deployment: {friendlyErrorMessage(error)}
      </div>
    );
  }

  if (isLoading) {
    return <div className="p-8 text-sm text-text-muted">Loading…</div>;
  }

  if (!deployment) {
    return (
      <div className="p-8 text-sm text-text-muted">
        Deployment &ldquo;{deploymentNamespace}/{deploymentName}&rdquo; not
        found.
      </div>
    );
  }

  return (
    <div className="flex flex-col gap-[18px] px-8 py-7">
      <div className="flex items-start justify-between">
        <div>
          <div className="font-mono text-xs text-text-faint">
            <Link href="/workloads">Workloads</Link> /{" "}
            <Link href="/workloads?tab=deployments">Deployments</Link>
          </div>
          <div className="mt-1.5 flex items-center gap-2.5">
            <span className="font-mono text-lg font-semibold">
              {deployment.name}
            </span>
            <span className="flex items-center gap-1.5 rounded-full border border-border bg-surface px-2.5 py-[3px] text-xs text-text-muted">
              <StatusDot color={deployment.statusColor} />
              {deployment.readySummary}
            </span>
            {deployment.paused && (
              <span className="rounded-full border border-warn px-2.5 py-[3px] text-xs text-warn">
                Rollout paused
              </span>
            )}
          </div>
        </div>
        <ActionBar>
          <ActionButton
            icon={<ScaleIcon />}
            label="Scale"
            hint="Change the number of replicas"
            onClick={handleOpenScale}
          />
          <ActionButton
            icon={<RestartIcon />}
            label="Restart rollout"
            hint="Replace the pods one by one, with no downtime"
            onClick={() => setPending({ kind: "restart" })}
          />
          <ActionButton
            icon={deployment.paused ? <PlayIcon /> : <PauseIcon />}
            label={deployment.paused ? "Resume rollout" : "Pause rollout"}
            hint={
              deployment.paused
                ? "Let pending changes roll out again"
                : "Hold the rollout: later changes wait until you resume"
            }
            onClick={handlePauseResume}
          />
          <ActionButton
            icon={<HistoryIcon />}
            label="Revisions"
            hint="See earlier versions and roll back to one"
            onClick={() => setTab("revisions")}
          />
          <ActionButton
            icon={<TrashIcon />}
            label="Delete"
            hint="Remove the deployment and all its pods — can't be undone"
            danger
            onClick={() => setPending({ kind: "delete" })}
          />
        </ActionBar>
      </div>

      <UnderlineTabs items={tabs} active={tab} onChange={setTab} />

      {tab === "overview" && (
        <WorkloadOverview
          resourceType="deployments"
          namespace={deploymentNamespace}
          name={deploymentName}
          pods={deployment.pods}
        >
          <div className="rounded-[10px] border border-border bg-surface p-5">
            <div className="mb-3.5 text-[13px] font-semibold">Metadata</div>
            <div className="flex flex-col gap-2.5">
              <Row label="Namespace" value={deployment.namespace} />
              <Row label="Replicas" value={deployment.replicas} />
              <Row label="Strategy" value={deployment.strategy} />
              <Row label="Image" value={deployment.image} />
              <Row label="Managed by" value={deployment.managedBy} />
              <Row label="Age" value={deployment.age} />
            </div>
          </div>
        </WorkloadOverview>
      )}

      {tab === "revisions" && (
        <FillHeight className="max-w-xl overflow-auto">
          <div className="flex flex-col gap-2.5 pr-1">
            {deployment.revisions.map((rev) => (
              <div
                key={rev.revision}
                className="flex items-center justify-between rounded-[10px] border p-4"
                style={{
                  borderColor: rev.current ? "var(--accent)" : "var(--border)",
                  background: "var(--surface)",
                }}
              >
                <div>
                  <div className="text-[13px] font-semibold">
                    Revision {rev.revision}{" "}
                    {rev.current && (
                      <span className="text-[11px] font-normal text-accent-text">
                        · current
                      </span>
                    )}
                  </div>
                  <div className="mt-0.5 font-mono text-[11px] text-text-faint">
                    {rev.image} · {rev.age}
                  </div>
                </div>
                {!rev.current && (
                  <button
                    type="button"
                    onClick={() =>
                      setPending({ kind: "rollback", revision: rev.revision })
                    }
                    className="rounded-md border border-border bg-surface-2 px-3 py-1.5 text-xs"
                  >
                    Rollback
                  </button>
                )}
              </div>
            ))}
          </div>
        </FillHeight>
      )}

      {tab === "yaml" && (
        <YamlPanel yaml={deployment.yaml} onApplied={() => mutate()} />
      )}

      <ConfirmModal
        open={pending !== null}
        title={confirmTitle(pending)}
        message={confirmMessage(pending, deploymentName)}
        confirmLabel={pending?.kind === "delete" ? "Delete" : "Confirm"}
        danger={pending?.kind === "delete"}
        busy={busy}
        onConfirm={handleConfirm}
        onCancel={() => setPending(null)}
      />
      {scaleOpen && (
        <ScaleModal
          resourceName={deploymentName}
          currentReplicas={deployment.desiredReplicas}
          busy={scaleBusy}
          onApply={handleScale}
          onCancel={() => setScaleOpen(false)}
        />
      )}
      <Toast toast={toast} onDismiss={() => setToast(null)} />
    </div>
  );
}

function confirmTitle(pending: PendingAction | null): string {
  if (!pending) return "";
  if (pending.kind === "restart") return "Restart rollout?";
  if (pending.kind === "delete") return "Delete deployment?";
  return "Rollback deployment?";
}

function confirmMessage(pending: PendingAction | null, name: string): string {
  if (!pending) return "";
  if (pending.kind === "restart") {
    return `This will trigger a rolling restart of "${name}". Pods will be recreated one by one.`;
  }
  if (pending.kind === "delete") {
    return `This permanently deletes "${name}" and all its pods. This cannot be undone.`;
  }
  return `This will roll "${name}" back to revision ${pending.revision}, replacing its current pod template.`;
}

function Row({ label, value }: { label: string; value: string }) {
  return (
    <div className="flex justify-between">
      <span className="text-xs text-text-muted">{label}</span>
      <span className="font-mono text-xs">{value}</span>
    </div>
  );
}
