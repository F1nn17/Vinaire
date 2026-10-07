"use client";

import Link from "next/link";
import { useSearchParams } from "next/navigation";
import { Suspense, useState } from "react";
import { friendlyErrorMessage } from "@/lib/api/errors";
import { parseResourceKey, useResourceObject } from "@/lib/api/allResources";
import { UnderlineTabs } from "@/components/ui/Tabs";
import { FillHeight } from "@/components/ui/FillHeight";
import { YamlPanel } from "@/components/editor/YamlPanel";
import { YamlEditor } from "@/components/editor/YamlEditor";

type Tab = "overview" | "yaml";

const tabs: { key: Tab; label: string }[] = [
  { key: "overview", label: "Overview" },
  { key: "yaml", label: "YAML" },
];

// A static export can't pre-render a page per object name, so this single page
// reads `?r=<group>/<version>/<resource>&ns=&name=` (see AGENTS.md's "Rendering model").
export default function ResourceDetailRoute() {
  return (
    <Suspense
      fallback={<div className="p-8 text-sm text-text-muted">Loading…</div>}
    >
      <ResourceDetail />
    </Suspense>
  );
}

function ResourceDetail() {
  const params = useSearchParams();
  const key = parseResourceKey(params.get("r")) ? params.get("r") : null;
  const namespace = params.get("ns") ?? "";
  const name = params.get("name") ?? "";
  const { data, error, isLoading, mutate } = useResourceObject(
    key,
    namespace,
    name,
  );
  const [tab, setTab] = useState<Tab>("overview");

  if (!key || !name) {
    return (
      <div className="p-8 text-sm text-text-muted">
        Pick an object from{" "}
        <Link href="/resources" className="text-accent-text underline">
          All resources
        </Link>
        .
      </div>
    );
  }
  if (error) {
    return (
      <div className="p-8 text-sm text-err">
        Failed to load: {friendlyErrorMessage(error)}
      </div>
    );
  }
  if (isLoading || !data) {
    return <div className="p-8 text-sm text-text-muted">Loading…</div>;
  }

  // A Secret's values are redacted in this view, so it must never be saved back
  // from here — the backend refuses it too. Values: the Secrets page's audited reveal.
  const isSecret = key === "core/v1/secrets";
  const back = `/resources?r=${encodeURIComponent(key)}${namespace ? `&ns=${encodeURIComponent(namespace)}` : ""}`;

  return (
    <div className="flex flex-col gap-[18px] px-8 py-7">
      <div>
        <div className="font-mono text-xs text-text-faint">
          <Link href="/resources">All resources</Link> /{" "}
          <Link href={back}>{data.kind}</Link>
        </div>
        <div className="mt-1.5 flex items-center gap-2.5">
          <span className="font-mono text-lg font-semibold">{data.name}</span>
          {data.namespace && (
            <span className="rounded-full border border-border bg-surface px-2.5 py-0.75 font-mono text-xs text-text-muted">
              {data.namespace}
            </span>
          )}
        </div>
      </div>

      <UnderlineTabs items={tabs} active={tab} onChange={setTab} />

      {tab === "overview" && (
        <div className="max-w-xl rounded-[10px] border border-border bg-surface p-5">
          <div className="mb-3.5 text-[13px] font-semibold">Metadata</div>
          <div className="flex flex-col gap-2.5">
            <Row label="Kind" value={data.kind} />
            {data.namespace && <Row label="Namespace" value={data.namespace} />}
            <Row label="Age" value={data.age} />
          </div>
          {Object.keys(data.labels ?? {}).length > 0 && (
            <div className="mt-4 border-t border-border pt-4">
              <div className="mb-2 text-xs text-text-muted">Labels</div>
              <div className="flex flex-wrap gap-1.5">
                {Object.entries(data.labels).map(([k, v]) => (
                  <span
                    key={k}
                    className="rounded-md border border-border bg-surface-2 px-2 py-1 font-mono text-[11px]"
                  >
                    {k}={v}
                  </span>
                ))}
              </div>
            </div>
          )}
        </div>
      )}

      {tab === "yaml" &&
        (isSecret ? (
          <>
            <div className="rounded-lg border border-border bg-surface-2 p-3 text-xs text-text-muted">
              Secret values are hidden here and this view is read-only. Use the
              Secrets page to reveal a value (audited).
            </div>
            <FillHeight className="overflow-hidden rounded-[10px] border border-border">
              <YamlEditor value={data.yaml} readOnly height="100%" />
            </FillHeight>
          </>
        ) : (
          <YamlPanel yaml={data.yaml} onApplied={() => mutate()} />
        ))}
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
