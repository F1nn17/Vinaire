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

type HelpSection = "cluster" | "crds" | "keycloak" | "observability";

const helpSections: { id: HelpSection; label: string }[] = [
  { id: "cluster", label: "Подключение кластера" },
  { id: "crds", label: "Кастомные ресурсы" },
  { id: "keycloak", label: "Вход по логину и паролю" },
  { id: "observability", label: "Метрики, логи, алерты" },
];

const fullClusterRoleYaml = `apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: vinaire-viewer
rules:
  - apiGroups: ["", "apps", "batch", "networking.k8s.io"]
    resources:
      [
        "pods", "deployments", "statefulsets", "daemonsets", "jobs",
        "configmaps", "secrets", "services", "ingresses", "events",
        "replicasets", "serviceaccounts", "persistentvolumeclaims",
        "persistentvolumes", "nodes",
      ]
    verbs: ["get", "list", "watch"]
  - apiGroups: ["metrics.k8s.io"]
    resources: ["pods", "nodes"]
    verbs: ["get", "list"]
  - apiGroups: [""]
    resources: ["pods/log"]
    verbs: ["get"]
  - apiGroups: [""]
    resources: ["pods/exec"]
    verbs: ["create"]
  - apiGroups: ["storage.k8s.io"]
    resources: ["storageclasses"]
    verbs: ["get", "list", "watch"]
  - apiGroups: ["rbac.authorization.k8s.io"]
    resources: ["roles", "rolebindings", "clusterroles", "clusterrolebindings"]
    verbs: ["get", "list", "watch"]
  - apiGroups: ["gateway.networking.k8s.io"]
    resources: ["gateways", "httproutes"]
    verbs: ["get", "list", "watch"]
  - apiGroups: ["apiextensions.k8s.io"]
    resources: ["customresourcedefinitions"]
    verbs: ["get", "list", "watch"]
  # Разрешение на запись — ровно 3 действия с Deployment, ничего больше.
  - apiGroups: ["apps"]
    resources: ["deployments"]
    verbs: ["patch", "delete"]`;

// Cluster management via the web UI: adding a cluster here persists it (a
// Secret in this backend's own namespace — see AGENTS.md's core rule) so
// it survives a pod restart, and registers it live immediately, no
// redeploy needed. Only available when Vinaire itself runs in-cluster —
// the backend reports that explicitly via a 503 if not, surfaced below the
// same way any other "unavailable" state is in this app.
export default function SettingsPage() {
  const [tab, setTab] = useState<SettingsTab>("clusters");
  const [helpSection, setHelpSection] = useState<HelpSection>("cluster");
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
            Every CRD installed on the connected cluster — see the
            &ldquo;Справка&rdquo; tab for what these are and why they show up
            here.
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
        <div className="flex max-w-4xl gap-5">
          <div className="flex w-52 shrink-0 flex-col gap-1">
            {helpSections.map((s) => (
              <button
                key={s.id}
                type="button"
                onClick={() => setHelpSection(s.id)}
                className={`rounded-md px-3 py-2 text-left text-[12.5px] ${
                  helpSection === s.id
                    ? "bg-surface-2 font-semibold text-accent-text"
                    : "text-text-muted"
                }`}
              >
                {s.label}
              </button>
            ))}
          </div>

          <div className="min-w-0 flex-1 rounded-[10px] border border-border bg-surface p-5 text-sm">
            {helpSection === "cluster" && <ClusterHelp />}
            {helpSection === "crds" && <CustomResourcesHelp />}
            {helpSection === "keycloak" && <KeycloakHelp />}
            {helpSection === "observability" && <ObservabilityHelp />}
          </div>
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

function Code({ children }: { children: string }) {
  return (
    <pre className="mt-1.5 overflow-auto rounded-md bg-surface-2 p-2.5 font-mono text-[11px] text-foreground">
      {children}
    </pre>
  );
}

function ClusterHelp() {
  return (
    <div className="flex flex-col gap-5">
      <div>
        <div className="mb-1 text-[13px] font-semibold">
          Как подключить кластер
        </div>
        <div className="text-xs text-text-muted">
          Vinaire не хранит своих собственных прав доступа — она заходит в
          подключаемый кластер под учётной записью (ServiceAccount), которую вы
          создаёте сами, и видит/может делать ровно то, что этой учётной записи
          разрешает RBAC в самом кластере. Ниже — как её создать и подключить,
          шаг за шагом.
        </div>
      </div>

      <div>
        <div className="font-semibold text-foreground">
          Если у вас несколько кластеров (например, два локальных k3d)
        </div>
        <div className="mt-1 text-text-muted">
          Все команды <span className="font-mono">kubectl</span> ниже
          выполняются в том кластере, который сейчас выбран у вас в терминале
          (текущий «context»), а не в том, что открыт в самой Vinaire — это два
          независимых понятия. Посмотрите список доступных кластеров и
          переключитесь на нужный, прежде чем выполнять шаги 1–5:
        </div>
        <Code>
          {`kubectl config get-contexts
kubectl config use-context <имя-нужного-кластера>`}
        </Code>
        <div className="mt-1 text-text-muted">
          Если сомневаетесь, какой context куда ведёт — сравните адрес сервера
          ниже (шаг 4) с тем, что открывается в браузере/консоли провайдера.
        </div>
      </div>

      <div>
        <div className="font-semibold text-foreground">
          1. Namespace для служебной учётной записи
        </div>
        <div className="mt-1 text-text-muted">
          Посмотреть, какие namespace уже есть в кластере:
        </div>
        <Code>{`kubectl get namespaces`}</Code>
        <div className="mt-1 text-text-muted">
          Можно использовать существующий (например,{" "}
          <span className="font-mono">default</span>) или создать отдельный —
          так проще потом найти и удалить всё, что относится к Vinaire, одной
          командой:
        </div>
        <Code>{`kubectl create namespace vinaire-viewer`}</Code>
        <div className="mt-1 text-text-muted">
          Дальше везде подставляйте выбранный namespace вместо{" "}
          <span className="font-mono">&lt;namespace&gt;</span>.
        </div>
      </div>

      <div>
        <div className="font-semibold text-foreground">
          2. Создайте ServiceAccount в этом namespace
        </div>
        <Code>{`kubectl create serviceaccount vinaire-viewer -n <namespace>`}</Code>
      </div>

      <div>
        <div className="font-semibold text-foreground">
          3. Выдайте ей права (RBAC)
        </div>
        <div className="mt-1 text-text-muted">
          Два варианта — выберите один.
        </div>

        <div className="mt-3 rounded-md border border-border p-3">
          <div className="text-[12.5px] font-semibold text-foreground">
            Вариант A (рекомендуется): роль под возможности Vinaire
          </div>
          <div className="mt-1 text-text-muted">
            Даёт ровно то, что умеет Vinaire: просмотр Pods/Deployments/
            ConfigMaps/Secrets/сеть/RBAC/метрики/кастомные ресурсы, плюс три
            действия с Deployment (restart/delete/rollback) — без полного
            cluster-admin. Сохраните в файл и примените:
          </div>
          <Code>{fullClusterRoleYaml}</Code>
          <Code>
            {`kubectl apply -f vinaire-viewer-role.yaml
kubectl create clusterrolebinding vinaire-viewer \\
  --clusterrole=vinaire-viewer --serviceaccount=<namespace>:vinaire-viewer`}
          </Code>
        </div>

        <div className="mt-3 rounded-md border border-border p-3">
          <div className="text-[12.5px] font-semibold text-foreground">
            Вариант B: быстро, только для одноразового локального кластера
          </div>
          <div className="mt-1 text-text-muted">
            Привязать встроенную роль{" "}
            <span className="font-mono">cluster-admin</span> — без составления
            своей роли, но полный доступ ко всему кластеру. Не используйте это
            для чего-то важного или для чужого/боевого кластера:
          </div>
          <Code>
            {`kubectl create clusterrolebinding vinaire-viewer \\
  --clusterrole=cluster-admin --serviceaccount=<namespace>:vinaire-viewer`}
          </Code>
        </div>
      </div>

      <div>
        <div className="font-semibold text-foreground">
          4. Узнайте адрес API-сервера этого кластера
        </div>
        <Code>{`kubectl cluster-info`}</Code>
        <div className="mt-1 text-text-muted">
          Верхняя строка («Kubernetes control plane is running at ...») — это и
          есть адрес для поля «Server URL» ниже.
        </div>
      </div>

      <div>
        <div className="font-semibold text-foreground">
          5. Получите токен ограниченного срока действия
        </div>
        <div className="mt-1 text-text-muted">
          Флаг <span className="font-mono">--duration</span> напрямую задаёт
          срок жизни сессии в Vinaire — не выдавайте бессрочные токены ни для
          чего, кроме одноразового локального теста.
        </div>
        <Code>{`kubectl create token vinaire-viewer -n <namespace> --duration=1h`}</Code>
      </div>

      <div>
        <div className="font-semibold text-foreground">
          6. Заполните форму во вкладке «Clusters»
        </div>
        <ul className="mt-1 list-disc pl-5 text-text-muted">
          <li>
            <span className="font-semibold text-foreground">Name</span> —
            произвольное короткое имя, под которым кластер будет показан в
            переключателе кластеров сверху (например,{" "}
            <span className="font-mono">prod-eu</span>).
          </li>
          <li>
            <span className="font-semibold text-foreground">Server URL</span> —
            адрес из шага 4.
          </li>
          <li>
            <span className="font-semibold text-foreground">
              ServiceAccount token
            </span>{" "}
            — токен из шага 5.
          </li>
          <li>
            <span className="font-semibold text-foreground">
              CA certificate
            </span>{" "}
            — необязательно; без него проверка TLS-сертификата для этого
            кластера будет отключена (подходит только для тестовых окружений, не
            для прод-кластера за реальным сертификатом).
          </li>
        </ul>
      </div>

      <div>
        <div className="font-semibold text-foreground">
          7. Что происходит дальше
        </div>
        <div className="mt-1 text-text-muted">
          Vinaire проверяет подключение реальным вызовом к API-серверу перед
          сохранением — если данные неверны, вы увидите ошибку и ничего не
          сохранится. Кластер сохраняется в Secret в собственном namespace
          бэкенда и переживает перезапуск пода — повторно добавлять его после
          редеплоя Vinaire не нужно. Дальше именно RBAC-роль, которую вы выдали
          на шаге 3, определяет, что можно увидеть и сделать — Vinaire ничего не
          добавляет и не убавляет сверху.
        </div>
      </div>

      <div>
        <div className="font-semibold text-foreground">8. Ещё один кластер</div>
        <div className="mt-1 text-text-muted">
          Повторите шаги «Если у вас несколько кластеров» → 1–7 для каждого
          следующего кластера (не забудьте{" "}
          <span className="font-mono">kubectl config use-context</span> на
          нужный). Подключённые кластеры видны и переключаются через выпадающий
          список в верхней панели Vinaire, рядом с поиском.
        </div>
      </div>
    </div>
  );
}

function CustomResourcesHelp() {
  return (
    <div className="flex flex-col gap-5">
      <div>
        <div className="mb-1 text-[13px] font-semibold">
          Что такое кастомные ресурсы
        </div>
        <div className="text-xs text-text-muted">
          В Kubernetes «из коробки» есть встроенные типы объектов — Pod,
          Deployment, Service и так далее. Custom Resource Definition (CRD) —
          это способ добавить в кластер свой собственный тип объекта. Обычно CRD
          ставятся вместе с каким-то инструментом или оператором — например,
          Traefik, cert-manager, ArgoCD, Helm — и описывают то, чем этот
          инструмент управляет (правило маршрутизации, сертификат, приложение и
          т.д.), тем же способом, что и встроенные объекты: с помощью YAML и{" "}
          <span className="font-mono">kubectl get/describe</span>.
        </div>
      </div>

      <div>
        <div className="font-semibold text-foreground">Зачем это в Vinaire</div>
        <div className="mt-1 text-text-muted">
          Вкладка «Custom Resources» находит все CRD, которые реально
          установлены на подключённом кластере, и позволяет посмотреть их
          объекты в read-only режиме — так же, как вы смотрите Pods или
          Deployments, но для того, что добавил конкретно ваш кластер (например,
          маршруты Traefik или Applications ArgoCD), без необходимости открывать
          kubectl.
        </div>
      </div>

      <div>
        <div className="font-semibold text-foreground">
          Пустой список объектов — это нормально
        </div>
        <div className="mt-1 text-text-muted">
          Если внутри конкретного типа ресурса ничего не показывается — это
          означает, что такой тип установлен (CRD есть), но объектов этого типа
          в кластере сейчас просто нет, а не то, что что-то сломано.
        </div>
      </div>

      <div>
        <div className="font-semibold text-foreground">
          Ошибка доступа при открытии конкретного типа
        </div>
        <div className="mt-1 text-text-muted">
          Список самих CRD виден благодаря правам на{" "}
          <span className="font-mono">customresourcedefinitions</span> в роли из
          раздела «Подключение кластера». Но чтобы посмотреть сами объекты
          внутри конкретного CRD, роли нужно отдельное разрешение на его
          API-группу — иначе кластер ответит ошибкой доступа именно при попытке
          открыть этот тип (сам список CRD при этом виден). Добавьте в
          ClusterRole ещё один блок с нужной группой, например:
        </div>
        <Code>
          {`  - apiGroups: ["cert-manager.io"]
    resources: ["*"]
    verbs: ["get", "list", "watch"]`}
        </Code>
        <div className="mt-1 text-text-muted">
          Группу и версию нужного CRD видно прямо в списке на вкладке «Custom
          Resources» (например,{" "}
          <span className="font-mono">hub.traefik.io/v1alpha1</span>).
        </div>
      </div>
    </div>
  );
}

function KeycloakHelp() {
  return (
    <div className="flex flex-col gap-5">
      <div>
        <div className="mb-1 text-[13px] font-semibold">
          Вход по логину и паролю (Keycloak / OIDC)
        </div>
        <div className="text-xs text-text-muted">
          По умолчанию Vinaire работает в режиме{" "}
          <span className="font-mono">AUTH_MODE=token</span> — на экране входа
          вставляется готовый Kubernetes-токен (см. раздел «Подключение
          кластера»). Чтобы вместо этого был обычный вход по логину и паролю,
          переключите Vinaire в режим{" "}
          <span className="font-mono">AUTH_MODE=oidc</span> — она проведёт
          настоящий вход через Keycloak (или любой другой OIDC-провайдер)
          целиком на своей стороне (в Go-бэкенде), без единой строчки этого в
          браузере.
        </div>
      </div>

      <div className="rounded-md border border-warn/60 bg-surface-2 p-3 text-text-muted">
        <span className="font-semibold text-foreground">Важно понимать: </span>
        вход через Keycloak — это только про то,{" "}
        <span className="italic">как вы попадаете в Vinaire</span>. Токен,
        которым Vinaire потом обращается к самому Kubernetes API — это тот же
        самый OIDC id_token, что вы получили при входе. Значит API-сервер
        подключаемого кластера должен сам доверять этому Keycloak (флаги вида{" "}
        <span className="font-mono">--oidc-issuer-url</span> при запуске
        kube-apiserver, или аналог у managed-провайдера). На обычном локальном
        k3d/k3s из коробки такого доверия нет — вход через Keycloak пройдёт, а
        обращения к кластеру будут падать с 401, пока API-сервер не настроен
        отдельно. Это делает не Vinaire, а администратор кластера — если это не
        ваш случай, оставайтесь на{" "}
        <span className="font-mono">AUTH_MODE=token</span>.
      </div>

      <div>
        <div className="font-semibold text-foreground">
          Быстрый способ: тестовый Keycloak из этого репозитория
        </div>
        <div className="mt-1 text-text-muted">
          В <span className="font-mono">/keycloak</span> уже лежит одноразовый
          Keycloak для локальной проверки самого входа (переживать за реальных
          пользователей тут не нужно — данные не сохраняются) — настройки
          Vinaire по умолчанию уже смотрят именно на него:
        </div>
        <Code>{`cd keycloak
docker compose up -d`}</Code>
        <div className="mt-1 text-text-muted">
          Тестовые пользователи: <span className="font-mono">alice/alice</span>{" "}
          и <span className="font-mono">bob/bob</span>. Подробности — в{" "}
          <span className="font-mono">keycloak/README.md</span> репозитория.
        </div>
      </div>

      <div>
        <div className="font-semibold text-foreground">
          Подключение своего Keycloak
        </div>
        <ol className="mt-1 list-decimal pl-5 text-text-muted">
          <li>
            В Keycloak создайте realm (или используйте существующий) и в нём —
            client с включённым{" "}
            <span className="font-mono">Client authentication</span>{" "}
            (confidential client). Обмен кодом на токен целиком происходит в
            бэкенде Vinaire, поэтому client secret не попадает в браузер.
          </li>
          <li className="mt-1.5">
            Valid redirect URI клиента — ровно адрес бэкенда Vinaire, например:
            <Code>{`https://<адрес-vinaire>/api/v1/auth/oidc/callback`}</Code>
          </li>
          <li className="mt-1.5">Сохраните client id и client secret.</li>
        </ol>
      </div>

      <div>
        <div className="font-semibold text-foreground">
          Переменные окружения на Deployment Vinaire
        </div>
        <Code>
          {`kubectl set env deployment/vinaire -n <namespace> \\
  AUTH_MODE=oidc \\
  OIDC_ISSUER_URL=https://<адрес-keycloak>/realms/<realm> \\
  OIDC_CLIENT_ID=<client id> \\
  OIDC_CLIENT_SECRET=<client secret> \\
  OIDC_REDIRECT_URL=https://<адрес-vinaire>/api/v1/auth/oidc/callback

kubectl rollout restart deployment/vinaire -n <namespace>`}
        </Code>
        <div className="mt-1 text-text-muted">
          После этого экран входа Vinaire ведёт на страницу логина Keycloak
          вместо поля для вставки токена.
        </div>
      </div>
    </div>
  );
}

function ObservabilityHelp() {
  return (
    <div className="flex flex-col gap-5">
      <div>
        <div className="mb-1 text-[13px] font-semibold">
          Метрики, логи и алерты
        </div>
        <div className="text-xs text-text-muted">
          Vinaire ничего из этого не устанавливает сама — она только
          подключается к уже работающему в кластере Prometheus/Loki/
          Alertmanager, если указать их адрес. Каждый из трёх настраивается
          отдельно и независимо от остальных.
        </div>
      </div>

      <div className="rounded-md border border-border bg-surface-2 p-3 text-text-muted">
        <span className="font-semibold text-foreground">Про Grafana: </span>
        Vinaire не подключается к Grafana и не встраивает её панели — такой
        интеграции сейчас нет. Если у вас есть Grafana, открывайте её как
        отдельный инструмент; здесь речь только про Prometheus/Loki/
        Alertmanager.
      </div>

      <div>
        <div className="font-semibold text-foreground">
          Alertmanager — колокольчик с алертами вверху страницы
        </div>
        <div className="mt-1 text-text-muted">
          Единственный из трёх, у которого уже есть готовое место в интерфейсе —
          список текущих алертов в колокольчике в правом верхнем углу.
        </div>
        <Code>
          {`kubectl set env deployment/vinaire -n <namespace> \\
  ALERTMANAGER_URL=http://<адрес-alertmanager>:9093
kubectl rollout restart deployment/vinaire -n <namespace>`}
        </Code>
      </div>

      <div>
        <div className="font-semibold text-foreground">
          Loki — поиск по истории логов в Pod → Logs
        </div>
        <div className="mt-1 text-text-muted">
          Живой хвост логов пода (верхняя часть вкладки Logs) идёт напрямую из
          Kubernetes и Loki для этого не нужен. Loki подключает нижнюю часть той
          же вкладки — поиск по логам за прошлый период. Он рассчитывает, что
          сборщик логов в кластере (Promtail, Grafana Agent и т.п.) подписывает
          строки метками <span className="font-mono">namespace</span> и{" "}
          <span className="font-mono">pod</span> — это самое частое соглашение,
          но не единственное; если у вашего сборщика метки называются иначе,
          поиск просто не найдёт логи конкретного пода.
        </div>
        <Code>
          {`kubectl set env deployment/vinaire -n <namespace> \\
  LOKI_URL=http://<адрес-loki>:3100
kubectl rollout restart deployment/vinaire -n <namespace>`}
        </Code>
      </div>

      <div>
        <div className="font-semibold text-foreground">Prometheus</div>
        <div className="mt-1 text-text-muted">
          На сегодня в интерфейсе Vinaire нет собственного графика, построенного
          на данных Prometheus — CPU/Memory на Overview и в Nodes берутся
          напрямую из metrics-server, без Prometheus вообще. Подключение ниже
          даёт бэкенду прокси-эндпоинт для сырых PromQL- запросов на будущее —
          сейчас он ничего дополнительно не показывает в самом приложении.
        </div>
        <Code>
          {`kubectl set env deployment/vinaire -n <namespace> \\
  PROMETHEUS_URL=http://<адрес-prometheus>:9090
kubectl rollout restart deployment/vinaire -n <namespace>`}
        </Code>
      </div>
    </div>
  );
}
