"use client";

import { useId, useState } from "react";

type Line = { text: string; color?: string };

export function TerminalPane({
  connectedTo,
  container,
  seedLines,
}: {
  connectedTo: string;
  container: string;
  seedLines: Line[];
}) {
  const [lines, setLines] = useState(seedLines);
  const [value, setValue] = useState("");
  const inputId = useId();

  function runCommand(command: string) {
    if (!command.trim()) return;
    setLines((prev) => [
      ...prev,
      { text: `$ ${command}`, color: "var(--ok)" },
      {
        text: "vinaire: read-only preview — exec into the pod isn't wired up to the backend yet.",
      },
    ]);
    setValue("");
  }

  return (
    <div className="flex flex-col gap-2 rounded-[10px] border border-border bg-code-bg p-4">
      <div className="font-mono text-xs text-text-faint">
        Connected to {connectedTo} · container: {container}
      </div>
      {lines.map((line, i) => (
        <div
          key={i}
          className="font-mono text-xs"
          style={{ color: line.color ?? "var(--text-muted)" }}
        >
          {line.text}
        </div>
      ))}
      <label htmlFor={inputId} className="sr-only">
        Terminal command
      </label>
      <div className="mt-2 flex items-center gap-2">
        <span aria-hidden="true" className="font-mono text-xs text-ok">
          $
        </span>
        <input
          id={inputId}
          type="text"
          value={value}
          onChange={(e) => setValue(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === "Enter") runCommand(value);
          }}
          placeholder="Type a command…"
          className="flex-1 border-none bg-transparent font-mono text-xs outline-none"
        />
      </div>
    </div>
  );
}
