import type { ReactNode } from "react";

// Roadmap Phase 33: a compact icon toolbar for a detail page's actions
// (Scale, Restart, Pause/Resume, Rollback, Delete, ...), modeled on Headlamp's
// dense action bar. Each button shows what it does on hover and on keyboard
// focus: its name in bold and, when given, a one-line `hint` saying what will
// happen — an icon alone doesn't tell you that "restart" replaces pods one by
// one. The tooltip is CSS-only (no JS state), and the native `title` is
// deliberately not used so the two never stack.
export function ActionButton({
  icon,
  label,
  hint,
  onClick,
  danger,
  disabled,
}: {
  icon: ReactNode;
  label: string;
  // one short sentence under the name in the tooltip
  hint?: string;
  onClick: () => void;
  danger?: boolean;
  disabled?: boolean;
}) {
  return (
    <span className="group relative">
      <button
        type="button"
        aria-label={label}
        onClick={onClick}
        disabled={disabled}
        className={`flex h-8 w-8 items-center justify-center rounded-lg border bg-surface transition-colors focus-visible:outline-none focus-visible:ring-2 disabled:cursor-not-allowed disabled:opacity-40 ${
          danger
            ? "border-err/40 text-err hover:border-err hover:bg-err/15 focus-visible:ring-err/60"
            : "border-border text-text-muted hover:border-accent hover:bg-accent/15 hover:text-accent-text focus-visible:ring-accent/60"
        }`}
      >
        {icon}
      </button>
      <span
        role="tooltip"
        className="pointer-events-none absolute right-0 top-full z-20 mt-1.5 w-max max-w-56 rounded-lg border border-border bg-surface-2 px-2.5 py-1.5 text-left opacity-0 shadow-lg transition-opacity group-focus-within:opacity-100 group-hover:opacity-100"
      >
        <span
          className={`block text-xs font-semibold ${danger ? "text-err" : "text-text"}`}
        >
          {label}
        </span>
        {hint && (
          <span className="mt-0.5 block text-[11px] text-text-muted">
            {hint}
          </span>
        )}
      </span>
    </span>
  );
}

export function ActionBar({ children }: { children: ReactNode }) {
  return <div className="flex shrink-0 items-center gap-1.5">{children}</div>;
}
