"use client";

import { useEffect, useRef } from "react";
import Editor, { DiffEditor } from "@monaco-editor/react";
import type { editor } from "monaco-editor";
import "./monacoSetup";

const baseOptions = {
  minimap: { enabled: false },
  fontSize: 12,
  fontFamily: "var(--font-mono), monospace",
  scrollBeyondLastLine: false,
  tabSize: 2,
  automaticLayout: true,
} as const;

export function MonacoYamlEditor({
  value,
  onChange,
  readOnly = false,
  height,
}: {
  value: string;
  onChange?: (value: string) => void;
  readOnly?: boolean;
  height: string;
}) {
  return (
    <Editor
      height={height}
      language="yaml"
      theme="vs-dark"
      value={value}
      onChange={(v) => onChange?.(v ?? "")}
      options={{ ...baseOptions, readOnly }}
    />
  );
}

export function MonacoYamlDiff({
  before,
  after,
  height,
}: {
  before: string;
  after: string;
  height: string;
}) {
  const diffRef = useRef<editor.IStandaloneDiffEditor | null>(null);

  // Detach the models before the wrapper disposes them, or Monaco throws
  // "TextModel got disposed before DiffEditorWidget model got reset".
  useEffect(() => {
    return () => diffRef.current?.setModel(null);
  }, []);

  return (
    <DiffEditor
      onMount={(e) => {
        diffRef.current = e;
      }}
      height={height}
      language="yaml"
      theme="vs-dark"
      original={before}
      modified={after}
      options={{
        ...baseOptions,
        readOnly: true,
        renderSideBySide: false,
        originalEditable: false,
        hideUnchangedRegions: { enabled: true },
      }}
    />
  );
}
