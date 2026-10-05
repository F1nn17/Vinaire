import type { ReactNode } from "react";
import { FillHeight } from "@/components/ui/FillHeight";

// A list on the left and the selected item on the right. The block takes the rest
// of the window (FillHeight): the list scrolls inside itself and so does the detail
// pane, while the page stays put — no scrollbar for the whole page, no empty band
// below.

export function ListDetailSplit<T>({
  items,
  getId,
  selectedId,
  onSelect,
  renderRow,
  children,
}: {
  items: T[];
  getId: (item: T) => string;
  selectedId: string;
  onSelect: (id: string) => void;
  renderRow: (item: T, isSelected: boolean) => ReactNode;
  children: ReactNode;
}) {
  return (
    <FillHeight className="flex gap-6">
      <div className="w-[420px] shrink-0 overflow-auto rounded-[10px] border border-border bg-surface">
        {items.map((item) => {
          const id = getId(item);
          const isSelected = id === selectedId;
          return (
            <button
              key={id}
              type="button"
              onClick={() => onSelect(id)}
              className={`block w-full border-b border-border px-4 py-3.5 text-left last:border-b-0 ${
                isSelected ? "bg-surface-2" : "bg-transparent"
              }`}
            >
              {renderRow(item, isSelected)}
            </button>
          );
        })}
      </div>

      <div className="flex min-h-0 min-w-0 flex-1 flex-col gap-3.5">
        {children}
      </div>
    </FillHeight>
  );
}
