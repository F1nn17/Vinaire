"use client";

import { NamespaceSelect } from "@/components/ui/NamespaceSelect";
import { Suspense, useEffect, useState } from "react";
import { useSearchParams } from "next/navigation";
import { copyText } from "@/lib/clipboard";
import { useSecrets } from "@/lib/hooks/useSecrets";
import { revealSecretKey } from "@/lib/api/secrets";
import { friendlyErrorMessage } from "@/lib/api/errors";
import { t } from "@/lib/i18n/translate";
import { ListDetailSplit } from "@/components/ui/ListDetailSplit";
import { ListSkeleton } from "@/components/ui/Skeleton";
import { SecretKeyModal } from "@/components/secrets/SecretKeyModal";
import { LoginHint } from "@/components/secrets/LoginHint";
import { DeleteSecretModal } from "@/components/secrets/DeleteSecretModal";
import { CreateResourceModal } from "@/components/editor/CreateResourceModal";
import { secretTemplates } from "@/components/editor/templates";
import { PlusIcon, SecretIcon } from "@/components/icons";
import type { SecretDetail } from "@/lib/types";

// How long a revealed Secret value stays on screen (Roadmap Phase 36).
const REVEAL_TIMEOUT_MS = 30_000;

type RevealState =
  | { status: "loading" }
  | { status: "revealed"; value: string }
  | { status: "error"; message: string };

export default function SecretsPage() {
  return (
    <Suspense
      fallback={<div className="p-8 text-sm text-text-muted">Loading…</div>}
    >
      <SecretsInner />
    </Suspense>
  );
}

// A link may open the page on one Secret (?namespace=app&name=db).
function SecretsInner() {
  const params = useSearchParams();
  const linkedNs = params.get("namespace") ?? "";
  const linkedName = params.get("name") ?? "";
  const [namespace, setNamespace] = useState(linkedNs);
  const [query, setQuery] = useState("");
  const { data: all, isLoading, error, mutate } = useSecrets(namespace);
  const needle = query.trim().toLowerCase();
  const data = all?.filter(
    (s) =>
      !needle ||
      s.name.toLowerCase().includes(needle) ||
      s.namespace.toLowerCase().includes(needle) ||
      s.type.toLowerCase().includes(needle) ||
      s.keys.some((k) => k.label.toLowerCase().includes(needle)),
  );
  const [createOpen, setCreateOpen] = useState(false);
  const [pickedId, setSelectedId] = useState<string | null>(null);
  const selectedId =
    pickedId ??
    (linkedName
      ? (all?.find((s) => s.name === linkedName && s.namespace === linkedNs)
          ?.id ?? null)
      : null);
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

  // A revealed value is plaintext in memory, so it doesn't stay: it is hidden
  // again after REVEAL_TIMEOUT_MS, whenever the tab is hidden, and when leaving
  // the page (the state goes with the component).
  const anyRevealed = Object.values(revealState).some(
    (r) => r.status === "revealed",
  );
  useEffect(() => {
    if (!anyRevealed) return;
    const timer = setTimeout(() => setRevealState({}), REVEAL_TIMEOUT_MS);
    const onHide = () => {
      if (document.hidden) setRevealState({});
    };
    document.addEventListener("visibilitychange", onHide);
    return () => {
      clearTimeout(timer);
      document.removeEventListener("visibilitychange", onHide);
    };
  }, [anyRevealed, revealState]);

  return (
    <div className="flex flex-col gap-[18px] px-8 py-7">
      <div className="flex items-center justify-between">
        <div className="text-[15px] font-semibold">Secrets</div>
        <button
          type="button"
          onClick={() => setCreateOpen(true)}
          className="flex items-center gap-1.5 rounded-lg bg-accent px-3.5 py-2 text-[12.5px] font-semibold text-white"
        >
          <PlusIcon />
          Create
        </button>
      </div>

      <div className="flex flex-wrap items-center gap-3">
        <label className="flex min-w-64 items-center gap-2">
          <span className="sr-only">Search</span>
          <input
            type="search"
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            placeholder={t("Search by name, namespace, type or key")}
            className="w-full rounded-lg border border-border bg-surface px-3 py-1.75 text-xs outline-none placeholder:text-text-faint"
          />
        </label>
        <label className="flex w-fit items-center gap-2">
          <span className="sr-only">Filter by namespace</span>
          <NamespaceSelect value={namespace} onChange={setNamespace} />
        </label>
      </div>

      {error ? (
        <div className="text-sm text-err">
          Failed to load Secrets: {friendlyErrorMessage(error)}
        </div>
      ) : isLoading || !data ? (
        <ListSkeleton />
      ) : data.length === 0 ? (
        <div className="text-sm text-text-muted">
          {needle
            ? t("Nothing matches the search.")
            : `No Secrets found${namespace ? ` in namespace "${namespace}"` : ""}.`}
        </div>
      ) : (
        <SecretsList
          data={data}
          selectedId={selectedId}
          onSelect={selectSecret}
          revealState={revealState}
          setRevealState={setRevealState}
          onDeleted={() => void mutate()}
        />
      )}
      {createOpen && (
        <CreateResourceModal
          templates={secretTemplates}
          onClose={() => setCreateOpen(false)}
          onCreated={() => void mutate()}
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
  onDeleted,
}: {
  data: SecretDetail[];
  onDeleted: () => void;
  selectedId: string | null;
  onSelect: (id: string) => void;
  revealState: Record<string, RevealState>;
  setRevealState: React.Dispatch<
    React.SetStateAction<Record<string, RevealState>>
  >;
}) {
  const selected = data.find((s) => s.id === selectedId) ?? data[0];
  const [deleting, setDeleting] = useState(false);
  const [editingKey, setEditingKey] = useState<string | null>(null);

  const [copied, setCopied] = useState<string | null>(null);
  const [copyError, setCopyError] = useState<string | null>(null);

  // Reveal (audited, like Show) and put the value on the clipboard without drawing it.
  async function copyKey(keyId: string, label: string) {
    setCopyError(null);
    try {
      const value = await revealSecretKey(
        selected.namespace,
        selected.name,
        label,
      );
      if (await copyText(value)) {
        setCopied(keyId);
        setTimeout(() => setCopied((c) => (c === keyId ? null : c)), 2000);
      } else {
        setCopyError(
          t("Copying isn't possible here; use Show and copy by hand."),
        );
      }
    } catch (err) {
      setCopyError(err instanceof Error ? err.message : "Failed to reveal");
    }
  }

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
      <div className="flex shrink-0 items-start justify-between gap-3">
        <div>
          <div className="font-mono text-[15px] font-semibold">
            {selected.name}
          </div>
          <div className="mt-0.5 font-mono text-[11px] text-text-faint">
            ns/{selected.namespace} · {selected.type} · {selected.age}
          </div>
        </div>
        <div className="flex items-center gap-2">
          <button
            type="button"
            onClick={() => setEditingKey("")}
            className="rounded-lg border border-border bg-surface px-3 py-1.5 text-xs text-accent-text"
          >
            {t("Add or change a key")}
          </button>
          <button
            type="button"
            onClick={() => setDeleting(true)}
            className="rounded-lg border border-border bg-surface px-3 py-1.5 text-xs text-err"
          >
            Delete
          </button>
        </div>
      </div>
      {editingKey !== null && (
        <SecretKeyModal
          namespace={selected.namespace}
          name={selected.name}
          existing={selected.keys.map((k) => k.label)}
          initialKey={editingKey}
          onClose={() => setEditingKey(null)}
          onSaved={onDeleted}
        />
      )}
      {deleting && (
        <DeleteSecretModal
          namespace={selected.namespace}
          name={selected.name}
          onClose={() => setDeleting(false)}
          onDeleted={() => {
            setDeleting(false);
            onDeleted();
          }}
        />
      )}
      <div className="flex flex-1 flex-col gap-3 overflow-auto">
        <LoginHint
          key={`${selected.namespace}/${selected.name}`}
          namespace={selected.namespace}
          name={selected.name}
          keys={selected.keys}
          onChanged={onDeleted}
        />
        {copyError && <div className="text-xs text-err">{copyError}</div>}
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
                  <div className="flex items-center gap-2">
                    <button
                      type="button"
                      onClick={() => void copyKey(key.id, key.label)}
                      className="rounded-md border border-border bg-surface-2 px-2 py-[3px] text-[11px] text-accent-text"
                    >
                      {copied === key.id ? t("Copied") : t("Copy")}
                    </button>
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
