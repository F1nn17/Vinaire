"use client";

import { t } from "@/lib/i18n/translate";
import { ChartIcon } from "@/components/marketplace/ChartIcon";
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
import { DeletePVCModal } from "@/components/storage/DeletePVCModal";
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
  const [category, setCategory] = useState("");
  const { data: repos } = useCatalogRepos();
  const [showDeprecated, setShowDeprecated] = useState(false);
  const { data, error, isLoading } = useCatalogSearch(
    search,
    repo,
    category,
    showDeprecated,
  );
  const categories = Array.from(
    new Set(
      repos?.repos.map((r) => r.category).filter((c): c is string => !!c),
    ),
  );

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
          {repos?.repos
            .filter((r) => !category || r.category === category)
            .map((r) => (
              <option key={r.name} value={r.name}>
                {r.name}
                {r.oci ? " (OCI)" : ""}
              </option>
            ))}
        </select>
      </div>

      {(showDeprecated || !!data?.hiddenDeprecated) && (
        <label className="flex items-center gap-2 text-xs text-text-muted">
          <input
            type="checkbox"
            checked={showDeprecated}
            onChange={(e) => setShowDeprecated(e.target.checked)}
          />
          Show deprecated charts
          {!showDeprecated && data?.hiddenDeprecated
            ? t(" ({n} hidden — no longer maintained, may be unsafe)", {
                n: data.hiddenDeprecated,
              })
            : ""}
        </label>
      )}

      {categories.length > 0 && (
        <div className="flex flex-wrap gap-2">
          {["", ...categories].map((c) => (
            <button
              key={c || "all"}
              type="button"
              onClick={() => {
                setCategory(c);
                setRepo("");
              }}
              className={`rounded-full border px-3 py-1 text-[12px] ${
                category === c
                  ? "border-accent bg-surface-2 text-accent-text"
                  : "border-border text-text-muted hover:border-accent"
              }`}
            >
              {c || "All"}
            </button>
          ))}
        </div>
      )}

      {data?.pending && data.pending.length > 0 && (
        <div className="rounded-lg border border-border bg-surface-2 p-3 text-xs text-text-muted">
          Still loading {data.pending.length} repositor
          {data.pending.length === 1 ? "y" : "ies"} (
          <span className="font-mono">{data.pending.join(", ")}</span>) — the
          list updates by itself.
        </div>
      )}

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
        <>
          {data && data.total > data.charts.length && (
            <div className="text-xs text-text-faint">
              Showing the {data.charts.length} best of {data.total} — narrow it
              down with a search, a category or a repository.
            </div>
          )}
          <div className="grid grid-cols-[repeat(auto-fill,minmax(260px,1fr))] gap-3.5">
            {data?.charts.map((c) => (
              <Link
                key={`${c.repo}/${c.name}`}
                href={`/marketplace/install?repo=${encodeURIComponent(c.repo)}&chart=${encodeURIComponent(c.name)}`}
                className="flex flex-col gap-2 rounded-[10px] border border-border bg-surface p-4 hover:border-accent"
              >
                <div className="flex items-center gap-2.5">
                  <ChartIcon repo={c.repo} chart={c.name} hasIcon={c.hasIcon} />
                  <span className="min-w-0 flex-1 truncate font-mono text-[13px] font-semibold">
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
                  {c.deprecated && (
                    <span className="text-warn">deprecated</span>
                  )}
                  {!c.deprecated && c.appVersion && (
                    <span>app {c.appVersion}</span>
                  )}
                </div>
              </Link>
            ))}
          </div>
        </>
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
  // volume claims Helm left behind by the last uninstall (Roadmap Phase 59)
  const [left, setLeft] = useState<{
    release: string;
    namespace: string;
    claims: string[];
  } | null>(null);
  const [deletingClaim, setDeletingClaim] = useState<string | null>(null);

  async function remove() {
    if (!removing) return;
    setBusy(true);
    setRemoveError(null);
    try {
      const res = await uninstallRelease(removing.namespace, removing.name);
      setLeft(
        res?.leftovers && res.leftovers.length > 0
          ? {
              release: removing.name,
              namespace: removing.namespace,
              claims: res.leftovers,
            }
          : null,
      );
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

      {left && (
        <div className="mb-3 flex flex-col gap-2 rounded-lg border border-warn/40 bg-surface p-3 text-xs">
          <div>
            {t("{r} was uninstalled.", { r: left.release })}{" "}
            {t(
              "Helm leaves these volume claims (your data) in place — delete the ones you don't need:",
            )}
          </div>
          <ul className="flex flex-col gap-1 font-mono">
            {left.claims.map((c) => (
              <li key={c} className="flex items-center gap-3">
                <span>{c}</span>
                <button
                  type="button"
                  onClick={() => setDeletingClaim(c)}
                  className="rounded-md border border-err/50 px-2 py-0.5 text-[11px] text-err hover:border-err"
                >
                  Delete
                </button>
              </li>
            ))}
          </ul>
          <div className="flex items-center gap-3">
            <a
              href={`/namespaces/detail?name=${encodeURIComponent(left.namespace)}`}
              className="text-accent-text underline"
            >
              {t("Open namespace {ns}", { ns: left.namespace })}
            </a>
            <button
              type="button"
              onClick={() => setLeft(null)}
              className="text-text-muted hover:text-text"
            >
              Dismiss
            </button>
          </div>
        </div>
      )}

      {left && deletingClaim && (
        <DeletePVCModal
          namespace={left.namespace}
          name={deletingClaim}
          onClose={() => setDeletingClaim(null)}
          onDeleted={() => {
            const rest = left.claims.filter((c) => c !== deletingClaim);
            setLeft(rest.length > 0 ? { ...left, claims: rest } : null);
            setDeletingClaim(null);
          }}
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
          title={t("Uninstall {name}", { name: removing.name })}
          message={t(
            "Removes release {name} and the resources its chart created in namespace {ns}. Volume claims that a StatefulSet created stay and are listed afterwards. This can't be undone.",
            { name: removing.name, ns: removing.namespace },
          )}
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
