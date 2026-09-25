export type TabItem<K extends string> = {
  key: K;
  label: string;
};

export function PillTabs<K extends string>({
  items,
  active,
  onChange,
}: {
  items: TabItem<K>[];
  active: K;
  onChange: (key: K) => void;
}) {
  return (
    <div className="flex w-fit gap-1 rounded-[9px] border border-border bg-surface p-1">
      {items.map((item) => {
        const isActive = item.key === active;
        return (
          <button
            key={item.key}
            type="button"
            onClick={() => onChange(item.key)}
            className={`rounded-md px-3.5 py-1.5 text-[12.5px] ${
              isActive ? "bg-surface-2 text-accent-text" : "text-text-muted"
            }`}
          >
            {item.label}
          </button>
        );
      })}
    </div>
  );
}

export function UnderlineTabs<K extends string>({
  items,
  active,
  onChange,
}: {
  items: TabItem<K>[];
  active: K;
  onChange: (key: K) => void;
}) {
  return (
    <div className="flex gap-[22px] border-b border-border">
      {items.map((item) => {
        const isActive = item.key === active;
        return (
          <button
            key={item.key}
            type="button"
            onClick={() => onChange(item.key)}
            className={`border-b-2 px-0.5 py-2.5 text-[13px] ${
              isActive
                ? "border-accent text-accent-text"
                : "border-transparent text-text-muted"
            }`}
          >
            {item.label}
          </button>
        );
      })}
    </div>
  );
}
