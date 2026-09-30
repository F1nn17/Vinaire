import type { ReactNode } from "react";

// Roadmap Phase 33: a compact icon toolbar for a detail page's actions
// (Scale, Restart, Pause/Resume, Rollback, Delete, ...) — replacing the
// stretched text buttons the mockups originally had, modeled on
// Headlamp's dense action bar. Every button carries a plain-text tooltip
// via the native `title` attribute rather than a bespoke Tooltip
// component — one label on hover doesn't need more than that.
export function ActionButton({
  icon,
  label,
  onClick,
  danger,
  disabled,
}: {
  icon: ReactNode;
  label: string;
  onClick: () => void;
  danger?: boolean;
  disabled?: boolean;
}) {
  return (
    <button
      type="button"
      title={label}
      aria-label={label}
      onClick={onClick}
      disabled={disabled}
      className={`flex h-8 w-8 items-center justify-center rounded-lg border border-border transition-colors disabled:cursor-not-allowed disabled:opacity-40 ${
        danger
          ? "text-err hover:border-err hover:bg-err/10"
          : "text-text-muted hover:border-accent hover:text-foreground"
      }`}
    >
      {icon}
    </button>
  );
}

export function ActionBar({ children }: { children: ReactNode }) {
  return <div className="flex shrink-0 items-center gap-1.5">{children}</div>;
}
