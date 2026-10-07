"use client";

import Link from "next/link";
import {
  isValidElement,
  useMemo,
  useSyncExternalStore,
  type ReactNode,
} from "react";
import { TableSkeleton } from "@/components/ui/Skeleton";

export type DataTableColumn<T> = {
  header: string;
  width: string;
  render: (row: T) => ReactNode;
  // What to sort and filter by, when the rendered cell isn't plain enough (the text
  // of the rendered cell is used otherwise).
  sortValue?: (row: T) => string | number;
};

// Every list in Vinaire is this component, so they all get the same things without
// each page doing anything: click a header to sort (again to reverse), a filter box
// that matches any column, a Columns menu to hide what you don't need — and the
// view (sort, filter, hidden columns) lives in the page's URL, so a reload or a link
// shows the same table. (Not in web storage: the app forbids it, and a URL is
// shareable.)

// ---- helpers ------------------------------------------------------------------

// The visible text of a rendered cell, for sorting and filtering.
export function nodeText(n: ReactNode): string {
  if (n === null || n === undefined || typeof n === "boolean") return "";
  if (typeof n === "string" || typeof n === "number") return String(n);
  if (Array.isArray(n)) return n.map(nodeText).join(" ");
  if (isValidElement(n)) {
    return nodeText((n.props as { children?: ReactNode }).children);
  }
  return "";
}

const unitSeconds: Record<string, number> = {
  s: 1,
  m: 60,
  h: 3600,
  d: 86400,
  w: 604800,
  y: 31536000,
};

// "3d4h" -> seconds; null when the text isn't an age.
export function ageSeconds(text: string): number | null {
  const t = text.trim();
  if (!/^(\d+[smhdwy])+$/.test(t)) return null;
  let total = 0;
  for (const m of t.matchAll(/(\d+)([smhdwy])/g)) {
    total += Number(m[1]) * unitSeconds[m[2]];
  }
  return total;
}

const collator = new Intl.Collator(undefined, {
  numeric: true,
  sensitivity: "base",
});

export function compareCells(a: string | number, b: string | number): number {
  if (typeof a === "number" && typeof b === "number") return a - b;
  const sa = String(a);
  const sb = String(b);
  const aa = ageSeconds(sa);
  const ab = ageSeconds(sb);
  if (aa !== null && ab !== null) return aa - ab;
  // empty and "—" always sort last
  const ea = sa === "" || sa === "—";
  const eb = sb === "" || sb === "—";
  if (ea !== eb) return ea ? 1 : -1;
  return collator.compare(sa, sb);
}

function tableId(headers: string[]): string {
  let h = 5381;
  for (const c of headers.join("|")) h = ((h << 5) + h + c.charCodeAt(0)) >>> 0;
  return h.toString(36).slice(0, 5);
}

type Sort = { header: string; dir: "asc" | "desc" } | null;

function readView(search: string, id: string, headers: string[]) {
  const p = new URLSearchParams(search);
  let sort: Sort = null;
  const s = p.get(`s_${id}`);
  if (s) {
    const i = s.lastIndexOf(":");
    const header = s.slice(0, i);
    const dir = s.slice(i + 1);
    if (headers.includes(header) && (dir === "asc" || dir === "desc")) {
      sort = { header, dir };
    }
  }
  const hidden = new Set(
    (p.get(`h_${id}`) ?? "")
      .split("|")
      .filter((h) => headers.includes(h) && h !== headers[0]),
  );
  return { sort, query: p.get(`q_${id}`) ?? "", hidden };
}

function writeView(id: string, sort: Sort, query: string, hidden: Set<string>) {
  const p = new URLSearchParams(window.location.search);
  const set = (k: string, v: string) =>
    v ? p.set(`${k}_${id}`, v) : p.delete(`${k}_${id}`);
  set("s", sort ? `${sort.header}:${sort.dir}` : "");
  set("q", query);
  set("h", [...hidden].join("|"));
  const qs = p.toString();
  window.history.replaceState(
    window.history.state,
    "",
    `${window.location.pathname}${qs ? `?${qs}` : ""}${window.location.hash}`,
  );
  window.dispatchEvent(new Event(VIEW_EVENT));
}

// The URL is the single source of truth for a table's view: components read it
// through useSyncExternalStore (the server snapshot is empty, so the prerendered
// HTML and the first client render agree) and write it with replaceState.
const VIEW_EVENT = "vinaire:table-view";

function subscribe(onChange: () => void) {
  window.addEventListener("popstate", onChange);
  window.addEventListener(VIEW_EVENT, onChange);
  return () => {
    window.removeEventListener("popstate", onChange);
    window.removeEventListener(VIEW_EVENT, onChange);
  };
}
const getSearch = () => window.location.search;
const getServerSearch = () => "";

// ---- component ----------------------------------------------------------------

export function DataTable<T>({
  columns,
  rows,
  rowKey,
  rowHref,
  emptyLabel = "Nothing here yet.",
  isLoading = false,
  toolbar = true,
}: {
  columns: DataTableColumn<T>[];
  rows: T[];
  rowKey: (row: T) => string;
  rowHref?: (row: T) => string | undefined;
  emptyLabel?: string;
  // Roadmap Phase 17: a real skeleton instead of every page rendering its
  // own bare "Loading…" text before this component. Callers still branch
  // on their `error` state themselves — this only covers the loading/empty/
  // loaded states.
  isLoading?: boolean;
  // Filter box and Columns menu; sorting by header is always on.
  toolbar?: boolean;
}) {
  // Callers often build `columns` inline, so identify the table by its headers (a
  // string), not by the array — that stays the same across renders.
  const SEP = "";
  const headersKey = columns.map((c) => c.header).join(SEP);
  const id = tableId(headersKey.split(SEP));
  const search = useSyncExternalStore(subscribe, getSearch, getServerSearch);
  const { sort, query, hidden } = useMemo(
    () => readView(search, id, headersKey.split(SEP)),
    [search, id, headersKey],
  );

  const persist = (s: Sort, q: string, h: Set<string>) =>
    writeView(id, s, q, h);

  function onSort(header: string) {
    const next: Sort =
      sort?.header !== header
        ? { header, dir: "asc" }
        : sort.dir === "asc"
          ? { header, dir: "desc" }
          : null; // third click: back to the server's order
    persist(next, query, hidden);
  }

  function onQuery(q: string) {
    persist(sort, q, hidden);
  }

  function toggleColumn(header: string) {
    const next = new Set(hidden);
    if (next.has(header)) next.delete(header);
    else next.add(header);
    // sorting by a column you just hid would be invisible
    persist(sort && next.has(sort.header) ? null : sort, query, next);
  }

  const visible = columns.filter((c) => !hidden.has(c.header));
  const gridTemplateColumns = visible.map((c) => c.width).join(" ");

  const shown = useMemo(() => {
    const q = query.trim().toLowerCase();
    let out = rows;
    if (q) {
      out = out.filter((row) =>
        columns.some((c) => {
          const v = c.sortValue
            ? String(c.sortValue(row))
            : nodeText(c.render(row));
          return v.toLowerCase().includes(q);
        }),
      );
    }
    if (sort) {
      const col = columns.find((c) => c.header === sort.header);
      if (col) {
        const val = (row: T) =>
          col.sortValue ? col.sortValue(row) : nodeText(col.render(row)).trim();
        const sign = sort.dir === "asc" ? 1 : -1;
        out = [...out].sort((a, b) => sign * compareCells(val(a), val(b)));
      }
    }
    return out;
  }, [rows, columns, query, sort]);

  if (isLoading) {
    return <TableSkeleton columns={columns.length} />;
  }

  const showToolbar = toolbar && rows.length > 0;

  return (
    <div className="flex flex-1 flex-col gap-2">
      {showToolbar && (
        <div className="flex items-center gap-3">
          <input
            type="text"
            aria-label="Filter table"
            value={query}
            onChange={(e) => onQuery(e.target.value)}
            placeholder="Filter rows"
            className="w-64 rounded-lg border border-border bg-surface px-3 py-1.5 text-xs outline-none placeholder:text-text-faint"
          />
          <span className="text-xs text-text-faint">
            {query.trim()
              ? `${shown.length} of ${rows.length}`
              : `${rows.length}`}
          </span>
          <details className="relative ml-auto">
            <summary className="cursor-pointer list-none rounded-lg border border-border bg-surface px-3 py-1.5 text-xs text-text-muted hover:border-accent">
              Columns
              {hidden.size > 0
                ? ` (${columns.length - hidden.size}/${columns.length})`
                : ""}
            </summary>
            <div className="absolute right-0 z-20 mt-1 min-w-44 rounded-lg border border-border bg-surface-2 p-2 shadow-lg">
              {columns.map((c, i) => (
                <label
                  key={c.header}
                  className={`flex items-center gap-2 px-2 py-1 text-xs ${
                    i === 0 ? "text-text-faint" : "text-text-muted"
                  }`}
                >
                  <input
                    type="checkbox"
                    checked={!hidden.has(c.header)}
                    // the first column names the row — it stays
                    disabled={i === 0}
                    onChange={() => toggleColumn(c.header)}
                  />
                  {c.header}
                </label>
              ))}
            </div>
          </details>
        </div>
      )}

      <div className="flex-1 overflow-auto rounded-[10px] border border-border bg-surface">
        <div
          className="sticky top-0 grid gap-0 border-b border-border bg-surface px-[18px] py-2.5"
          style={{ gridTemplateColumns }}
          role="row"
        >
          {visible.map((col) => {
            const active = sort?.header === col.header;
            return (
              <span
                key={col.header}
                role="columnheader"
                aria-sort={
                  active
                    ? sort.dir === "asc"
                      ? "ascending"
                      : "descending"
                    : "none"
                }
              >
                <button
                  type="button"
                  onClick={() => onSort(col.header)}
                  title="Click to sort"
                  className={`flex items-center gap-1 text-[11px] uppercase tracking-wide hover:text-text ${
                    active ? "text-accent-text" : "text-text-muted"
                  }`}
                >
                  {col.header}
                  <span aria-hidden className="w-2 text-[9px]">
                    {active ? (sort.dir === "asc" ? "▲" : "▼") : ""}
                  </span>
                </button>
              </span>
            );
          })}
        </div>

        {shown.length === 0 ? (
          <div className="px-[18px] py-6 text-[13px] text-text-faint">
            {rows.length > 0 ? "No rows match the filter." : emptyLabel}
          </div>
        ) : (
          shown.map((row) => {
            const href = rowHref?.(row);
            const rowClassName =
              "grid items-center gap-0 border-b border-border px-[18px] py-3 last:border-b-0";
            const cells = visible.map((col) => (
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
    </div>
  );
}
