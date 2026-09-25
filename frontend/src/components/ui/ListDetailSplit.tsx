import type { ReactNode } from "react";

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
    <div className="flex min-h-0 flex-1 gap-6">
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

      <div className="flex min-h-0 flex-1 flex-col gap-3.5">{children}</div>
    </div>
  );
}
