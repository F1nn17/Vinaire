"use client";

import type { HostKey, SshAuth, SshTarget } from "@/lib/api/provision";
import type { ProvisionEvent } from "@/lib/api/provision";

// Pieces shared by the "Add node" dialog (Roadmap Phase 30) and the "Add
// cluster" wizard's SSH modes (Phase 34): the SSH login fields, the host key
// fingerprint confirmation, and the live log. Same rules in both: credentials
// stay in component state only and are wiped once the request is sent.

export type AuthType = "key" | "password";

export type TargetForm = {
  host: string;
  port: string;
  username: string;
  authType: AuthType;
  password: string;
  privateKey: string;
  passphrase: string;
};

export const emptyTarget = (username: string): TargetForm => ({
  host: "",
  port: "22",
  username,
  authType: "key",
  password: "",
  privateKey: "",
  passphrase: "",
});

export const inputClass =
  "w-full rounded-lg border border-border bg-surface-2 px-3 py-1.75 font-mono text-xs outline-none placeholder:text-text-faint";

export function buildAuth(t: TargetForm): SshAuth {
  return t.authType === "key"
    ? { type: "key", privateKey: t.privateKey, passphrase: t.passphrase }
    : { type: "password", password: t.password };
}

export function targetComplete(t: TargetForm): boolean {
  return (
    t.host.trim() !== "" &&
    /^\d+$/.test(t.port) &&
    t.username.trim() !== "" &&
    (t.authType === "key" ? t.privateKey.trim() !== "" : t.password !== "")
  );
}

export function toSshTarget(t: TargetForm, fingerprint: string): SshTarget {
  return {
    host: t.host.trim(),
    port: Number(t.port),
    username: t.username.trim(),
    auth: buildAuth(t),
    hostKeyFingerprint: fingerprint,
  };
}

export function Fingerprint({
  label,
  hostKey,
  host,
  confirmed,
  onConfirm,
}: {
  label: string;
  hostKey: HostKey;
  host: string;
  confirmed: boolean;
  onConfirm: (v: boolean) => void;
}) {
  return (
    <div className="rounded-lg border border-border bg-surface-2 p-3">
      <div className="text-xs font-semibold">
        {label} — <span className="font-mono">{host}</span>
      </div>
      <div className="mt-2 break-all font-mono text-xs">
        {hostKey.fingerprint}
      </div>
      <div className="mt-0.5 text-[11px] text-text-faint">
        {hostKey.keyType}
      </div>
      <label className="mt-2.5 flex items-center gap-2 text-xs">
        <input
          type="checkbox"
          checked={confirmed}
          onChange={(e) => onConfirm(e.target.checked)}
        />
        I&apos;ve checked this fingerprint matches the server
      </label>
    </div>
  );
}

export function TargetFields({
  title,
  value,
  onChange,
  idPrefix,
}: {
  title: string;
  value: TargetForm;
  onChange: (t: TargetForm) => void;
  idPrefix: string;
}) {
  const set = (patch: Partial<TargetForm>) => onChange({ ...value, ...patch });
  return (
    <div className="flex flex-col gap-3">
      <div className="text-xs font-semibold">{title}</div>
      <div className="grid grid-cols-[1fr_90px_1fr] gap-3">
        <label className="flex flex-col gap-1.5 text-xs text-text-muted">
          Host / IP
          <input
            id={`${idPrefix}-host`}
            value={value.host}
            onChange={(e) => set({ host: e.target.value })}
            placeholder="10.0.0.21"
            className={inputClass}
          />
        </label>
        <label className="flex flex-col gap-1.5 text-xs text-text-muted">
          Port
          <input
            id={`${idPrefix}-port`}
            value={value.port}
            onChange={(e) => set({ port: e.target.value })}
            className={inputClass}
          />
        </label>
        <label className="flex flex-col gap-1.5 text-xs text-text-muted">
          Username
          <input
            id={`${idPrefix}-user`}
            value={value.username}
            onChange={(e) => set({ username: e.target.value })}
            className={inputClass}
          />
        </label>
      </div>

      <div className="flex gap-4 text-xs">
        <label className="flex items-center gap-1.5">
          <input
            type="radio"
            checked={value.authType === "key"}
            onChange={() => set({ authType: "key" })}
          />
          SSH key{" "}
          <span className="rounded bg-surface-2 px-1.5 py-0.5 text-[10px] text-ok">
            recommended
          </span>
        </label>
        <label className="flex items-center gap-1.5">
          <input
            type="radio"
            checked={value.authType === "password"}
            onChange={() => set({ authType: "password" })}
          />
          Password
        </label>
      </div>

      {value.authType === "key" ? (
        <>
          <label className="flex flex-col gap-1.5 text-xs text-text-muted">
            Private key
            <textarea
              id={`${idPrefix}-key`}
              rows={4}
              spellCheck={false}
              autoComplete="off"
              value={value.privateKey}
              onChange={(e) => set({ privateKey: e.target.value })}
              placeholder="-----BEGIN OPENSSH PRIVATE KEY-----"
              className={inputClass}
            />
          </label>
          <label className="flex flex-col gap-1.5 text-xs text-text-muted">
            Key passphrase (if it has one)
            <input
              id={`${idPrefix}-passphrase`}
              type="password"
              autoComplete="off"
              value={value.passphrase}
              onChange={(e) => set({ passphrase: e.target.value })}
              className={inputClass}
            />
          </label>
        </>
      ) : (
        <label className="flex flex-col gap-1.5 text-xs text-text-muted">
          Password
          <input
            id={`${idPrefix}-password`}
            type="password"
            autoComplete="off"
            value={value.password}
            onChange={(e) => set({ password: e.target.value })}
            className={inputClass}
          />
          <span className="text-[11px] text-text-faint">
            Prefer an SSH key — a password also unlocks sudo for a non-root
            user.
          </span>
        </label>
      )}
    </div>
  );
}

// LogPane renders a provisioning run's live log.
export function LogPane({
  events,
  running,
  paneRef,
}: {
  events: ProvisionEvent[];
  running: boolean;
  paneRef?: React.RefObject<HTMLDivElement | null>;
}) {
  return (
    <div
      ref={paneRef}
      className="h-72 overflow-auto rounded-lg border border-border bg-code-bg p-3 font-mono text-[11.5px] leading-[1.55]"
    >
      {events.map((e) => (
        <div
          key={e.seq}
          className={
            e.kind === "step"
              ? "mt-2 font-semibold text-accent-text"
              : e.kind === "status" && e.status === "failed"
                ? "text-err"
                : e.kind === "status"
                  ? "text-ok"
                  : "text-text-muted"
          }
        >
          {e.kind === "step" ? `\u25b8 ${e.text}` : e.text}
        </div>
      ))}
      {running && <div className="mt-1 text-text-faint">\u2026</div>}
    </div>
  );
}
