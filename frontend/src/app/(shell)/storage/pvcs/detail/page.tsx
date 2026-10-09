"use client";

import Link from "next/link";
import { useRouter, useSearchParams } from "next/navigation";
import { Suspense, useState } from "react";
import { usePVC } from "@/lib/hooks/useStorage";
import { friendlyErrorMessage } from "@/lib/api/errors";
import { StatusDot } from "@/components/ui/StatusDot";
import { UnderlineTabs } from "@/components/ui/Tabs";
import { YamlPanel } from "@/components/editor/YamlPanel";
import { EventRow } from "@/components/ui/EventRow";
import { ActionBar, ActionButton } from "@/components/ui/ActionBar";
import { TrashIcon } from "@/components/icons";
import { DeletePVCModal } from "@/components/storage/DeletePVCModal";

type Tab = "overview" | "events" | "yaml";

const tabs: { key: Tab; label: string }[] = [
  { key: "overview", label: "Overview" },
  { key: "events", label: "Events" },
  { key: "yaml", label: "YAML" },
];

// A static export can't pre-render a page per PVC name (unknowable at build
// time) — this route is a single static page, `/storage/pvcs/detail`, that
// reads which PVC to show from `?namespace=&name=` instead of a
// `[namespace]/[name]` route segment. See AGENTS.md's "Rendering model".
export default function PVCDetailPageRoute() {
  return (
    <Suspense
      fallback={<div className="p-8 text-sm text-text-muted">Loading…</div>}
    >
      <PVCDetailPage />
    </Suspense>
  );
}

function PVCDetailPage() {
  const searchParams = useSearchParams();
  const namespace = searchParams.get("namespace") ?? "";
  const name = searchParams.get("name") ?? "";
  const { data: pvc, isLoading, error, mutate } = usePVC(namespace, name);
  const [tab, setTab] = useState<Tab>("overview");
  const router = useRouter();
  const [deleting, setDeleting] = useState(false);

  if (error) {
    return (
      <div className="p-8 text-sm text-err">
        Failed to load PVC: {friendlyErrorMessage(error)}
      </div>
    );
  }

  if (isLoading) {
    return <div className="p-8 text-sm text-text-muted">Loading…</div>;
  }

  if (!pvc) {
    return (
      <div className="p-8 text-sm text-text-muted">
        PVC &ldquo;{namespace}/{name}&rdquo; not found.
      </div>
    );
  }

  return (
    <div className="flex flex-col gap-[18px] px-8 py-7">
      <div className="flex items-start justify-between">
        <div>
          <div className="font-mono text-xs text-text-faint">
            <Link href="/storage">Storage</Link> /{" "}
            <Link href="/storage">PVCs</Link>
          </div>
          <div className="mt-1.5 flex items-center gap-2.5">
            <span className="font-mono text-lg font-semibold">{pvc.name}</span>
            <span className="flex items-center gap-1.5 rounded-full border border-border bg-surface px-2.5 py-[3px] text-xs text-text-muted">
              <StatusDot color={pvc.statusColor} />
              {pvc.status}
            </span>
          </div>
        </div>
        <ActionBar>
          <ActionButton
            icon={<TrashIcon />}
            label="Delete"
            hint="Delete the volume claim — the data on it is lost with the usual reclaim policy"
            danger
            onClick={() => setDeleting(true)}
          />
        </ActionBar>
      </div>

      <UnderlineTabs items={tabs} active={tab} onChange={setTab} />

      {tab === "overview" && (
        <div className="grid grid-cols-[1.3fr_1fr] gap-5">
          <div className="rounded-[10px] border border-border bg-surface p-5">
            <div className="mb-3.5 text-[13px] font-semibold">Metadata</div>
            <div className="flex flex-col gap-2.5">
              <Row label="Namespace" value={pvc.namespace} />
              <Row label="Capacity" value={pvc.capacity} />
              <Row label="Access modes" value={pvc.accessModes} />
              <Row label="Storage class" value={pvc.storageClass} />
              <Row label="Volume" value={pvc.volumeName} />
              <Row label="Age" value={pvc.age} />
            </div>
            <div className="mt-4 border-t border-border pt-4">
              <div className="mb-2 text-xs text-text-muted">Labels</div>
              <div className="flex flex-wrap gap-1.5">
                {Object.entries(pvc.labels).map(([k, v]) => (
                  <span
                    key={k}
                    className="rounded-md border border-border bg-surface-2 px-2 py-1 font-mono text-[11px]"
                  >
                    {k}={v}
                  </span>
                ))}
              </div>
            </div>
          </div>

          <div className="rounded-[10px] border border-border bg-surface p-[18px]">
            <div className="mb-2.5 text-[13px] font-semibold">Mounted by</div>
            {pvc.mountedBy.length === 0 ? (
              <div className="text-xs text-text-muted">
                No pods currently mount this PVC.
              </div>
            ) : (
              <div className="flex flex-col gap-2">
                {pvc.mountedBy.map((p) => (
                  <Link
                    key={`${p.namespace}/${p.name}`}
                    href={`/workloads/pods/detail?namespace=${encodeURIComponent(p.namespace)}&name=${encodeURIComponent(p.name)}`}
                    className="rounded-md border border-border bg-surface-2 px-2.5 py-1.5 font-mono text-[12px] hover:bg-surface"
                  >
                    {p.name}
                  </Link>
                ))}
              </div>
            )}
          </div>
        </div>
      )}

      {tab === "events" && (
        <div className="flex flex-col rounded-[10px] border border-border bg-surface p-5">
          {pvc.events.map((e, i) => (
            <EventRow key={i} {...e} />
          ))}
        </div>
      )}

      {tab === "yaml" && (
        <YamlPanel yaml={pvc.yaml} onApplied={() => mutate()} />
      )}
      {deleting && (
        <DeletePVCModal
          namespace={pvc.namespace}
          name={pvc.name}
          onClose={() => setDeleting(false)}
          onDeleted={() => router.push("/storage")}
        />
      )}
    </div>
  );
}

function Row({ label, value }: { label: string; value: string }) {
  return (
    <div className="flex justify-between">
      <span className="text-xs text-text-muted">{label}</span>
      <span className="font-mono text-xs">{value}</span>
    </div>
  );
}
