"use client";

import { useState } from "react";
import { friendlyErrorMessage } from "@/lib/api/errors";
import type { Instance } from "@/lib/api/instances";
import {
  createBackup,
  deleteBackup,
  useBackupConfig,
  useBackups,
  type BackupItem,
} from "@/lib/api/backups";

const field =
  "rounded-lg border border-border bg-surface-2 px-3 py-1.75 font-mono text-xs outline-none";

// Backups of one instance (Roadmap Phase 77b): a dump now or on a schedule, to a volume claim in the
// namespace or to one of the S3 targets the operator configured, and volume snapshots where the
// cluster supports them. Passwords are never typed: the dump reads the instance's own Secret, and
// S3 credentials come from a Secret you name.
export function BackupsModal({
  instance,
  onClose,
}: {
  instance: Instance;
  onClose: () => void;
}) {
  const { data, mutate } = useBackups(instance.namespace, instance.name);
  const { data: cfg } = useBackupConfig();
  const [kind, setKind] = useState<"dump" | "snapshot">("dump");
  const [target, setTarget] = useState("pvc");
  const [size, setSize] = useState("5Gi");
  const [secret, setSecret] = useState("");
  const [schedule, setSchedule] = useState("");
  const [klass, setKlass] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [note, setNote] = useState<string | null>(null);

  const canSnapshot = cfg?.snapshots.available ?? false;
  const imageOnly = instance.origin === "image";

  async function create() {
    setBusy(true);
    setError(null);
    setNote(null);
    try {
      const res = await createBackup(instance.namespace, instance.name, {
        kind,
        ...(kind === "dump"
          ? {
              target,
              ...(target === "pvc" ? { size } : { credentialsSecret: secret }),
              ...(schedule ? { schedule } : {}),
            }
          : { snapshotClass: klass || undefined }),
      });
      setNote(`Created: ${res.created.join(", ")}`);
      void mutate();
    } catch (e) {
      setError(friendlyErrorMessage(e));
    } finally {
      setBusy(false);
    }
  }

  async function remove(b: BackupItem) {
    setError(null);
    try {
      await deleteBackup(instance.namespace, b.kind, b.name);
      void mutate();
    } catch (e) {
      setError(friendlyErrorMessage(e));
    }
  }

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 p-4">
      <div className="max-h-[90vh] w-full max-w-2xl overflow-auto rounded-xl border border-border bg-surface p-5">
        <div className="text-sm font-semibold">Backups of {instance.name}</div>

        {!imageOnly ? (
          <div className="mt-3 text-xs text-text-muted">
            Backups here are for applications installed from an image.
          </div>
        ) : (
          <div className="mt-3 flex flex-col gap-3 rounded-lg border border-border bg-surface-2 p-3">
            <div className="flex gap-2 text-xs">
              <button
                type="button"
                onClick={() => setKind("dump")}
                className={`rounded-lg border px-2.5 py-1 ${kind === "dump" ? "border-accent text-accent" : "border-border"}`}
              >
                Dump of the data
              </button>
              <button
                type="button"
                disabled={!canSnapshot}
                title={
                  canSnapshot
                    ? ""
                    : "This cluster has no volume snapshot support (CSI snapshots)"
                }
                onClick={() => setKind("snapshot")}
                className={`rounded-lg border px-2.5 py-1 disabled:opacity-40 ${kind === "snapshot" ? "border-accent text-accent" : "border-border"}`}
              >
                Volume snapshot
              </button>
            </div>

            {kind === "dump" ? (
              <>
                <label className="flex flex-col gap-1 text-xs text-text-muted">
                  Where to put it
                  <select
                    value={target}
                    onChange={(e) => setTarget(e.target.value)}
                    className={field}
                  >
                    <option value="pvc">
                      A volume claim in this namespace
                    </option>
                    {(cfg?.s3Targets ?? []).map((t) => (
                      <option key={t.name} value={t.name}>
                        S3: {t.name} ({t.bucket})
                      </option>
                    ))}
                  </select>
                </label>
                {target === "pvc" ? (
                  <label className="flex flex-col gap-1 text-xs text-text-muted">
                    Size of the claim (created if it does not exist)
                    <input
                      value={size}
                      onChange={(e) => setSize(e.target.value)}
                      className={field}
                    />
                  </label>
                ) : (
                  <label className="flex flex-col gap-1 text-xs text-text-muted">
                    Secret with the S3 keys (AWS_ACCESS_KEY_ID and
                    AWS_SECRET_ACCESS_KEY), in this namespace
                    <input
                      value={secret}
                      onChange={(e) => setSecret(e.target.value)}
                      className={field}
                    />
                  </label>
                )}
                <label className="flex flex-col gap-1 text-xs text-text-muted">
                  Schedule (cron, e.g. 0 3 * * *) — empty: once, now
                  <input
                    value={schedule}
                    onChange={(e) => setSchedule(e.target.value)}
                    className={field}
                  />
                </label>
              </>
            ) : (
              <label className="flex flex-col gap-1 text-xs text-text-muted">
                Snapshot class (empty: the cluster default)
                <select
                  value={klass}
                  onChange={(e) => setKlass(e.target.value)}
                  className={field}
                >
                  <option value="">Default</option>
                  {(cfg?.snapshots.classes ?? []).map((c) => (
                    <option key={c} value={c}>
                      {c}
                    </option>
                  ))}
                </select>
              </label>
            )}
            <div>
              <button
                type="button"
                disabled={
                  busy || (kind === "dump" && target !== "pvc" && !secret)
                }
                onClick={() => void create()}
                className="rounded-lg bg-accent px-3.5 py-2 text-[12.5px] font-semibold text-white disabled:opacity-50"
              >
                {busy
                  ? "Creating…"
                  : schedule && kind === "dump"
                    ? "Schedule"
                    : "Back up now"}
              </button>
            </div>
          </div>
        )}

        {note && <div className="mt-3 text-xs text-text-muted">{note}</div>}
        {error && (
          <div className="mt-3 rounded-lg border border-err/30 bg-surface-2 p-3 text-xs text-err">
            {error}
          </div>
        )}

        <div className="mt-4 text-xs font-semibold">Existing backups</div>
        <div className="mt-2 flex flex-col gap-1.5">
          {(data?.backups ?? []).length === 0 && (
            <div className="text-xs text-text-muted">None yet.</div>
          )}
          {(data?.backups ?? []).map((b) => (
            <div
              key={`${b.kind}/${b.name}`}
              className="flex items-baseline justify-between gap-3 text-xs"
            >
              <span className="font-mono">
                {b.kind} {b.name}
                <span className="text-text-muted">
                  {" "}
                  · {b.state}
                  {b.schedule ? ` · ${b.schedule}` : ""}
                  {b.target ? ` · ${b.target}` : ""}
                </span>
              </span>
              <button
                type="button"
                onClick={() => void remove(b)}
                className="text-err"
              >
                Delete
              </button>
            </div>
          ))}
        </div>
        <div className="mt-2 text-xs text-text-muted">
          Deleting a backup here removes the Job, schedule or snapshot — files
          already written to a claim or a bucket stay where they are.
        </div>

        <div className="mt-5 flex justify-end">
          <button
            type="button"
            onClick={onClose}
            className="rounded-lg border border-border bg-surface-2 px-3.5 py-2 text-[12.5px]"
          >
            Close
          </button>
        </div>
      </div>
    </div>
  );
}
