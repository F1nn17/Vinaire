"use client";

import { useMemo } from "react";
import { t } from "@/lib/i18n/translate";
import { applyExternalDb, detectExternalDb } from "@/lib/values/recipe";

// Roadmap Phase 59: for a chart without a recipe that bundles a database the usual way — a
// sub-chart switched by `<db>.enabled` and an `externalDatabase` block of its own — say so, and
// offer to start the change: the bundled database off, the chart's own skeleton in the values.
export function ExternalDbHint({
  defaults,
  values,
  onValues,
}: {
  defaults: string;
  values: string;
  onValues: (next: string) => void;
}) {
  const hint = useMemo(() => detectExternalDb(defaults), [defaults]);
  if (!hint) return null;
  return (
    <div className="flex flex-col gap-2 rounded-lg border border-border bg-surface p-3 text-xs">
      <div className="font-semibold">
        {t("This chart installs its own database ({db})", { db: hint.bundled })}
      </div>
      <div className="text-text-muted">
        {t(
          "To use a database you already run, switch the bundled one off and fill in the chart's externalDatabase settings. The button below puts both into “Your values”; then enter the host, the database and — preferably — the name of an existing Secret instead of a password.",
        )}
      </div>
      <div>
        <button
          type="button"
          onClick={() => onValues(applyExternalDb(values, hint))}
          className="rounded-lg border border-border px-3 py-1.5 hover:bg-surface-2"
        >
          {t("Use my own database: add the settings")}
        </button>
      </div>
    </div>
  );
}
