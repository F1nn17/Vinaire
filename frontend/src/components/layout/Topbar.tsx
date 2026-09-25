import { BellIcon, ChevronDownIcon, SearchIcon } from "@/components/icons";
import { BackendStatus } from "@/components/layout/BackendStatus";

export function Topbar() {
  return (
    <div className="flex h-[60px] shrink-0 items-center gap-4 border-b border-border px-6">
      <button
        type="button"
        className="flex items-center gap-2 rounded-lg border border-border bg-surface px-3 py-2"
      >
        <span className="font-mono text-[13px] font-semibold">prod-msk-01</span>
        <ChevronDownIcon />
      </button>

      <label htmlFor="global-search" className="sr-only">
        Search resources
      </label>
      <div className="flex max-w-[480px] flex-1 items-center gap-2.5 rounded-lg border border-border bg-surface px-3.5 py-[9px]">
        <SearchIcon className="text-text-muted" />
        <input
          id="global-search"
          type="search"
          placeholder="Search resources…"
          className="flex-1 border-none bg-transparent font-mono text-[13px] outline-none"
        />
        <span className="rounded-[5px] border border-border px-1.5 py-0.5 font-mono text-[11px] text-text-faint">
          ⌘K
        </span>
      </div>

      <div className="ml-auto flex items-center gap-4">
        <BackendStatus />
        <button
          type="button"
          aria-label="Alerts"
          className="relative text-text-muted"
        >
          <BellIcon />
          <span
            aria-hidden="true"
            className="absolute -right-0.5 -top-0.5 h-2 w-2 rounded-full border-2 border-bg bg-err"
          />
        </button>
        <div
          aria-hidden="true"
          className="h-[30px] w-[30px] rounded-full border border-border bg-surface-2"
        />
      </div>
    </div>
  );
}
