import type { LogLine as LogLineType } from "@/lib/types";

const levelColor: Record<LogLineType["level"], string> = {
  INFO: "var(--ok)",
  WARN: "var(--warn)",
  ERROR: "var(--err)",
};

function escapeRegExp(s: string): string {
  return s.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
}

// The text with every case-insensitive occurrence of `needle` marked. Rendered as
// React text nodes inside <mark>, so log content is never interpreted as HTML.
function Highlighted({ text, needle }: { text: string; needle: string }) {
  if (!needle) return <>{text}</>;
  const parts = text.split(new RegExp(`(${escapeRegExp(needle)})`, "i"));
  return (
    <>
      {parts.map((part, i) =>
        i % 2 === 1 ? (
          <mark key={i} className="rounded-sm bg-warn/30 text-text">
            {part}
          </mark>
        ) : (
          part
        ),
      )}
    </>
  );
}

export function LogLine({
  time,
  level,
  text,
  highlight = "",
  active = false,
  id,
}: LogLineType & { highlight?: string; active?: boolean; id?: string }) {
  return (
    <div
      id={id}
      className={`flex gap-2.5 py-0.5 font-mono text-xs ${
        active ? "rounded bg-accent/15 ring-1 ring-accent/50" : ""
      }`}
    >
      <span className="shrink-0 text-text-faint">{time}</span>
      <span className="w-11 shrink-0" style={{ color: levelColor[level] }}>
        {level}
      </span>
      <span className="text-text">
        <Highlighted text={text} needle={highlight} />
      </span>
    </div>
  );
}
