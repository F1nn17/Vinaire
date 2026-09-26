import { SearchIcon } from "@/components/icons";
import { AlertsBell } from "@/components/layout/AlertsBell";
import { BackendStatus } from "@/components/layout/BackendStatus";
import { ClusterPicker } from "@/components/layout/ClusterPicker";
import { UserMenu } from "@/components/layout/UserMenu";

export function Topbar() {
  return (
    <div className="flex h-[60px] shrink-0 items-center gap-4 border-b border-border px-6">
      <ClusterPicker />

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
        <AlertsBell />
        <UserMenu />
      </div>
    </div>
  );
}
