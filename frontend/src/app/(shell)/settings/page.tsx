"use client";

import Link from "next/link";
import { useState, type FormEvent } from "react";
import { useClusters } from "@/lib/hooks/useClusters";
import { useCRDs } from "@/lib/hooks/useCRDs";
import { addCluster, removeCluster } from "@/lib/api/clusters";
import { friendlyErrorMessage } from "@/lib/api/errors";
import { ConfirmModal } from "@/components/ui/ConfirmModal";
import { Toast, type ToastState } from "@/components/ui/Toast";
import { PuzzleIcon } from "@/components/icons";
import type { Cluster } from "@/lib/types";

type SettingsTab = "clusters" | "crds" | "help";

const tabs: { id: SettingsTab; label: string }[] = [
  { id: "clusters", label: "Clusters" },
  { id: "crds", label: "Custom Resources" },
  { id: "help", label: "Справка" },
];

// Cluster management via the web UI: adding a cluster here persists it (a
// Secret in this backend's own namespace — see AGENTS.md's core rule) so
// it survives a pod restart, and registers it live immediately, no
// redeploy needed. Only available when Vinaire itself runs in-cluster —
// the backend reports that explicitly via a 503 if not, surfaced below the
// same way any other "unavailable" state is in this app.
export default function SettingsPage() {
  const [tab, setTab] = useState<SettingsTab>("clusters");
  const { data: clusters, error, isLoading, mutate } = useClusters();
  const { data: crds, error: crdsError, isLoading: crdsLoading } = useCRDs();

  const [name, setName] = useState("");
  const [server, setServer] = useState("");
  const [token, setToken] = useState("");
  const [caData, setCaData] = useState("");
  const [busy, setBusy] = useState(false);
  const [formError, setFormError] = useState<string | null>(null);
  const [toast, setToast] = useState<ToastState>(null);
  const [pendingRemove, setPendingRemove] = useState<Cluster | null>(null);
  const [removing, setRemoving] = useState(false);

  async function handleAdd(e: FormEvent) {
    e.preventDefault();
    if (!name.trim() || !server.trim() || !token.trim()) return;

    setBusy(true);
    setFormError(null);
    try {
      await addCluster({
        name: name.trim(),
        server: server.trim(),
        token: token.trim(),
        caData: caData.trim() || undefined,
      });
      setToast({ type: "success", message: `Cluster "${name.trim()}" added.` });
      setName("");
      setServer("");
      setToken("");
      setCaData("");
      await mutate();
    } catch (err) {
      setFormError(friendlyErrorMessage(err));
    } finally {
      setBusy(false);
    }
  }

  async function handleRemove() {
    if (!pendingRemove) return;
    setRemoving(true);
    try {
      await removeCluster(pendingRemove.name);
      setToast({
        type: "success",
        message: `Cluster "${pendingRemove.name}" removed.`,
      });
      setPendingRemove(null);
      await mutate();
    } catch (err) {
      setToast({ type: "error", message: friendlyErrorMessage(err) });
    } finally {
      setRemoving(false);
    }
  }

  return (
    <div className="flex flex-col gap-[18px] px-8 py-7">
      <div className="text-[15px] font-semibold">Settings</div>

      <div className="flex gap-1 border-b border-border">
        {tabs.map((t) => (
          <button
            key={t.id}
            type="button"
            onClick={() => setTab(t.id)}
            className={`px-3.5 py-2 text-[12.5px] ${
              tab === t.id
                ? "border-b-2 border-accent font-semibold text-accent-text"
                : "text-text-muted"
            }`}
          >
            {t.label}
          </button>
        ))}
      </div>

      {tab === "clusters" && (
        <div className="max-w-2xl rounded-[10px] border border-border bg-surface p-5">
          <div className="mb-1 text-[13px] font-semibold">Clusters</div>
          <div className="mb-4 text-xs text-text-muted">
            Clusters this Vinaire instance is connected to. Clusters you add
            here persist across restarts.
          </div>

          {error ? (
            <div className="text-sm text-err">
              Failed to load clusters: {friendlyErrorMessage(error)}
            </div>
          ) : isLoading || !clusters ? (
            <div className="text-sm text-text-muted">Loading…</div>
          ) : (
            <div className="mb-5 flex flex-col gap-2">
              {clusters.map((c) => (
                <div
                  key={c.name}
                  className="flex items-center justify-between rounded-md border border-border bg-surface-2 px-3 py-2"
                >
                  <span className="flex items-center gap-2 font-mono text-[12.5px]">
                    {c.name}
                    {c.default && (
                      <span className="rounded-full border border-border px-1.5 py-[1px] text-[10px] text-text-muted">
                        default
                      </span>
                    )}
                  </span>
                  {c.removable && (
                    <button
                      type="button"
                      onClick={() => setPendingRemove(c)}
                      className="rounded-md border border-err px-2.5 py-1 text-[11px] text-err"
                    >
                      Remove
                    </button>
                  )}
                </div>
              ))}
            </div>
          )}

          <form
            onSubmit={handleAdd}
            className="flex flex-col gap-3 border-t border-border pt-4"
          >
            <div className="text-xs font-semibold text-text-muted">
              Add a cluster
            </div>

            <div className="grid grid-cols-2 gap-3">
              <label className="flex flex-col gap-1">
                <span className="text-xs text-text-muted">Name</span>
                <input
                  value={name}
                  onChange={(e) => setName(e.target.value)}
                  placeholder="e.g. prod-eu"
                  className="rounded-lg border border-border bg-surface px-3 py-1.75 font-mono text-xs outline-none placeholder:text-text-faint"
                />
              </label>
              <label className="flex flex-col gap-1">
                <span className="text-xs text-text-muted">Server URL</span>
                <input
                  value={server}
                  onChange={(e) => setServer(e.target.value)}
                  placeholder="https://1.2.3.4:6443"
                  className="rounded-lg border border-border bg-surface px-3 py-1.75 font-mono text-xs outline-none placeholder:text-text-faint"
                />
              </label>
            </div>

            <label className="flex flex-col gap-1">
              <span className="text-xs text-text-muted">
                ServiceAccount token
              </span>
              <textarea
                value={token}
                onChange={(e) => setToken(e.target.value)}
                rows={3}
                placeholder="kubectl create token <serviceaccount> -n <namespace> --duration=..."
                className="resize-none rounded-lg border border-border bg-surface px-3 py-2 font-mono text-xs outline-none placeholder:text-text-faint"
              />
            </label>

            <label className="flex flex-col gap-1">
              <span className="text-xs text-text-muted">
                CA certificate (optional, PEM)
              </span>
              <textarea
                value={caData}
                onChange={(e) => setCaData(e.target.value)}
                rows={3}
                placeholder="-----BEGIN CERTIFICATE-----"
                className="resize-none rounded-lg border border-border bg-surface px-3 py-2 font-mono text-xs outline-none placeholder:text-text-faint"
              />
              {!caData.trim() && (
                <span className="text-[11px] text-warn">
                  No CA certificate — TLS certificate verification will be
                  skipped for this cluster.
                </span>
              )}
            </label>

            {formError && <div className="text-xs text-err">{formError}</div>}

            <button
              type="submit"
              disabled={busy}
              className="w-fit rounded-lg bg-accent px-3.5 py-2 text-[12.5px] font-semibold text-white disabled:opacity-60"
            >
              {busy ? "Connecting…" : "Add cluster"}
            </button>
          </form>
        </div>
      )}

      {tab === "crds" && (
        <div className="max-w-2xl rounded-[10px] border border-border bg-surface p-5">
          <div className="mb-1 text-[13px] font-semibold">Custom Resources</div>
          <div className="mb-4 text-xs text-text-muted">
            Every CRD installed on the connected cluster (Roadmap Phase 15).
          </div>

          {crdsError ? (
            <div className="text-sm text-err">
              Failed to load Custom Resources: {friendlyErrorMessage(crdsError)}
            </div>
          ) : crdsLoading || !crds ? (
            <div className="text-sm text-text-muted">Loading…</div>
          ) : crds.length === 0 ? (
            <div className="text-sm text-text-muted">
              No CRDs installed on this cluster.
            </div>
          ) : (
            <div className="flex flex-col gap-1.5">
              {crds.map((crd) => (
                <Link
                  key={crd.name}
                  href={`/custom-resources?crd=${encodeURIComponent(crd.name)}`}
                  className="flex items-center justify-between rounded-md border border-border bg-surface-2 px-3 py-2"
                >
                  <span className="flex items-center gap-2 text-[12.5px]">
                    <PuzzleIcon />
                    {crd.kind}
                  </span>
                  <span className="font-mono text-[11px] text-text-faint">
                    {crd.group}/{crd.version}
                  </span>
                </Link>
              ))}
            </div>
          )}
        </div>
      )}

      {tab === "help" && (
        <div className="max-w-2xl rounded-[10px] border border-border bg-surface p-5 text-sm">
          <div className="mb-1 text-[13px] font-semibold">
            Как подключить кластер
          </div>
          <div className="mb-4 text-xs text-text-muted">
            Пошаговая инструкция по добавлению кластера через вкладку «Clusters»
            выше.
          </div>

          <ol className="flex flex-col gap-4 text-[13px] text-text-muted">
            <li>
              <div className="font-semibold text-foreground">
                1. Создайте ServiceAccount в целевом кластере
              </div>
              <pre className="mt-1.5 overflow-auto rounded-md bg-surface-2 p-2.5 font-mono text-[11px] text-foreground">
                {`kubectl create serviceaccount vinaire-viewer -n <namespace>`}
              </pre>
            </li>
            <li>
              <div className="font-semibold text-foreground">
                2. Выдайте роль (не cluster-admin)
              </div>
              <div className="mt-1">
                Пример минимальной ClusterRole для Vinaire есть в файле{" "}
                <code className="font-mono text-[11.5px]">AGENTS.md</code>{" "}
                репозитория (раздел «Example: a minimal ClusterRole for
                Vinaire»). Привяжите её через{" "}
                <code className="font-mono text-[11.5px]">
                  ClusterRoleBinding
                </code>{" "}
                или <code className="font-mono text-[11.5px]">RoleBinding</code>
                , в зависимости от того, сколько кластера этой учётной записи
                можно видеть.
              </div>
              <pre className="mt-1.5 overflow-auto rounded-md bg-surface-2 p-2.5 font-mono text-[11px] text-foreground">
                {`kubectl create clusterrolebinding vinaire-viewer \\
  --clusterrole=vinaire-viewer --serviceaccount=<namespace>:vinaire-viewer`}
              </pre>
            </li>
            <li>
              <div className="font-semibold text-foreground">
                3. Получите токен ограниченного срока действия
              </div>
              <div className="mt-1">
                Флаг <code className="font-mono text-[11.5px]">--duration</code>{" "}
                напрямую задаёт срок жизни сессии — не выдавайте бессрочные
                токены ни для чего, кроме одноразового локального теста.
              </div>
              <pre className="mt-1.5 overflow-auto rounded-md bg-surface-2 p-2.5 font-mono text-[11px] text-foreground">
                {`kubectl create token vinaire-viewer -n <namespace> --duration=1h`}
              </pre>
            </li>
            <li>
              <div className="font-semibold text-foreground">
                4. Заполните форму во вкладке «Clusters»
              </div>
              <ul className="mt-1 list-disc pl-5">
                <li>
                  <span className="font-semibold text-foreground">Name</span> —
                  произвольное короткое имя, под которым кластер будет показан в
                  переключателе кластеров (например,{" "}
                  <span className="font-mono">prod-eu</span>).
                </li>
                <li>
                  <span className="font-semibold text-foreground">
                    Server URL
                  </span>{" "}
                  — адрес API-сервера целевого кластера (
                  <span className="font-mono">https://host:6443</span>).
                </li>
                <li>
                  <span className="font-semibold text-foreground">
                    ServiceAccount token
                  </span>{" "}
                  — токен из шага 3.
                </li>
                <li>
                  <span className="font-semibold text-foreground">
                    CA certificate
                  </span>{" "}
                  — необязательно; без него проверка TLS-сертификата для этого
                  кластера будет отключена (подходит только для тестовых
                  окружений).
                </li>
              </ul>
            </li>
            <li>
              <div className="font-semibold text-foreground">
                5. Что происходит дальше
              </div>
              <div className="mt-1">
                Vinaire проверяет подключение реальным вызовом к API-серверу
                перед сохранением. Кластер сохраняется в Secret в собственном
                namespace бэкенда и переживает перезапуск пода — повторно
                добавлять его после редеплоя не нужно. Реальные права доступа
                (RBAC) в подключённом кластере определяют, что можно увидеть и
                сделать — Vinaire не добавляет собственных ограничений сверху.
              </div>
            </li>
          </ol>
        </div>
      )}

      <ConfirmModal
        open={pendingRemove !== null}
        title="Remove cluster?"
        message={`This only removes "${pendingRemove?.name}" from Vinaire — it doesn't touch the cluster itself.`}
        confirmLabel="Remove"
        danger
        busy={removing}
        onConfirm={handleRemove}
        onCancel={() => setPendingRemove(null)}
      />
      <Toast toast={toast} onDismiss={() => setToast(null)} />
    </div>
  );
}
