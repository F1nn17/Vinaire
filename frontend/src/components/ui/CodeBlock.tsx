export function CodeBlock({ code }: { code: string }) {
  return (
    <div className="overflow-auto rounded-[10px] border border-border bg-code-bg p-[18px]">
      <pre className="m-0 font-mono text-xs leading-[1.6] text-text">
        {code}
      </pre>
    </div>
  );
}
