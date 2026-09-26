"use client";

import { useState } from "react";
import { useSecrets } from "@/lib/hooks/useSecrets";
import { revealSecretKey } from "@/lib/api/secrets";
import { ListDetailSplit } from "@/components/ui/ListDetailSplit";
import { ListSkeleton } from "@/components/ui/Skeleton";
import { PlusIcon, SecretIcon } from "@/components/icons";
import type { SecretDetail } from "@/lib/types";

type RevealState =
  | { status: "loading" }
  | { status: "revealed"; value: string }
  | { status: "error"; message: string };

export default function SecretsPage() {
  const [namespace, setNamespace] = useState("");
  const { data, isLoading, error } = useSecrets(namespace);
  const [selectedId, setSelectedId] = useState<string | null>(null);
  // Revealed values live only in this component's state — never in the SWR
  // cache — so navigating to another secret (or away and back) re-masks
  // them instead of quietly holding decoded plaintext around.
  const [revealState, setRevealState] = useState<Record<string, RevealState>>(
    {},
  );

  function selectSecret(id: string) {
    setSelectedId(id);
    setRevealState({});
  }

  return (
    <div className="flex flex-col gap-[18px] px-8 py-7">
      <div className="flex items-center justify-between">
        <div className="text-[15px] font-semibold">Secrets</div>
        <button
          type="button"
          className="flex items-center gap-1.5 rounded-lg bg-accent px-3.5 py-2 text-[12.5px] font-semibold text-white"
        >
          <PlusIcon />
          Create
        </button>
      </div>

      <label className="flex w-fit items-center gap-2">
        <span className="sr-only">Filter by namespace</span>
        <input
          type="text"
          value={namespace}
          onChange={(e) => setNamespace(e.target.value)}
          placeholder="All namespaces"
          className="rounded-lg border border-border bg-surface px-3 py-1.75 font-mono text-xs outline-none placeholder:text-text-faint"
        />
      </label>

      {error ? (
        <div className="text-sm text-err">
          Failed to load Secrets: {error.message}
        </div>
      ) : isLoading || !data ? (
        <ListSkeleton />
      ) : data.length === 0 ? (
        <div className="text-sm text-text-muted">
          No Secrets found{namespace ? ` in namespace "${namespace}"` : ""}.
        </div>
      ) : (
        <SecretsList
          data={data}
          selectedId={selectedId}
          onSelect={selectSecret}
          revealState={revealState}
          setRevealState={setRevealState}
        />
      )}
    </div>
  );
}

function SecretsList({
  data,
  selectedId,
  onSelect,
  revealState,
  setRevealState,
}: {
  data: SecretDetail[];
  selectedId: string | null;
  onSelect: (id: string) => void;
  revealState: Record<string, RevealState>;
  setRevealState: React.Dispatch<
    React.SetStateAction<Record<string, RevealState>>
  >;
}) {
  const selected = data.find((s) => s.id === selectedId) ?? data[0];

  async function toggleKey(keyId: string, label: string) {
    const current = revealState[keyId];

    if (current?.status === "revealed") {
      setRevealState((prev) => {
        const next = { ...prev };
        delete next[keyId];
        return next;
      });
      return;
    }
    if (current?.status === "loading") return;

    setRevealState((prev) => ({ ...prev, [keyId]: { status: "loading" } }));
    try {
      const value = await revealSecretKey(
        selected.namespace,
        selected.name,
        label,
      );
      setRevealState((prev) => ({
        ...prev,
        [keyId]: { status: "revealed", value },
      }));
    } catch (err) {
      setRevealState((prev) => ({
        ...prev,
        [keyId]: {
          status: "error",
          message: err instanceof Error ? err.message : "Failed to reveal",
        },
      }));
    }
  }

  return (
    <ListDetailSplit
      items={data}
      getId={(s) => s.id}
      selectedId={selected.id}
      onSelect={onSelect}
      renderRow={(s) => (
        <>
          <div className="flex items-center gap-2">
            <SecretIcon size={14} className="shrink-0 text-text-muted" />
            <span className="font-mono text-[13px] font-semibold">
              {s.name}
            </span>
          </div>
          <div className="mt-1.5 flex items-center gap-2 font-mono text-[11px] text-text-faint">
            <span>ns/{s.namespace}</span>
            <span>·</span>
            <span>{s.type}</span>
            <span>·</span>
            <span>{s.age}</span>
          </div>
        </>
      )}
    >
      <div className="shrink-0">
        <div className="font-mono text-[15px] font-semibold">
          {selected.name}
        </div>
        <div className="mt-0.5 font-mono text-[11px] text-text-faint">
          ns/{selected.namespace} · {selected.type} · {selected.age}
        </div>
      </div>
      <div className="flex flex-1 flex-col gap-3 overflow-auto">
        {selected.keys.length === 0 ? (
          <div className="text-xs text-text-muted">No keys.</div>
        ) : (
          selected.keys.map((key) => {
            const state = revealState[key.id];
            return (
              <div
                key={key.id}
                className="rounded-[10px] border border-border bg-surface p-4"
              >
                <div className="mb-2 flex items-center justify-between">
                  <span className="font-mono text-xs text-text-muted">
                    {key.label}
                  </span>
                  <button
                    type="button"
                    onClick={() => toggleKey(key.id, key.label)}
                    disabled={state?.status === "loading"}
                    className="rounded-md border border-border bg-surface-2 px-2 py-[3px] text-[11px] text-accent-text disabled:opacity-60"
                  >
                    {state?.status === "loading"
                      ? "Revealing…"
                      : state?.status === "revealed"
                        ? "Hide"
                        : state?.status === "error"
                          ? "Retry"
                          : "Show"}
                  </button>
                </div>
                {state?.status === "error" ? (
                  <div className="text-xs text-err">{state.message}</div>
                ) : (
                  <div className="break-all font-mono text-xs">
                    {state?.status === "revealed"
                      ? state.value
                      : "••••••••••••"}
                  </div>
                )}
              </div>
            );
          })
        )}
      </div>
    </ListDetailSplit>
  );
}
