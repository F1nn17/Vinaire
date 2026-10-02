"use client";

import Link from "next/link";
import { useState } from "react";
import { useCRDs } from "@/lib/hooks/useCRDs";
import { friendlyErrorMessage } from "@/lib/api/errors";
import { PuzzleIcon } from "@/components/icons";

type SettingsTab = "crds" | "help";

const tabs: { id: SettingsTab; label: string }[] = [
  { id: "crds", label: "Custom Resources" },
  { id: "help", label: "Справка" },
];

type HelpSection =
  | "cluster"
  | "node"
  | "marketplace"
  | "gitops"
  | "crds"
  | "keycloak"
  | "observability";

const helpSections: { id: HelpSection; label: string }[] = [
  { id: "cluster", label: "Подключение кластера" },
  { id: "node", label: "Добавление ноды" },
  { id: "marketplace", label: "Marketplace (Helm)" },
  { id: "gitops", label: "GitOps" },
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
  const [tab, setTab] = useState<SettingsTab>("help");
  const [helpSection, setHelpSection] = useState<HelpSection>("cluster");
  const { data: crds, error: crdsError, isLoading: crdsLoading } = useCRDs();

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
            {helpSection === "node" && <NodeHelp />}
            {helpSection === "marketplace" && <MarketplaceHelp />}
            {helpSection === "gitops" && <GitOpsHelp />}
            {helpSection === "crds" && <CustomResourcesHelp />}
            {helpSection === "keycloak" && <KeycloakHelp />}
            {helpSection === "observability" && <ObservabilityHelp />}
          </div>
        </div>
      )}
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
          Все способы добавления кластера собраны в одном месте — на странице{" "}
          <Link href="/clusters" className="text-accent-text">
            Clusters
          </Link>{" "}
          (пункт «Clusters» в боковом меню). Нажмите «Add cluster» — или «+ Add
          cluster» в выпадающем списке кластеров в верхней панели — и выберите
          способ. Другого места, где можно добавить кластер, в Vinaire нет.
        </div>
      </div>

      <div>
        <div className="font-semibold text-foreground">
          Какой способ выбрать
        </div>
        <ul className="mt-1 list-disc pl-5 text-text-muted">
          <li>
            <span className="font-semibold text-foreground">
              Found on this machine
            </span>{" "}
            — второй кластер стоит на той же машине, где запущена Vinaire
            (домашний сервер, ноутбук: k3s, k3d, kind, minikube). Vinaire сама
            находит его по kubeconfig-файлам, вам остаётся подтвердить.
          </li>
          <li>
            <span className="font-semibold text-foreground">
              Import existing (SSH)
            </span>{" "}
            — кластер k3s уже работает на другом сервере: Vinaire заходит туда
            по SSH, забирает kubeconfig и подключается.
          </li>
          <li>
            <span className="font-semibold text-foreground">
              Create new (SSH)
            </span>{" "}
            — есть чистый сервер, на нём нужен новый кластер: Vinaire сама
            ставит k3s.
          </li>
          <li>
            <span className="font-semibold text-foreground">
              Server URL + token
            </span>{" "}
            — любой кластер, к которому у вас есть адрес API и токен
            ServiceAccount (самое узкое и безопасное: токен можно ограничить
            ровно нужными правами). Пошаговая инструкция ниже.
          </li>
          <li>
            <span className="font-semibold text-foreground">
              Import via kubeconfig
            </span>{" "}
            — любой дистрибутив, включая облачные: вставляете или загружаете
            kubeconfig.
          </li>
        </ul>
      </div>

      <div>
        <div className="font-semibold text-foreground">
          Found on this machine — когда это работает
        </div>
        <div className="mt-1 text-text-muted">
          Vinaire должна быть запущена прямо на машине (бинарник или Docker на
          хосте) и ей нужно место, где хранить зашифрованный список кластеров —
          переменная <span className="font-mono">VINAIRE_DATA_DIR</span>. Она
          ищет в <span className="font-mono">$KUBECONFIG</span>,{" "}
          <span className="font-mono">~/.kube/config</span>,{" "}
          <span className="font-mono">/etc/rancher/k3s/k3s.yaml</span> и в путях
          из <span className="font-mono">DISCOVERY_KUBECONFIG_PATHS</span>. k3d
          и kind находятся через их записи в kubeconfig — доступ к Docker-сокету
          Vinaire не нужен и не используется. Кластер, который Vinaire уже
          использует, в списке помечен «already added».
        </div>
        <Code>
          {`# запуск Vinaire прямо на машине
cd backend
VINAIRE_DATA_DIR=$HOME/.vinaire AUTH_MODE=disabled go run .`}
        </Code>
        <div className="mt-1 text-text-muted">
          Файл <span className="font-mono">k3s.yaml</span> по умолчанию читает
          только root, а Vinaire сама никогда не повышает права и не вызывает
          sudo. Если он показан как «not readable» — скопируйте его туда, где
          Vinaire может его прочитать, и нажмите Rescan:
        </div>
        <Code>
          {`sudo cp /etc/rancher/k3s/k3s.yaml ~/.kube/k3s.yaml
sudo chown $USER ~/.kube/k3s.yaml
# и добавьте путь в DISCOVERY_KUBECONFIG_PATHS (или в KUBECONFIG)`}
        </Code>
        <div className="mt-1 text-text-muted">
          <span className="font-semibold text-foreground">
            Если Vinaire запущена внутри кластера (как под)
          </span>{" "}
          — этот способ недоступен: под не видит файлы вашего компьютера, и
          мастер сам об этом скажет. Используйте «Import via kubeconfig»: на
          вашей машине выполните
        </div>
        <Code>
          {`kubectl config get-contexts
kubectl config view --raw --minify --flatten --context <имя-контекста>`}
        </Code>
        <div className="mt-1 text-text-muted">
          и вставьте вывод. Адрес сервера в нём должен быть доступен из пода:
          внутри пода <span className="font-mono">127.0.0.1</span> — это сам
          под, поэтому используйте адрес машины в сети или, для k3d на Docker
          Desktop, <span className="font-mono">host.docker.internal</span>.
        </div>
      </div>

      <div>
        <div className="font-semibold text-foreground">
          Import / Create по SSH — что нужно
        </div>
        <ul className="mt-1 list-disc pl-5 text-text-muted">
          <li>
            Доступ по SSH (лучше по ключу) к серверу. Перед входом Vinaire
            покажет отпечаток SSH-ключа сервера — сверьте его командой{" "}
            <span className="font-mono">
              ssh-keygen -lf /etc/ssh/ssh_host_*_key.pub
            </span>{" "}
            на самом сервере и подтвердите. Если ключ по пути изменится,
            подключение прервётся до отправки пароля.
          </li>
          <li>
            Пароль и ключ нужны только на время операции: Vinaire держит их в
            памяти, нигде не сохраняет и не пишет в логи. Работает только по
            HTTPS (для локальной отладки есть{" "}
            <span className="font-mono">PROVISION_ALLOW_INSECURE=true</span>).
          </li>
          <li>
            У вас должно быть право создавать узлы в текущем кластере (RBAC{" "}
            <span className="font-mono">create</span> на{" "}
            <span className="font-mono">nodes</span>).
          </li>
          <li>
            <span className="font-semibold text-foreground">
              Адрес кластера
            </span>{" "}
            — тот адрес, по которому Vinaire (и вы) будете к нему обращаться.
            При создании k3s выпускает сертификат только на этот адрес (
            <span className="font-mono">--tls-san</span>), поэтому подключение
            по другому адресу даст ошибку TLS. Если нужен порт или имя, отличные
            от <span className="font-mono">адрес:6443</span> — заполните «API
            URL».
          </li>
          <li>
            В «Distribution» можно выбрать{" "}
            <span className="font-semibold text-foreground">
              Kubernetes (kubeadm)
            </span>{" "}
            вместо k3s — обычный «настоящий» Kubernetes на Debian/Ubuntu. Для
            Create потребуется сервер минимум с 2 CPU и 2 ГБ RAM, с доступом к{" "}
            <span className="font-mono">pkgs.k8s.io</span> и{" "}
            <span className="font-mono">registry.k8s.io</span>. Vinaire сама
            отключит swap, включит нужные модули ядра и sysctl, поставит
            containerd и закреплённые пакеты kubelet/kubeadm/kubectl, выполнит{" "}
            <span className="font-mono">kubeadm init</span> и поставит сеть
            подов (Flannel или Calico — версии закреплены в конфигурации
            бэкенда, а не «latest»). Опции: версия Kubernetes, CNI, «single
            node» (запускать нагрузку и на control-plane) и{" "}
            <span className="font-semibold text-foreground">
              control-plane endpoint
            </span>{" "}
            — задайте его сразу, если когда-нибудь может понадобиться HA:
            добавить его потом практически невозможно. Список версий и CNI лежит
            в <span className="font-mono">kubeadm.json</span> бэкенда (или в
            файле из <span className="font-mono">KUBEADM_CONFIG</span>).
          </li>
          <li>
            Кластер kubeadm создаётся пустым — в нём нет Ingress-контроллера,
            StorageClass и metrics-server, поэтому графики CPU/памяти будут
            «unavailable», а PersistentVolumeClaim будут висеть в Pending, пока
            вы их не установите (мастер напомнит об этом после создания).
          </li>
          <li>
            Create откажется работать (ничего не меняя), если на сервере уже
            есть <span className="font-mono">/etc/rancher/k3s</span> или занят
            порт 6443 — для готового кластера используйте Import. Установка
            занимает 1–2 минуты; поддерживается один сервер (без HA). После
            создания Vinaire предложит «Add worker nodes» — см. «Добавление
            ноды».
          </li>
        </ul>
      </div>

      <div>
        <div className="font-semibold text-foreground">Типы кластеров</div>
        <div className="mt-1 text-text-muted">
          Каждому кластеру присваивается тип: k3s, k3d, kind, minikube,
          microk8s, kubeadm, managed (облачный) или unknown. Он определяется
          автоматически, показан значком в переключателе кластеров и на странице
          Clusters и может быть исправлен там же в выпадающем списке. Тип влияет
          только на одно: кнопка «Add node» показывается для k3s и kubeadm, где
          добавить сервер по SSH имеет смысл, и скрыта для k3d/kind (там «ноды»
          — это контейнеры), minikube и облачных кластеров.
        </div>
      </div>

      <div>
        <div className="font-semibold text-foreground">
          Что хранится и как защищено
        </div>
        <ul className="mt-1 list-disc pl-5 text-text-muted">
          <li>
            Список добавленных кластеров (с их учётными данными) шифруется
            AES-256-GCM. Ключ берётся из{" "}
            <span className="font-mono">CLUSTER_STORE_KEY</span> (base64, 32
            байта: <span className="font-mono">openssl rand -base64 32</span>),
            а если её нет — генерируется и лежит отдельно: в Secret{" "}
            <span className="font-mono">vinaire-clusters-key</span> (в кластере)
            или в файле с правами 0600 в{" "}
            <span className="font-mono">VINAIRE_DATA_DIR</span>.
          </li>
          <li>
            В мастере, если шифрование недоступно, это будет написано прямо
            («stored UNENCRYPTED»).
          </li>
          <li>
            Из kubeconfig берутся учётные данные только выбранного контекста,
            только встроенные в файл (inline); exec-плагины, ссылки на файлы и
            proxy-url не поддерживаются — они запускали бы команды или читали
            файлы на машине Vinaire.
          </li>
          <li>
            Один и тот же кластер нельзя добавить дважды под разными именами.
            «Remove» убирает только запись в Vinaire — сам кластер не
            затрагивается и продолжает работать.
          </li>
        </ul>
      </div>

      <div>
        <div className="font-semibold text-foreground">Частые ошибки</div>
        <ul className="mt-1 list-disc pl-5 text-text-muted">
          <li>
            <span className="font-mono">
              x509: certificate is valid for …, not …
            </span>{" "}
            — сертификат кластера не включает адрес, по которому подключается
            Vinaire. Используйте адрес из списка в ошибке, поле «API URL» или
            создайте кластер с нужным{" "}
            <span className="font-mono">--tls-san</span>.
          </li>
          <li>
            «already registered as …» — такой кластер уже добавлен (определяется
            по его реальному идентификатору, а не по адресу).
          </li>
          <li>
            «cluster management is unavailable…» — некуда сохранять список:
            запустите Vinaire в кластере или задайте{" "}
            <span className="font-mono">VINAIRE_DATA_DIR</span>.
          </li>
          <li>
            «exec plugin… not supported» — в kubeconfig учётные данные
            получаются запуском программы (так делают EKS/GKE). Возьмите токен
            ServiceAccount и используйте «Server URL + token».
          </li>
          <li>
            Статус «unreachable» на странице Clusters — Vinaire не достучалась
            до API: проверьте адрес, сеть и что кластер запущен.
          </li>
        </ul>
      </div>

      <div>
        <div className="mb-1 text-[13px] font-semibold">
          Способ «Server URL + token»: пошагово
        </div>
        <div className="text-xs text-text-muted">
          Vinaire не хранит своих собственных прав доступа — она заходит в
          подключаемый кластер под учётной записью (ServiceAccount), которую вы
          создаёте сами, и видит/может делать ровно то, что этой учётной записи
          разрешает RBAC в самом кластере. Ниже — как её создать и подключить.
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
          6. Заполните форму «Server URL + token» в мастере
        </div>
        <ul className="mt-1 list-disc pl-5 text-text-muted">
          <li>
            <span className="font-semibold text-foreground">
              Name in Vinaire
            </span>{" "}
            — произвольное короткое имя, под которым кластер будет показан в
            переключателе кластеров (например,{" "}
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
          сохранится. Дальше именно RBAC-роль, которую вы выдали на шаге 3,
          определяет, что можно увидеть и сделать — Vinaire ничего не добавляет
          и не убавляет сверху. Подключённые кластеры видны и переключаются
          через выпадающий список в верхней панели.
        </div>
      </div>
    </div>
  );
}

function NodeHelp() {
  return (
    <div className="flex flex-col gap-5">
      <div>
        <div className="mb-1 text-[13px] font-semibold">
          Как добавить ноду (рабочий сервер) в кластер
        </div>
        <div className="text-xs text-text-muted">
          На странице{" "}
          <Link href="/nodes" className="text-accent-text">
            Nodes
          </Link>{" "}
          кнопка «Add node»: вы указываете чистый сервер, Vinaire сама заходит
          на него по SSH, ставит k3s-агент и присоединяет сервер к выбранному
          сейчас кластеру. На самом сервере вручную выполнять ничего не нужно.
        </div>
      </div>

      <div>
        <div className="font-semibold text-foreground">
          Когда кнопки «Add node» нет
        </div>
        <div className="mt-1 text-text-muted">
          Кнопка показывается только для кластеров, где добавить машину по SSH
          имеет смысл — типы k3s и kubeadm. Для k3d и kind «ноды» — это Docker-
          контейнеры, minikube — одна виртуальная машина, а облачные кластеры
          масштабируются средствами провайдера, поэтому вместо кнопки вы увидите
          пояснение. Если тип определён неверно — исправьте его на странице{" "}
          <Link href="/clusters" className="text-accent-text">
            Clusters
          </Link>
          .
        </div>
      </div>

      <div>
        <div className="font-semibold text-foreground">Что нужно заранее</div>
        <ul className="mt-1 list-disc pl-5 text-text-muted">
          <li>
            Чистый Linux-сервер с systemd (или OpenRC) и curl/wget, на котором
            ещё нет k3s (иначе проверка остановится и ничего не изменит).
          </li>
          <li>
            Доступ по SSH: root или пользователь с sudo. По ключу — лучше; с
            паролем тоже работает (тот же пароль используется для sudo).
          </li>
          <li>
            Сервер должен видеть API кластера (порт 6443) по адресу, который вы
            укажете, — это проверяется первым же шагом. Адрес должен быть таким,
            каким он виден <span className="italic">с нового сервера</span>, а
            не <span className="font-mono">127.0.0.1</span>.
          </li>
          <li>
            Токен присоединения k3s: либо вставьте его, либо дайте Vinaire SSH
            на control-plane, и она прочитает его сама.
          </li>
          <li>
            У вас должно быть право создавать узлы в кластере (RBAC{" "}
            <span className="font-mono">create</span> на{" "}
            <span className="font-mono">nodes</span>), а Vinaire открыта по
            HTTPS.
          </li>
        </ul>
        <div className="mt-1 text-text-muted">
          Токен на control-plane (k3s) выглядит так:
        </div>
        <Code>{`sudo cat /var/lib/rancher/k3s/server/node-token`}</Code>
      </div>

      <div>
        <div className="font-semibold text-foreground">Шаги в диалоге</div>
        <ol className="mt-1 list-decimal pl-5 text-text-muted">
          <li>
            Введите адрес, порт и пользователя нового сервера и выберите способ
            входа (SSH-ключ рекомендуется).
          </li>
          <li>
            «Joining the cluster»: вставьте токен — или выберите чтение токена с
            control-plane по SSH (тогда нужен и вход на control-plane). Укажите
            адрес API кластера как он виден с нового сервера. Имя ноды и версию
            k3s указывать необязательно: имя по умолчанию — hostname, версия
            подбирается под ваш control-plane, чтобы воркер не оказался новее
            сервера.
          </li>
          <li>
            «Continue»: Vinaire покажет отпечаток SSH-ключа сервера. Сверьте его
            с тем, что выдаёт на самом сервере{" "}
            <span className="font-mono">
              ssh-keygen -lf /etc/ssh/ssh_host_*_key.pub
            </span>
            , и поставьте галочку. Пока не подтверждено — ничего секретного не
            отправляется.
          </li>
          <li>
            «Add node»: идёт установка, лог виден вживую. Обычно 1–2 минуты. В
            конце нода появляется в списке Nodes со статусом Ready.
          </li>
        </ol>
      </div>

      <div>
        <div className="font-semibold text-foreground">
          Если кластер — kubeadm (обычный Kubernetes)
        </div>
        <div className="mt-1 text-text-muted">
          Vinaire сама выберет kubeadm по типу кластера. Вместо токена k3s нужна{" "}
          <span className="font-semibold">join-команда</span> — её печатает
          control-plane:
        </div>
        <Code>{`sudo kubeadm token create --print-join-command`}</Code>
        <div className="mt-1 text-text-muted">
          Вставьте её целиком (или дайте Vinaire SSH на control-plane — она
          создаст команду сама). Адрес API берётся из самой команды. Новый
          сервер должен быть Debian/Ubuntu, минимум 2 CPU и 2 ГБ RAM, с доступом
          к pkgs.k8s.io и registry.k8s.io; Vinaire подготовит его (swap, модули
          ядра, containerd, пакеты нужной версии — как у вашего control-plane) и
          выполнит join. Перед этим проверяется, что имя хоста и product_uuid не
          совпадают с уже существующими нодами и что часы сервера не расходятся
          с вашими: расхождение ломает сертификаты и токены присоединения. Токен
          и хэш CA в логе скрываются.
        </div>
      </div>

      <div>
        <div className="font-semibold text-foreground">
          Reset: если установка оборвалась на полпути
        </div>
        <div className="mt-1 text-text-muted">
          Прерванная установка оставляет сервер в неопределённом состоянии, и
          повторить её поверх нельзя. Если установка (создание кластера или
          добавление ноды) закончилась ошибкой, в окне появится «Reset this
          server…»: Vinaire снова спросит SSH-доступ, покажет отпечаток ключа и
          потребует ввести адрес сервера — это необратимо. Для kubeadm
          выполняется <span className="font-mono">kubeadm reset -f</span>, затем
          чистятся конфигурация CNI, правила iptables и остатки состояния
          Kubernetes; для k3s запускается его собственный uninstall-скрипт.
          После Reset сервер можно настраивать заново с нуля.
        </div>
      </div>

      <div>
        <div className="font-semibold text-foreground">Безопасность</div>
        <div className="mt-1 text-text-muted">
          Пароль, ключ и токен живут в памяти только на время операции: они не
          сохраняются на диск, не попадают в логи и в журнал действий (там
          только адрес, пользователь и тип входа). Установка идёт от root через
          официальный установщик k3s. Если Vinaire открыта не по HTTPS, форма
          откажется отправлять учётные данные (для локальной отладки есть{" "}
          <span className="font-mono">PROVISION_ALLOW_INSECURE=true</span>).
          Адреса 127.0.0.1 и адрес метаданных облака (169.254.169.254) запрещены
          как цель.
        </div>
      </div>

      <div>
        <div className="font-semibold text-foreground">
          Если что-то пошло не так
        </div>
        <ul className="mt-1 list-disc pl-5 text-text-muted">
          <li>
            «k3s is already installed here» — на сервере уже есть k3s. Для
            чистого старта выполните на нём{" "}
            <span className="font-mono">k3s-agent-uninstall.sh</span> (или{" "}
            <span className="font-mono">k3s-uninstall.sh</span> для сервера).
          </li>
          <li>
            «can’t reach the cluster API» — новый сервер не видит адрес API:
            проверьте адрес, файрвол и порт 6443.
          </li>
          <li>
            «host key mismatch» — отпечаток сервера не совпал с подтверждённым:
            не продолжайте, пока не выясните причину.
          </li>
          <li>
            «Not allowed — adding nodes requires permission…» — у вашей учётной
            записи нет права <span className="font-mono">create</span> на{" "}
            <span className="font-mono">nodes</span> в кластере.
          </li>
          <li>
            Ошибка sudo — для пользователя без root нужен вход с паролем (он же
            пароль sudo) или sudo без пароля (NOPASSWD).
          </li>
          <li>
            Нода появилась, но остаётся NotReady — обычно не открыты порты между
            нодами (10250/tcp и 8472/udp для сети flannel).
          </li>
        </ul>
      </div>

      <div>
        <div className="font-semibold text-foreground">Как убрать ноду</div>
        <div className="mt-1 text-text-muted">
          Удаление ноды из Vinaire пока не делается. Вручную: выведите ноду из
          работы и удалите её из кластера, затем очистите сервер.
        </div>
        <Code>
          {`kubectl drain <имя-ноды> --ignore-daemonsets --delete-emptydir-data
kubectl delete node <имя-ноды>
# на самом сервере:
/usr/local/bin/k3s-agent-uninstall.sh`}
        </Code>
      </div>
    </div>
  );
}

function MarketplaceHelp() {
  return (
    <div className="flex flex-col gap-5">
      <div>
        <div className="mb-1 text-[13px] font-semibold">
          Что такое Marketplace
        </div>
        <div className="text-xs text-text-muted">
          Каталог готовых приложений для кластера — Helm-чартов. Вместо ручного{" "}
          <span className="font-mono">helm install</span> вы выбираете чарт, имя
          релиза, namespace и значения, а Vinaire ставит его сама. Для свежего
          kubeadm-кластера здесь удобно ставить то, чего в нём нет:
          Ingress-контроллер (ingress-nginx), metrics-server для графиков
          CPU/памяти, cert-manager для TLS.
        </div>
      </div>

      <div>
        <div className="font-semibold text-foreground">Как установить</div>
        <ol className="mt-1 list-decimal pl-5 text-xs text-text-muted">
          <li>
            Вкладка «Catalog»: найдите чарт и откройте его. Видны README и все
            значения по умолчанию (вкладка «Chart defaults»).
          </li>
          <li>
            Укажите имя релиза и namespace (если такого namespace нет, он будет
            создан). В «Your values» пишите только то, что хотите изменить, —
            остальное останется по умолчанию.
          </li>
          <li>
            Если у чарта есть верхнеуровневое значение{" "}
            <span className="font-mono">nodeSelector</span>, появится выбор
            ноды, на которой запустить приложение. У чартов с другой раскладкой
            значений (например,{" "}
            <span className="font-mono">controller.nodeSelector</span>) Vinaire
            не гадает: впишите нужное значение сами.
          </li>
        </ol>
      </div>

      <div>
        <div className="font-semibold text-foreground">
          Установленные релизы: Upgrade, Rollback, Uninstall
        </div>
        <div className="mt-1 text-text-muted">
          Вкладка «Installed» показывает Helm-релизы, которые вы можете видеть.
          Upgrade открывает редактор с текущими значениями релиза (они{" "}
          <span className="font-semibold">заменяют</span> прежние, ничего не
          сливается незаметно) и выбор версии чарта. Rollback возвращает релиз к
          более ранней ревизии — как новую ревизию, история не стирается.
          Uninstall удаляет релиз и всё, что создал чарт, и просит ввести имя
          релиза: данные в созданных чартом томах могут пропасть.
        </div>
      </div>

      <div>
        <div className="font-semibold text-foreground">
          Откуда берутся чарты и как найти нужный
        </div>
        <div className="mt-1 text-text-muted">
          В каталоге около пятидесяти источников, сгруппированных по категориям
          (Databases, Messaging, Networking, Security, Observability, Storage,
          CI/CD, Platform, Apps) — переключайте их кнопками над списком. Поиск
          ранжирует по релевантности: сначала имя чарта, затем ключевые слова и
          описание. PostgreSQL ищите по{" "}
          <span className="font-mono">postgres</span> (это найдёт CloudNativePG,
          Zalando, Percona и Bitnami), Kafka — по{" "}
          <span className="font-mono">kafka</span> (Strimzi, Redpanda, Bitnami).
          Источники бывают двух видов: обычные Helm-репозитории и OCI-реестры
          (помечены «OCI»). OCI читается анонимно и только по TLS, без учётных
          данных этой машины; скачанный чарт проверяется по контрольной сумме.
        </div>
        <div className="mt-2 text-text-muted">
          <span className="font-semibold">Bitnami:</span> его бесплатные образы
          публикуются только с тегом <span className="font-mono">latest</span>.
          Для разработки это нормально, для продакшена с закреплёнными версиями
          — нет; для такого случая берите операторы (CloudNativePG, Strimzi,
          Percona). Список источников меняется только в конфигурации бэкенда (
          <span className="font-mono">catalog.json</span> /{" "}
          <span className="font-mono">CATALOG_CONFIG</span>) — там же можно
          добавить свой репозиторий или OCI-реестр с перечнем чартов.
        </div>
      </div>

      <div>
        <div className="font-semibold text-foreground">
          Права и безопасность
        </div>
        <div className="mt-1 text-text-muted">
          Установка идёт от вашего имени, не от имени Vinaire: что чарт может
          создать, решает RBAC вашего токена. Helm хранит каждый релиз как
          Secret (<span className="font-mono">helm.sh/release.v1</span>) в
          namespace релиза, поэтому нужны права на Secrets, плюс права на всё,
          что создаёт сам чарт (Deployment, Service, ClusterRole, CRD…). Пример
          блока для ClusterRole — в справке «Подключение кластера».
        </div>
        <div className="mt-2 text-text-muted">
          Список репозиториев задаётся только конфигурацией бэкенда (
          <span className="font-mono">catalog.json</span> или файл из{" "}
          <span className="font-mono">CATALOG_CONFIG</span>) — из интерфейса
          добавить произвольный адрес нельзя. Чарт используется, только если его
          скачанный архив совпал с контрольной суммой из индекса репозитория.
          Каждое действие пишется в аудит (без значений — в них бывают пароли).
          Поду Vinaire нужен исходящий доступ к адресам репозиториев.
        </div>
      </div>

      <div>
        <div className="font-semibold text-foreground">
          Если установка не прошла
        </div>
        <div className="mt-1 text-text-muted">
          «No matches for kind …» / «ensure CRDs are installed first» — чарту
          нужны CRD, которых в кластере ещё нет. Сначала поставьте их (у многих
          чартов есть отдельный CRD-чарт или значение вроде{" "}
          <span className="font-mono">installCRDs=true</span>) и повторите.
          «Forbidden» — у вашего токена не хватает прав; сообщение называет,
          каких именно. Релиз со статусом «failed» остаётся в списке — его можно
          откатить или удалить.
        </div>
      </div>
    </div>
  );
}

function GitOpsHelp() {
  return (
    <div className="flex flex-col gap-5">
      <div>
        <div className="mb-1 text-[13px] font-semibold">Что такое GitOps</div>
        <div className="text-xs text-text-muted">
          Состояние кластера описано файлами в git, а специальный контроллер
          следит, чтобы кластер им соответствовал. В Vinaire режим задаётся для
          каждого кластера отдельно на странице GitOps (на странице кластеров
          есть кнопка «GitOps»). Пока режим «Off», пункта GitOps в меню нет.
        </div>
      </div>

      <div>
        <div className="font-semibold text-foreground">Native</div>
        <div className="mt-1 text-text-muted">
          Небольшая встроенная синхронизация — для кластеров без Argo CD. Вы
          добавляете источник: https-адрес репозитория, ветку или тег и папку с
          манифестами. Vinaire периодически читает репозиторий, сравнивает
          каждый объект с кластером и применяет то, что отличается. Показывается
          статус по каждому объекту, коммит, который проверен и применён, и
          кнопка Sync now.
        </div>
        <ul className="mt-2 list-disc pl-5 text-xs text-text-muted">
          <li>
            <span className="font-semibold">От чьего имени применяется.</span>{" "}
            Фоновая синхронизация идёт, когда никто не вошёл, поэтому ваш токен
            использовать нельзя, а от имени самой Vinaire она не применяет
            ничего — это дало бы любому, кто может добавить источник, права
            Vinaire. Вместо этого при создании источника вы даёте токен
            отдельного ServiceAccount; права этого аккаунта и есть всё, что
            репозиторий сможет сделать. Страница покажет, от чьего имени идёт
            синхронизация, и предупредит, если это cluster-admin.
          </li>
          <li>
            <span className="font-semibold">Автоприменение.</span> Включено —
            каждое изменение в git применяется, а ручные правки того, чем
            управляет git, возвращаются. Выключено — Vinaire только сообщает о
            расхождениях, применяет по Sync now.
          </li>
          <li>
            <span className="font-semibold">Забирать поля у других.</span> Без
            этой опции поле, изменённое через kubectl или другим контроллером,
            показывается как конфликт и не затирается. Включите её, чтобы git
            был единственным источником правды.
          </li>
          <li>
            <span className="font-semibold">Удаления нет.</span> Объект,
            убранный из git, из кластера не удаляется: он помечается «removed
            from git — still in the cluster». Удалять придётся самим.
          </li>
          <li>
            Читаются только файлы .yaml/.yml/.json (kustomize и Helm-шаблоны
            здесь не раскрываются), порядок: Namespaces, затем CRD, затем всё
            остальное. Приватный репозиторий — токен только на чтение; он, как и
            токен ServiceAccount, хранится зашифрованным и больше не
            показывается.
          </li>
        </ul>
        <Code>
          {`kubectl create serviceaccount gitops -n <ns>
kubectl create role gitops -n <ns> --verb=get,list,create,patch,update \
  --resource=configmaps,services,deployments.apps
kubectl create rolebinding gitops -n <ns> --role=gitops --serviceaccount=<ns>:gitops
kubectl create token gitops -n <ns> --duration=8760h`}
        </Code>
      </div>

      <div>
        <div className="font-semibold text-foreground">Argo CD</div>
        <div className="mt-1 text-text-muted">
          Если Argo CD уже стоит в кластере (его можно поставить из Marketplace,
          репозиторий <span className="font-mono">argo</span>), этот режим
          показывает его Application — синхронизацию и здоровье — и даёт Sync
          now и создание Application по шаблону. Всё это идёт от вашего имени,
          права решает RBAC. Если выбрать Native там, где Argo CD найден, будет
          предупреждение: два контроллера на одном кластере могут перезаписывать
          друг друга.
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
          Prometheus рисует вкладку Metrics на страницах Pod, Deployment и Node
          — история CPU, памяти, сети и файловой системы со ступенчатым графиком
          (диапазон 1 ч — 7 дней, живое обновление каждые 10 секунд). Текущие
          значения на Overview и в Nodes по-прежнему берутся из metrics-server и
          Prometheus не требуют. Vinaire сам составляет PromQL и ждёт обычные
          метрики cAdvisor из kubelet (
          <span className="font-mono">container_cpu_usage_seconds_total</span>,{" "}
          <span className="font-mono">container_memory_working_set_bytes</span>,{" "}
          <span className="font-mono">container_network_*_bytes_total</span>,{" "}
          <span className="font-mono">container_fs_usage_bytes</span>) — их
          отдаёт kube-prometheus-stack «из коробки». Для Node нужна метка{" "}
          <span className="font-mono">node</span> на этих сериях (в
          kube-prometheus-stack она есть). Если какой-то метрики нет (частый
          случай — файловая система пода на containerd), вкладка честно пишет
          «нет данных», а не рисует пустой график; без PROMETHEUS_URL — «метрики
          недоступны».
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
