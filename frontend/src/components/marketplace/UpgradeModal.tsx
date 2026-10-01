"use client";

import { useEffect, useState } from "react";
import {
  getReleaseValues,
  upgradeRelease,
  type HelmRelease,
} from "@/lib/api/catalog";
import { useCatalogRepos, useChart } from "@/lib/hooks/useCatalog";
import { applyErrorMessage } from "@/components/editor/useYamlApply";
import { YamlEditor } from "@/components/editor/YamlEditor";

const inputClass =
  "rounded-lg border border-border bg-surface-2 px-3 py-1.75 text-xs outline-none";

// Upgrade a release to another version of its chart. The editor starts from the
// values the release currently has and *replaces* them on upgrade, so nothing is
// merged invisibly. Reading those values is an audited call (they can hold
// passwords) and happens when this dialog opens.
export function UpgradeModal({
  release,
  onClose,
  onDone,
}: {
  release: HelmRelease;
  onClose: () => void;
  onDone: () => void;
}) {
  const { data: repos } = useCatalogRepos();
  const [repo, setRepo] = useState(release.repo ?? "");
  const [version, setVersion] = useState("");
  const [values, setValues] = useState<string | null>(null);
  const [wait, setWait] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [done, setDone] = useState<string | null>(null);

  const { data: chart, error: chartError } = useChart(
    repo,
    release.chart,
    version || undefined,
  );

  useEffect(() => {
    let live = true;
    getReleaseValues(release.namespace, release.name)
      .then((v) => live && setValues(v))
      .catch((err) => {
        if (live) {
          setError(applyErrorMessage(err));
          setValues("");
        }
      });
    return () => {
      live = false;
    };
  }, [release.namespace, release.name]);

  async function submit() {
    setBusy(true);
    setError(null);
    try {
      const res = await upgradeRelease(release.namespace, release.name, {
        repo,
        chart: release.chart,
        version: version || undefined,
        values: values ?? "",
        wait,
      });
      setDone(
        `Upgraded to ${res.release.chart}-${res.release.chartVersion} (revision ${res.release.revision}).`,
      );
      onDone();
    } catch (err) {
      setError(applyErrorMessage(err));
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 p-4">
      <div className="flex max-h-[92vh] w-full max-w-2xl flex-col rounded-xl border border-border bg-surface p-5">
        <div className="text-sm font-semibold">Upgrade {release.name}</div>
        <div className="mt-1 text-xs text-text-muted">
          Currently {release.chart}-{release.chartVersion} in{" "}
          <span className="font-mono">{release.namespace}</span>.
        </div>

        <div className="mt-4 flex flex-col gap-4 overflow-auto pr-1">
          {!release.repo && (
            <label className="flex flex-col gap-1.5 text-xs text-text-muted">
              Repository
              <select
                aria-label="Repository"
                value={repo}
                onChange={(e) => setRepo(e.target.value)}
                className={inputClass}
              >
                <option value="">Choose where this chart comes from…</option>
                {repos?.repos.map((r) => (
                  <option key={r.name} value={r.name}>
                    {r.name}
                  </option>
                ))}
              </select>
              <span className="text-[11px] text-text-faint">
                This release wasn&apos;t installed through Vinaire, so it
                doesn&apos;t know its source.
              </span>
            </label>
          )}

          <label className="flex flex-col gap-1.5 text-xs text-text-muted">
            Chart version
            <select
              aria-label="Chart version"
              value={version}
              onChange={(e) => setVersion(e.target.value)}
              disabled={!repo}
              className={inputClass}
            >
              <option value="">
                Latest{chart && !version ? ` (${chart.version})` : ""}
              </option>
              {chart?.versions.map((v) => (
                <option key={v.version} value={v.version}>
                  {v.version}
                  {v.version === release.chartVersion ? " (current)" : ""}
                  {v.deprecated ? " — deprecated" : ""}
                </option>
              ))}
            </select>
            {chartError && (
              <span className="text-[11px] text-err">
                {applyErrorMessage(chartError)}
              </span>
            )}
          </label>

          <div className="flex flex-col gap-1.5 text-xs text-text-muted">
            Values (these replace the release&apos;s current values)
            <div className="h-64 overflow-hidden rounded-lg border border-border">
              {values === null ? (
                <div className="p-4 text-text-muted">
                  Loading the release&apos;s current values…
                </div>
              ) : (
                <YamlEditor value={values} onChange={setValues} height="100%" />
              )}
            </div>
          </div>

          <label className="flex items-center gap-2 text-xs text-text-muted">
            <input
              type="checkbox"
              checked={wait}
              onChange={(e) => setWait(e.target.checked)}
            />
            Wait until the resources are ready (up to 5 minutes)
          </label>
        </div>

        {error && (
          <div className="mt-3 whitespace-pre-wrap rounded-lg border border-err/30 bg-surface-2 p-3 text-xs text-err">
            {error}
          </div>
        )}
        {done && (
          <div className="mt-3 rounded-lg border border-ok/30 bg-surface-2 p-3 text-xs text-ok">
            {done}
          </div>
        )}

        <div className="mt-4 flex justify-end gap-2.5">
          <button
            type="button"
            onClick={onClose}
            disabled={busy}
            className="rounded-lg border border-border bg-surface-2 px-3.5 py-2 text-[12.5px] disabled:opacity-60"
          >
            {done ? "Close" : "Cancel"}
          </button>
          {!done && (
            <button
              type="button"
              onClick={() => void submit()}
              disabled={busy || !repo || values === null}
              className="rounded-lg bg-accent px-3.5 py-2 text-[12.5px] font-semibold text-white disabled:opacity-60"
            >
              {busy ? "Upgrading…" : "Upgrade"}
            </button>
          )}
        </div>
      </div>
    </div>
  );
}
