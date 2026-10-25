"use client";

import Link from "next/link";
import { useState } from "react";
import { copyText } from "@/lib/clipboard";
import {
  backendLabel,
  describeIngress,
  hostHasTLS,
  routeUrl,
  type IngressBackend,
} from "@/lib/ingress";

// The overview of an Ingress (Roadmap Phase 89): where it answers, what it sends where, whether it is served over TLS —
// and a way to copy each value and to edit. Everything is read from the object's YAML (the caller's own read) and shown as
// text; there is no write path here: "Edit" opens the YAML tab, whose apply is the generic one (Phase 25).
export function IngressOverview({
  yaml,
  namespace,
  onEdit,
}: {
  yaml: string;
  namespace: string;
  onEdit: () => void;
}) {
  const info = describeIngress(yaml);
  if (!info) return null;
  const { routes, tls } = info;
  const hosts = Array.from(new Set(routes.map((r) => r.host).filter(Boolean)));
  const plain = hosts.filter((h) => !hostHasTLS(h, tls));

  return (
    <div className="flex flex-col gap-4">
      <div className="flex max-w-4xl flex-col gap-3 rounded-[10px] border border-border bg-surface p-5">
        <div className="flex items-center justify-between">
          <div className="text-[13px] font-semibold">Ingress</div>
          <button
            type="button"
            onClick={onEdit}
            className="rounded-md border border-border px-3 py-1.5 text-xs hover:bg-surface-2"
          >
            Edit (YAML)
          </button>
        </div>
        <div className="flex flex-col gap-2.5">
          <Field
            label="Class"
            value={info.className || "—"}
            copy={info.className}
          />
          <Field
            label="Address"
            value={
              info.addresses.length
                ? info.addresses.join(", ")
                : "not assigned yet"
            }
            copy={info.addresses.join(", ")}
          />
          {info.defaultBackend && (
            <div className="flex items-center justify-between gap-3">
              <span className="text-xs text-text-muted">Default backend</span>
              <Backend b={info.defaultBackend} namespace={namespace} />
            </div>
          )}
        </div>
      </div>

      <div className="max-w-4xl overflow-hidden rounded-[10px] border border-border bg-surface">
        <div className="border-b border-border px-5 py-3 text-[13px] font-semibold">
          Routes
        </div>
        {routes.length === 0 ? (
          <div className="px-5 py-4 text-xs text-text-muted">
            This Ingress has no rules.
          </div>
        ) : (
          <div className="overflow-x-auto">
            <table className="w-full text-left text-xs">
              <thead className="text-text-faint">
                <tr>
                  <th className="px-5 py-2 font-medium">Host</th>
                  <th className="px-3 py-2 font-medium">Path</th>
                  <th className="px-3 py-2 font-medium">Backend</th>
                  <th className="px-3 py-2 font-medium">TLS</th>
                </tr>
              </thead>
              <tbody>
                {routes.map((r, i) => {
                  const url = routeUrl(r, tls);
                  const secure = !!r.host && hostHasTLS(r.host, tls);
                  return (
                    <tr key={i} className="border-t border-border align-top">
                      <td className="px-5 py-2 font-mono">
                        {r.host || "(any host)"}
                        {r.host && <CopyButton text={r.host} label="host" />}
                        {url && (
                          <a
                            href={url}
                            target="_blank"
                            rel="noopener noreferrer"
                            className="ml-2 text-accent-text underline"
                          >
                            Open
                          </a>
                        )}
                      </td>
                      <td className="px-3 py-2 font-mono">
                        {r.path}
                        {r.pathType && (
                          <span className="ml-1.5 text-text-faint">
                            {r.pathType}
                          </span>
                        )}
                      </td>
                      <td className="px-3 py-2">
                        <Backend b={r.backend} namespace={namespace} />
                      </td>
                      <td className="px-3 py-2">
                        <span className={secure ? "text-ok" : "text-warn"}>
                          {secure ? "HTTPS" : "none"}
                        </span>
                      </td>
                    </tr>
                  );
                })}
              </tbody>
            </table>
          </div>
        )}
      </div>

      <div className="max-w-4xl rounded-[10px] border border-border bg-surface p-5">
        <div className="mb-3 text-[13px] font-semibold">TLS</div>
        {tls.length === 0 ? (
          <div className="text-xs text-text-muted">
            No TLS: this Ingress is served over plain http. To serve it over
            HTTPS, add a <span className="font-mono">spec.tls</span> block with
            the host and the name of a{" "}
            <span className="font-mono">kubernetes.io/tls</span> Secret in the
            YAML tab.
          </div>
        ) : (
          <div className="flex flex-col gap-2">
            {tls.map((t, i) => (
              <div
                key={i}
                className="flex flex-wrap items-center gap-2 text-xs"
              >
                <span className="font-mono">
                  {t.hosts.length ? t.hosts.join(", ") : "(all hosts)"}
                </span>
                <span className="text-text-faint">→</span>
                {t.secretName ? (
                  <Link
                    href={`/resources/detail?r=${encodeURIComponent("core/v1/secrets")}&ns=${encodeURIComponent(namespace)}&name=${encodeURIComponent(t.secretName)}`}
                    className="font-mono text-accent-text underline"
                  >
                    {t.secretName}
                  </Link>
                ) : (
                  <span className="text-text-faint">
                    the controller&apos;s own certificate
                  </span>
                )}
              </div>
            ))}
            {plain.length > 0 && (
              <div className="mt-1 text-xs text-warn">
                Not covered by TLS: {plain.join(", ")}
              </div>
            )}
          </div>
        )}
      </div>

      {Object.keys(info.annotations).length > 0 && (
        <div className="max-w-4xl rounded-[10px] border border-border bg-surface p-5">
          <div className="mb-3 text-[13px] font-semibold">Annotations</div>
          <div className="flex flex-col gap-1.5">
            {Object.entries(info.annotations).map(([k, v]) => (
              <div
                key={k}
                className="flex flex-wrap gap-x-2 font-mono text-[11px]"
              >
                <span className="text-text-muted">{k}</span>
                <span className="break-all">{v}</span>
              </div>
            ))}
          </div>
        </div>
      )}
    </div>
  );
}

function Field({
  label,
  value,
  copy,
}: {
  label: string;
  value: string;
  copy?: string;
}) {
  return (
    <div className="flex items-center justify-between gap-3">
      <span className="text-xs text-text-muted">{label}</span>
      <span className="font-mono text-xs">
        {value}
        {copy ? <CopyButton text={copy} label={label} /> : null}
      </span>
    </div>
  );
}

// A Service backend links to the Service's page in Routing; any other backend is shown as text.
function Backend({ b, namespace }: { b: IngressBackend; namespace: string }) {
  if (b.service) {
    return (
      <span className="font-mono">
        <Link
          href={`/network/detail?r=${encodeURIComponent("core/v1/services")}&ns=${encodeURIComponent(namespace)}&name=${encodeURIComponent(b.service.name)}`}
          className="text-accent-text underline"
        >
          {b.service.name}
        </Link>
        {b.service.port && <span>:{b.service.port}</span>}
        <CopyButton text={backendLabel(b)} label="backend" />
      </span>
    );
  }
  return <span className="font-mono">{backendLabel(b) || "—"}</span>;
}

function CopyButton({ text, label }: { text: string; label: string }) {
  const [done, setDone] = useState(false);
  return (
    <button
      type="button"
      aria-label={`Copy ${label}`}
      onClick={async () => {
        if (await copyText(text)) {
          setDone(true);
          setTimeout(() => setDone(false), 1500);
        }
      }}
      className="ml-1.5 rounded border border-border px-1.5 py-0.5 text-[10px] text-text-muted hover:bg-surface-2"
    >
      {done ? "Copied" : "Copy"}
    </button>
  );
}
