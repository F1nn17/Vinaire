// Roadmap Phase 17: a shared loading skeleton, used everywhere a list was
// previously showing bare "Loading…" text — a real signal of "content is
// coming, here's roughly its shape" rather than a blank/text-only pause.

export function TableSkeleton({
  columns,
  rows = 6,
}: {
  columns: number;
  rows?: number;
}) {
  return (
    <div className="flex-1 overflow-hidden rounded-[10px] border border-border bg-surface">
      <div className="border-b border-border px-[18px] py-2.5">
        <div className="h-3 w-20 animate-pulse rounded bg-surface-2" />
      </div>
      {Array.from({ length: rows }).map((_, i) => (
        <div
          key={i}
          className="flex items-center gap-4 border-b border-border px-[18px] py-3.5 last:border-b-0"
        >
          {Array.from({ length: columns }).map((_, j) => (
            <div
              key={j}
              className="h-3 flex-1 animate-pulse rounded bg-surface-2"
              style={{ animationDelay: `${(i * columns + j) * 30}ms` }}
            />
          ))}
        </div>
      ))}
    </div>
  );
}

// For ListDetailSplit-style pages (ConfigMaps, Secrets) — a stack of
// placeholder rows matching that layout's row shape, instead of DataTable's
// grid.
export function ListSkeleton({ rows = 6 }: { rows?: number }) {
  return (
    <div className="flex flex-1 flex-col overflow-hidden rounded-[10px] border border-border bg-surface">
      {Array.from({ length: rows }).map((_, i) => (
        <div
          key={i}
          className="flex flex-col gap-2 border-b border-border px-4 py-3.5 last:border-b-0"
        >
          <div
            className="h-3 w-1/2 animate-pulse rounded bg-surface-2"
            style={{ animationDelay: `${i * 40}ms` }}
          />
          <div
            className="h-2.5 w-1/3 animate-pulse rounded bg-surface-2"
            style={{ animationDelay: `${i * 40 + 20}ms` }}
          />
        </div>
      ))}
    </div>
  );
}
