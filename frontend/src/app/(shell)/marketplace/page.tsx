"use client";

import Link from "next/link";
import { useEffect, useState } from "react";
import {
  useCatalogRepos,
  useCatalogSearch,
  useReleases,
} from "@/lib/hooks/useCatalog";
import { useNamespaces } from "@/lib/hooks/useNamespaces";
import { uninstallRelease, type HelmRelease } from "@/lib/api/catalog";
import { friendlyErrorMessage } from "@/lib/api/errors";
import { applyErrorMessage } from "@/components/editor/useYamlApply";
import { PillTabs } from "@/components/ui/Tabs";
import { DataTable, type DataTableColumn } from "@/components/ui/DataTable";
import { StatusDot } from "@/components/ui/StatusDot";
import { TypeToConfirmModal } from "@/components/ui/TypeToConfirmModal";
import { SearchIcon } from "@/components/icons";
import { UpgradeModal } from "@/components/marketplace/UpgradeModal";
import { RollbackModal } from "@/components/marketplace/RollbackModal";
import type { StatusColor } from "@/lib/types";

type Tab = "catalog" | "installed";

const inputClass =
  "rounded-lg border border-border bg-surface-2 px-3 py-1.75 text-xs outline-none";

function releaseColor(status: string): StatusColor {
  if (status === "deployed") return "ok";
  if (status === "failed") return "err";
  return "warn";
}

export default function MarketplacePage() {
  const [tab, setTab] = useState<Tab>("catalog");
  return (
    <div className="flex flex-col gap-[18px] px-8 py-7">
      <div className="flex items-center justify-between">
        <div className="text-[15px] font-semibold">Marketplace</div>
        <PillTabs
          items={[
            { key: "catalog", label: "Catalog" },
            { key: "installed", label: "Installed" },
          ]}
          active={tab}
          onChange={setTab}
        />
      </div>
      {tab === "catalog" ? <CatalogTab /> : <InstalledTab />}
    </div>
  );
}

// ---------------------------------------------------------------- catalog

function CatalogTab() {
  const [input, setInput] = useState("");
  const [search, setSearch] = useState("");
  const [repo, setRepo] = useState("");
  const { data: repos } = useCatalogRepos();
  const { data, error, isLoading } = useCatalogSearch(search, repo);

  // Debounce typing so each keystroke isn't a request.
  useEffect(() => {
    const t = setTimeout(() => setSearch(input.trim()), 300);
    return () => clearTimeout(t);
  }, [input]);

  return (
    <>
      <div className="flex flex-wrap items-center gap-3">
        <label className="relative flex-1 min-w-[220px]">
          <span className="pointer-events-none absolute left-3 top-1/2 -translate-y-1/2 text-text-faint">
            <SearchIcon size={14} />
          </span>
          <input
            aria-label="Search charts"
            value={input}
            onChange={(e) => setInput(e.target.value)}
            placeholder="Search charts — ingress, postgres, metrics…"
            className={`${inputClass} w-full pl-8`}
          />
        </label>
        <select
          aria-label="Repository"
          value={repo}
          onChange={(e) => setRepo(e.target.value)}
          className={inputClass}
        >
          <option value="">All repositories</option>
          {repos?.repos.map((r) => (
            <option key={r.name} value={r.name}>
              {r.name}
            </option>
          ))}
        </select>
      </div>

      {data?.errors && Object.keys(data.errors).length > 0 && (
        <div className="rounded-lg border border-warn/30 bg-surface-2 p-3 text-xs text-warn">
          Some repositories couldn&apos;t be reached:
          <ul className="mt-1 list-disc pl-5 text-text-muted">
            {Object.entries(data.errors).map(([name, msg]) => (
              <li key={name}>
                <span className="font-mono">{name}</span> — {msg}
              </li>
            ))}
          </ul>
        </div>
      )}

      {error ? (
        <div className="text-sm text-err">
          Failed to load the catalog: {friendlyErrorMessage(error)}
        </div>
      ) : isLoading && !data ? (
        <div className="text-sm text-text-muted">Loading the catalog…</div>
      ) : data && data.charts.length === 0 ? (
        <div className="text-sm text-text-faint">
          No charts match.{" "}
          {repos &&
            `Repositories: ${repos.repos.map((r) => r.name).join(", ")}.`}
        </div>
      ) : (
        <div className="grid grid-cols-[repeat(auto-fill,minmax(260px,1fr))] gap-3.5">
          {data?.charts.map((c) => (
            <Link
              key={`${c.repo}/${c.name}`}
              href={`/marketplace/install?repo=${encodeURIComponent(c.repo)}&chart=${encodeURIComponent(c.name)}`}
              className="flex flex-col gap-2 rounded-[10px] border border-border bg-surface p-4 hover:border-accent"
            >
              <div className="flex items-start justify-between gap-2">
                <span className="truncate font-mono text-[13px] font-semibold">
                  {c.name}
                </span>
                <span className="shrink-0 font-mono text-[11px] text-text-muted">
                  {c.version}
                </span>
              </div>
              <div className="line-clamp-3 min-h-[3.2em] text-xs text-text-muted">
                {c.description || "No description."}
              </div>
              <div className="mt-auto flex items-center justify-between text-[11px] text-text-faint">
                <span>{c.repo}</span>
                {c.deprecated && <span className="text-warn">deprecated</span>}
                {!c.deprecated && c.appVersion && (
                  <span>app {c.appVersion}</span>
                )}
              </div>
            </Link>
          ))}
        </div>
      )}
    </>
  );
}

// -------------------------------------------------------------- installed

function InstalledTab() {
  const [ns, setNs] = useState("");
  const { data: namespaces } = useNamespaces();
  const { data, error, isLoading, mutate } = useReleases(ns || undefined);
  const [upgrading, setUpgrading] = useState<HelmRelease | null>(null);
  const [rollingBack, setRollingBack] = useState<HelmRelease | null>(null);
  const [removing, setRemoving] = useState<HelmRelease | null>(null);
  const [busy, setBusy] = useState(false);
  const [removeError, setRemoveError] = useState<string | null>(null);

  async function remove() {
    if (!removing) return;
    setBusy(true);
    setRemoveError(null);
    try {
      await uninstallRelease(removing.namespace, removing.name);
      setRemoving(null);
      await mutate();
    } catch (err) {
      setRemoveError(applyErrorMessage(err));
    } finally {
      setBusy(false);
    }
  }

  const actionClass =
    "rounded-md border border-border bg-surface-2 px-2.5 py-1 text-[11.5px] hover:border-accent";
  const columns: DataTableColumn<HelmRelease>[] = [
    {
      header: "Release",
      width: "1.4fr",
      render: (r) => (
        <span className="truncate pr-3 font-mono text-[12.5px]">{r.name}</span>
      ),
    },
    {
      header: "Namespace",
      width: "1.1fr",
      render: (r) => (
        <span className="font-mono text-xs text-text-muted">{r.namespace}</span>
      ),
    },
    {
      header: "Chart",
      width: "1.6fr",
      render: (r) => (
        <span className="truncate pr-3 font-mono text-xs">
          {r.chart}-{r.chartVersion}
          {r.appVersion && (
            <span className="text-text-faint"> · app {r.appVersion}</span>
          )}
        </span>
      ),
    },
    {
      header: "Status",
      width: ".9fr",
      render: (r) => (
        <span className="flex items-center gap-1.5 text-xs">
          <StatusDot color={releaseColor(r.status)} />
          {r.status}
        </span>
      ),
    },
    {
      header: "Rev",
      width: ".4fr",
      render: (r) => (
        <span className="font-mono text-xs text-text-muted">{r.revision}</span>
      ),
    },
    {
      header: "Updated",
      width: "1.2fr",
      render: (r) => (
        <span className="font-mono text-xs text-text-muted">
          {new Date(r.updated).toLocaleString()}
        </span>
      ),
    },
    {
      header: "",
      width: "2fr",
      render: (r) => (
        <span className="flex justify-end gap-2">
          <button
            type="button"
            className={actionClass}
            onClick={() => setUpgrading(r)}
          >
            Upgrade
          </button>
          <button
            type="button"
            className={actionClass}
            onClick={() => setRollingBack(r)}
            disabled={r.revision < 2}
            title={r.revision < 2 ? "There is no earlier revision" : undefined}
          >
            Rollback
          </button>
          <button
            type="button"
            className={`${actionClass} text-err`}
            onClick={() => {
              setRemoveError(null);
              setRemoving(r);
            }}
          >
            Uninstall
          </button>
        </span>
      ),
    },
  ];

  return (
    <>
      <div className="flex items-center gap-3">
        <select
          aria-label="Namespace"
          value={ns}
          onChange={(e) => setNs(e.target.value)}
          className={inputClass}
        >
          <option value="">All namespaces</option>
          {namespaces?.map((n) => (
            <option key={n.name} value={n.name}>
              {n.name}
            </option>
          ))}
        </select>
        <span className="text-xs text-text-faint">
          Helm releases visible to you. Helm keeps each release as a Secret in
          its namespace, so listing needs access to Secrets.
        </span>
      </div>

      {error ? (
        <div className="text-sm text-err">
          Failed to load releases: {friendlyErrorMessage(error)}
        </div>
      ) : (
        <DataTable
          columns={columns}
          rows={data ?? []}
          rowKey={(r) => `${r.namespace}/${r.name}`}
          isLoading={isLoading || !data}
          emptyLabel="No Helm releases. Install one from the Catalog tab."
        />
      )}

      {upgrading && (
        <UpgradeModal
          release={upgrading}
          onClose={() => setUpgrading(null)}
          onDone={() => void mutate()}
        />
      )}
      {rollingBack && (
        <RollbackModal
          release={rollingBack}
          onClose={() => setRollingBack(null)}
          onDone={() => void mutate()}
        />
      )}
      {removing && (
        <TypeToConfirmModal
          title={`Uninstall ${removing.name}`}
          message={`Removes release ${removing.name} and the resources its chart created in namespace ${removing.namespace}. Data in persistent volumes the chart created may be deleted with it. This can't be undone.`}
          expected={removing.name}
          confirmLabel="Uninstall"
          busy={busy}
          error={removeError}
          onConfirm={() => void remove()}
          onCancel={() => setRemoving(null)}
        />
      )}
    </>
  );
}
