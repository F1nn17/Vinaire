"use client";

import { t } from "@/lib/i18n/translate";
import { useNamespaces } from "@/lib/hooks/useNamespaces";

// The namespace filter of a list: a drop-down of the namespaces that exist, "All namespaces" first
// (value ""). A value that isn't in the list (a link to a namespace the user can't list, or one that
// was just deleted) is kept as an option so the filter never silently changes.
export function NamespaceSelect({
  value,
  onChange,
  className = "",
}: {
  value: string;
  onChange: (namespace: string) => void;
  className?: string;
}) {
  const { data } = useNamespaces();
  const names = (data ?? []).map((n) => n.name);
  const options = value && !names.includes(value) ? [value, ...names] : names;
  return (
    <select
      aria-label="Filter by namespace"
      value={value}
      onChange={(e) => onChange(e.target.value)}
      className={`rounded-lg border border-border bg-surface px-3 py-1.75 font-mono text-xs outline-none ${className}`}
    >
      <option value="">{t("All namespaces")}</option>
      {options.map((n) => (
        <option key={n} value={n}>
          {n}
        </option>
      ))}
    </select>
  );
}
