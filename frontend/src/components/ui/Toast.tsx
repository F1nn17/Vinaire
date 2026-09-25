"use client";

import { useEffect } from "react";

export type ToastState = { type: "success" | "error"; message: string } | null;

export function Toast({
  toast,
  onDismiss,
}: {
  toast: ToastState;
  onDismiss: () => void;
}) {
  useEffect(() => {
    if (!toast) return;
    const timer = setTimeout(onDismiss, 4000);
    return () => clearTimeout(timer);
  }, [toast, onDismiss]);

  if (!toast) return null;

  return (
    <div className="fixed bottom-5 right-5 z-50">
      <div
        className={`flex items-center gap-2.5 rounded-lg border px-4 py-3 text-[12.5px] shadow-lg ${
          toast.type === "success"
            ? "border-ok/30 bg-surface text-ok"
            : "border-err/30 bg-surface text-err"
        }`}
      >
        <span>{toast.message}</span>
        <button
          type="button"
          onClick={onDismiss}
          className="text-text-faint hover:text-text"
          aria-label="Dismiss"
        >
          ×
        </button>
      </div>
    </div>
  );
}
