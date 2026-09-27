"use client";

import Link from "next/link";
import { useSearchParams } from "next/navigation";
import { Suspense, useState } from "react";
import { useCRDInstance, useCRDs } from "@/lib/hooks/useCRDs";
import { friendlyErrorMessage } from "@/lib/api/errors";
import { UnderlineTabs } from "@/components/ui/Tabs";
import { CodeBlock } from "@/components/ui/CodeBlock";

type Tab = "overview" | "yaml";

const tabs: { key: Tab; label: string }[] = [
  { key: "overview", label: "Overview" },
  { key: "yaml", label: "YAML" },
];

// A static export can't pre-render a page per CRD instance name (unknowable
// at build time) — this route is a single static page,
// `/custom-resources/detail`, that reads which instance to show from
// `?crd=&namespace=&name=` instead of a route segment. See AGENTS.md's
// "Rendering model".
export default function CustomResourceDetailPageRoute() {
  return (
    <Suspense
      fallback={<div className="p-8 text-sm text-text-muted">Loading…</div>}
    >
      <CustomResourceDetailPage />
    </Suspense>
  );
}

function CustomResourceDetailPage() {
  const searchParams = useSearchParams();
  const crdName = searchParams.get("crd") ?? "";
  const namespace = searchParams.get("namespace") ?? "";
  const name = searchParams.get("name") ?? "";

  const { data: crds } = useCRDs();
  const crd = crds?.find((c) => c.name === crdName);
  const {
    data: instance,
    isLoading,
    error,
  } = useCRDInstance(crdName, name, namespace);
  const [tab, setTab] = useState<Tab>("overview");

  if (error) {
    return (
      <div className="p-8 text-sm text-err">
        Failed to load instance: {friendlyErrorMessage(error)}
      </div>
    );
  }

  if (isLoading) {
    return <div className="p-8 text-sm text-text-muted">Loading…</div>;
  }

  if (!instance) {
    return (
      <div className="p-8 text-sm text-text-muted">
        Instance &ldquo;{namespace ? `${namespace}/` : ""}
        {name}&rdquo; not found.
      </div>
    );
  }

  const listHref = `/custom-resources?crd=${encodeURIComponent(crdName)}`;

  return (
    <div className="flex flex-col gap-[18px] px-8 py-7">
      <div>
        <div className="font-mono text-xs text-text-faint">
          <Link href="/custom-resources">Custom Resources</Link> /{" "}
          <Link href={listHref}>{crd?.kind ?? instance.kind}</Link>
        </div>
        <div className="mt-1.5 flex items-center gap-2.5">
          <span className="font-mono text-lg font-semibold">
            {instance.name}
          </span>
          <span className="rounded-full border border-border bg-surface px-2.5 py-[3px] text-xs text-text-muted">
            {instance.kind}
          </span>
        </div>
      </div>

      <UnderlineTabs items={tabs} active={tab} onChange={setTab} />

      {tab === "overview" && (
        <div className="rounded-[10px] border border-border bg-surface p-5">
          <div className="mb-3.5 text-[13px] font-semibold">Metadata</div>
          <div className="flex flex-col gap-2.5">
            <Row label="Namespace" value={instance.namespace || "—"} />
            <Row label="Kind" value={instance.kind} />
            <Row label="Age" value={instance.age} />
          </div>
          <div className="mt-4 border-t border-border pt-4">
            <div className="mb-2 text-xs text-text-muted">Labels</div>
            <div className="flex flex-wrap gap-1.5">
              {Object.entries(instance.labels).map(([k, v]) => (
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
      )}

      {tab === "yaml" && <CodeBlock code={instance.yaml} />}
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
