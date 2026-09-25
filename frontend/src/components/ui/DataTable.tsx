import Link from "next/link";
import type { ReactNode } from "react";

export type DataTableColumn<T> = {
  header: string;
  width: string;
  render: (row: T) => ReactNode;
};

export function DataTable<T>({
  columns,
  rows,
  rowKey,
  rowHref,
  emptyLabel = "Nothing here yet.",
}: {
  columns: DataTableColumn<T>[];
  rows: T[];
  rowKey: (row: T) => string;
  rowHref?: (row: T) => string | undefined;
  emptyLabel?: string;
}) {
  const gridTemplateColumns = columns.map((c) => c.width).join(" ");

  return (
    <div className="flex-1 overflow-auto rounded-[10px] border border-border bg-surface">
      <div
        className="sticky top-0 grid gap-0 border-b border-border bg-surface px-[18px] py-2.5"
        style={{ gridTemplateColumns }}
      >
        {columns.map((col) => (
          <span
            key={col.header}
            className="text-[11px] uppercase tracking-wide text-text-muted"
          >
            {col.header}
          </span>
        ))}
      </div>

      {rows.length === 0 ? (
        <div className="px-[18px] py-6 text-[13px] text-text-faint">
          {emptyLabel}
        </div>
      ) : (
        rows.map((row) => {
          const href = rowHref?.(row);
          const rowClassName =
            "grid items-center gap-0 border-b border-border px-[18px] py-3 last:border-b-0";
          const cells = columns.map((col) => (
            <div key={col.header}>{col.render(row)}</div>
          ));

          return href ? (
            <Link
              key={rowKey(row)}
              href={href}
              className={`${rowClassName} hover:bg-surface-2`}
              style={{ gridTemplateColumns }}
            >
              {cells}
            </Link>
          ) : (
            <div
              key={rowKey(row)}
              className={rowClassName}
              style={{ gridTemplateColumns }}
            >
              {cells}
            </div>
          );
        })
      )}
    </div>
  );
}
