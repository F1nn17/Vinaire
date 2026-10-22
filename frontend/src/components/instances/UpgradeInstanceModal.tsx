"use client";

import { useEffect, useState } from "react";
import { friendlyErrorMessage } from "@/lib/api/errors";
import { apiFetch } from "@/lib/api/http";
import { getImageTags, type ImageTag } from "@/lib/api/imageTags";
import { createBackup, type BackupItem } from "@/lib/api/backups";
import { upgradeInstance, type Instance } from "@/lib/api/instances";

const field =
  "rounded-lg border border-border bg-surface-2 px-3 py-1.75 font-mono text-xs outline-none";

// Engines whose data can be dumped before the change (internal/backup).
const dumpable = ["postgres", "mysql", "mariadb", "redis", "valkey"];

function split(image: string): { repo: string; tag: string } {
  const i = image.lastIndexOf(":");
  return { repo: image.slice(0, i), tag: image.slice(i + 1) };
}

function major(tag: string): number | null {
  const m = /^v?(\d+)/.exec(tag);
  return m ? Number(m[1]) : null;
}

// Changing the version of an application installed from an image (Roadmap Phase 77d). Only the tag
// changes — the repository stays that of the running image. A dump can be taken first and the change
// waits for it; a change of major version has to be accepted explicitly, because a database cannot
// read the data of another major version.
export function UpgradeInstanceModal({
  instance,
  onClose,
  onDone,
}: {
  instance: Instance;
  onClose: () => void;
  onDone: () => void;
}) {
  const image = instance.image ?? "";
  const { repo, tag: current } = split(image);
  const [tags, setTags] = useState<ImageTag[]>([]);
  const [tag, setTag] = useState("");
  const [accept, setAccept] = useState(false);
  const [dumpFirst, setDumpFirst] = useState(
    dumpable.includes(instance.app ?? ""),
  );
  const [typed, setTyped] = useState("");
  const [busy, setBusy] = useState(false);
  const [status, setStatus] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    getImageTags(repo)
      .then((r) => setTags(r.tags.slice(0, 40)))
      .catch(() => setTags([]));
  }, [repo]);

  const curMajor = major(current);
  const newMajor = major(tag);
  const needsAccept =
    !!tag && (curMajor === null || newMajor === null || curMajor !== newMajor);
  const canDump = dumpable.includes(instance.app ?? "");

  async function waitForDump(job: string) {
    for (let i = 0; i < 100; i++) {
      await new Promise((r) => setTimeout(r, 3000));
      const res = await apiFetch<{ backups: BackupItem[] }>(
        `/api/v1/instances/${encodeURIComponent(instance.namespace)}/${encodeURIComponent(instance.name)}/backups`,
      );
      const b = res.backups.find((x) => x.name === job);
      if (b?.state === "succeeded") return;
      if (b?.state === "failed")
        throw new Error("The dump failed; nothing was changed.");
    }
    throw new Error("The dump did not finish in time; nothing was changed.");
  }

  async function run(revert: boolean) {
    setBusy(true);
    setError(null);
    try {
      if (!revert && dumpFirst && canDump) {
        setStatus("Taking a dump first…");
        const res = await createBackup(instance.namespace, instance.name, {
          kind: "dump",
          target: "pvc",
        });
        await waitForDump(res.created[0]);
      }
      setStatus("Changing the version…");
      await upgradeInstance(instance.namespace, instance.name, {
        tag,
        acceptMajor: accept,
        revert,
      });
      onDone();
    } catch (e) {
      setError(friendlyErrorMessage(e));
      setStatus(null);
      setBusy(false);
    }
  }

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 p-4">
      <div className="max-h-[90vh] w-full max-w-lg overflow-auto rounded-xl border border-border bg-surface p-5">
        <div className="text-sm font-semibold">
          Change the version of {instance.name}
        </div>
        <div className="mt-2 text-xs text-text-muted">
          Now: <span className="font-mono text-text">{image}</span>. Only the
          tag changes; the pod restarts with the new version.
        </div>

        <label className="mt-4 flex flex-col gap-1 text-xs text-text-muted">
          New tag
          <input
            list="instance-tags"
            value={tag}
            onChange={(e) => setTag(e.target.value.trim())}
            className={field}
            placeholder={current}
          />
          <datalist id="instance-tags">
            {tags.map((t) => (
              <option key={t.name} value={t.name} />
            ))}
          </datalist>
        </label>

        {needsAccept && (
          <label className="mt-3 flex items-start gap-2 rounded-lg border border-border bg-surface-2 p-3 text-xs">
            <input
              type="checkbox"
              checked={accept}
              onChange={(e) => setAccept(e.target.checked)}
              className="mt-0.5"
            />
            <span>
              The major version changes (or cannot be compared). A database
              usually cannot read the data of another major version — it may not
              start, and going back may not work. I have a backup and accept
              this.
            </span>
          </label>
        )}

        {canDump && (
          <label className="mt-3 flex items-center gap-2 text-xs">
            <input
              type="checkbox"
              checked={dumpFirst}
              onChange={(e) => setDumpFirst(e.target.checked)}
            />
            Take a dump into the backup volume first (recommended)
          </label>
        )}

        <label className="mt-4 flex flex-col gap-1.5 text-xs text-text-muted">
          Type <span className="font-mono text-text">{instance.name}</span> to
          confirm
          <input
            value={typed}
            onChange={(e) => setTyped(e.target.value)}
            className={field}
          />
        </label>

        {status && <div className="mt-3 text-xs text-text-muted">{status}</div>}
        {error && (
          <div className="mt-3 rounded-lg border border-err/30 bg-surface-2 p-3 text-xs text-err">
            {error}
          </div>
        )}

        <div className="mt-5 flex items-center justify-between gap-2.5">
          <div>
            {instance.previousImage && (
              <button
                type="button"
                disabled={busy || typed !== instance.name}
                onClick={() => void run(true)}
                title={instance.previousImage}
                className="rounded-lg border border-border bg-surface-2 px-3 py-2 text-[12.5px] disabled:opacity-50"
              >
                Go back to the previous version
              </button>
            )}
          </div>
          <div className="flex gap-2.5">
            <button
              type="button"
              onClick={onClose}
              disabled={busy}
              className="rounded-lg border border-border bg-surface-2 px-3.5 py-2 text-[12.5px] disabled:opacity-60"
            >
              Cancel
            </button>
            <button
              type="button"
              disabled={
                busy ||
                !tag ||
                typed !== instance.name ||
                (needsAccept && !accept)
              }
              onClick={() => void run(false)}
              className="rounded-lg bg-accent px-3.5 py-2 text-[12.5px] font-semibold text-white disabled:opacity-50"
            >
              {busy ? "Working…" : "Change version"}
            </button>
          </div>
        </div>
      </div>
    </div>
  );
}
