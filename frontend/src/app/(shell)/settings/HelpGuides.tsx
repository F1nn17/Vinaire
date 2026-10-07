import Link from "next/link";
import type { ReactNode } from "react";

// Справка: обзорные разделы («что умеет Vinaire», «с чего начать», «права и
// роли», «частые проблемы», «о проекте»). Более узкие инструкции — подключение
// кластера, Marketplace, GitOps и т. д. — лежат в settings/page.tsx.

function Code({ children }: { children: string }) {
  return (
    <pre className="mt-1.5 overflow-auto rounded-md bg-surface-2 p-2.5 font-mono text-[11px] text-foreground">
      {children}
    </pre>
  );
}

function H({ children }: { children: ReactNode }) {
  return (
    <div className="mb-1 text-[13px] font-semibold text-foreground">
      {children}
    </div>
  );
}

function Mono({ children }: { children: ReactNode }) {
  return <span className="font-mono">{children}</span>;
}

function Note({ children }: { children: ReactNode }) {
  return (
    <div className="rounded-md border border-warn/60 bg-surface-2 p-3 text-text-muted">
      {children}
    </div>
  );
}

const featureRows: { area: string; what: ReactNode }[] = [
  {
    area: "Overview",
    what: "Состояние выбранного кластера: ноды, поды, события, загрузка CPU и памяти.",
  },
  {
    area: "Clusters",
    what: "Несколько кластеров в одном окне. Добавление по токену, по kubeconfig, из найденных на этой машине, по SSH (установка k3s/kubeadm). Удаление кластера убирает только его регистрацию в Vinaire.",
  },
  {
    area: "Topology",
    what: "Карта связей: что к чему относится (сервис → поды → ноды).",
  },
  {
    area: "Namespaces",
    what: "Список пространств имён, их квоты и лимиты, создание и удаление (с подтверждением вводом имени).",
  },
  {
    area: "Nodes",
    what: "Ноды, их ёмкость и загрузка; добавление ноды по SSH и сброс (reset) для поддерживаемых типов кластеров.",
  },
  {
    area: "Deployments & pods",
    what: "Deployments, StatefulSets, DaemonSets, Jobs и поды. Действия: масштабирование, перезапуск, пауза/возобновление, откат на прошлую ревизию, удаление. У пода — логи (живые и поиск по истории), терминал, проброс порта, события, метрики, YAML.",
  },
  {
    area: "YAML и формы",
    what: "Любой объект можно править как YAML (Monaco). Перед записью кластер проверяет манифест (dry-run), вы видите diff и только потом применяете. Создание Deployment, Service, Ingress, ConfigMap, Secret и Namespace доступно и формой.",
  },
  {
    area: "Marketplace",
    what: "Установка приложений из Helm-репозиториев и OCI-реестров (PostgreSQL, Kafka, Redis, Keycloak, Grafana…) с живым прогрессом, проверкой ваших прав до начала, обновлением, откатом и удалением релизов.",
  },
  {
    area: "GitOps",
    what: "Встроенная синхронизация манифестов из git (native) или работа с Argo CD — включается отдельно для кластера.",
  },
  {
    area: "ConfigMaps / Secrets",
    what: "Просмотр и редактирование. Значения Secret скрыты, показать их можно явным действием — оно записывается в аудит.",
  },
  {
    area: "Storage / Network / RBAC",
    what: "PVC, PV и StorageClass; Service, Ingress и Gateway; роли и привязки (только просмотр).",
  },
  {
    area: "Поиск и уведомления",
    what: "⌘K / Ctrl+K — быстрый переход к любому ресурсу; колокольчик показывает алерты, если подключена система метрик.",
  },
  {
    area: "Аудит и безопасность",
    what: "Каждое изменение пишется в журнал аудита (кто, что, какой объект — без паролей и значений секретов), и его можно просматривать на странице Audit log (нужно право get на audit.vinaire.io — роль -admin). Права решает сам Kubernetes: Vinaire действует от вашего имени.",
  },
];

export function OverviewHelp() {
  return (
    <div className="flex flex-col gap-5">
      <div>
        <H>Что умеет Vinaire</H>
        <div className="text-xs text-text-muted">
          Vinaire — веб-интерфейс к Kubernetes. По умолчанию он{" "}
          <span className="font-semibold text-foreground">только читает</span>;
          изменения — это небольшой, заранее известный набор действий, каждое из
          которых проходит с вашими правами и попадает в аудит. Всё, что вы
          делаете, Kubernetes проверяет так же, как проверил бы{" "}
          <Mono>kubectl</Mono> от вашего имени.
        </div>
      </div>

      <div className="overflow-hidden rounded-md border border-border">
        {featureRows.map((r, i) => (
          <div
            key={r.area}
            className={`grid grid-cols-[11rem_1fr] gap-3 px-3 py-2.5 ${
              i > 0 ? "border-t border-border" : ""
            }`}
          >
            <div className="font-semibold text-foreground">{r.area}</div>
            <div className="text-text-muted">{r.what}</div>
          </div>
        ))}
      </div>

      <div className="text-xs text-text-muted">
        Подробности по каждой теме — в соседних разделах слева: подключение
        кластера, добавление ноды, Marketplace, GitOps, кастомные ресурсы, вход
        по логину и паролю, метрики и логи.
      </div>
    </div>
  );
}

export function StartHelp() {
  return (
    <div className="flex flex-col gap-5">
      <div>
        <H>С чего начать</H>
        <ol className="list-decimal pl-5 text-text-muted">
          <li>
            <span className="font-semibold text-foreground">Войдите.</span> В
            режиме <Mono>token</Mono> (по умолчанию) вставьте токен
            ServiceAccount — как его получить, см. «Права и роли». Токен живёт
            ограниченное время; когда он истечёт, Vinaire вернёт вас на экран
            входа.
          </li>
          <li className="mt-1.5">
            <span className="font-semibold text-foreground">
              Выберите кластер
            </span>{" "}
            в верхней панели. Каждая страница показывает именно его. Добавить
            ещё один можно на странице{" "}
            <Link href="/clusters" className="text-accent-text">
              Clusters
            </Link>
            .
          </li>
          <li className="mt-1.5">
            <span className="font-semibold text-foreground">
              Найдите нужное.
            </span>{" "}
            Меню слева разбито по темам; <Mono>⌘K</Mono> / <Mono>Ctrl+K</Mono>{" "}
            ищет ресурс по имени.
          </li>
        </ol>
      </div>

      <div>
        <H>Типичные задачи</H>
        <div className="flex flex-col gap-2.5 text-text-muted">
          <div>
            <span className="font-semibold text-foreground">
              Посмотреть логи приложения.
            </span>{" "}
            Deployments &amp; pods → под → вкладка Logs. «Live» — поток в
            реальном времени с поиском по тексту (Enter — следующее совпадение);
            «History» — поиск по истории, если подключён Loki.
          </div>
          <div>
            <span className="font-semibold text-foreground">
              Перезапустить приложение.
            </span>{" "}
            Откройте Deployment → значок «Restart rollout» справа вверху: поды
            заменятся по очереди без простоя.
          </div>
          <div>
            <span className="font-semibold text-foreground">
              Изменить конфигурацию.
            </span>{" "}
            Откройте объект → вкладка YAML → правка → Apply. Сначала кластер
            проверит манифест, вы увидите diff, и только после подтверждения
            изменение запишется.
          </div>
          <div>
            <span className="font-semibold text-foreground">
              Поставить готовое приложение.
            </span>{" "}
            Marketplace → выбрать чарт → указать имя и namespace → Install. До
            начала Vinaire проверит ваши права и покажет, чего не хватает.
          </div>
          <div>
            <span className="font-semibold text-foreground">
              Зайти в контейнер.
            </span>{" "}
            Под → вкладка Terminal (нужно право <Mono>pods/exec</Mono>).
          </div>
          <div>
            <span className="font-semibold text-foreground">
              Открыть веб-интерфейс пода.
            </span>{" "}
            Под → «Forward port» — откроется в новой вкладке через защищённый
            прокси Vinaire; на сервере никакой порт не открывается.
          </div>
        </div>
      </div>
    </div>
  );
}

export function RolesHelp() {
  return (
    <div className="flex flex-col gap-5">
      <div>
        <H>Права и роли</H>
        <div className="text-xs text-text-muted">
          В Vinaire нет собственной системы прав: что вы можете, решает
          Kubernetes RBAC для того токена, под которым вы вошли. Нет права — вы
          получите настоящий ответ <Mono>403 Forbidden</Mono> от самого
          кластера. Поэтому главный вопрос всегда один: «какие роли привязаны к
          моей учётной записи».
        </div>
      </div>

      <Note>
        <span className="font-semibold text-foreground">
          Два разных ServiceAccount — не путайте.
        </span>{" "}
        Первый — собственный аккаунт Vinaire (его создаёт Helm-чарт; он нужен
        только для живых обновлений и почти ничего не может). Второй —{" "}
        <span className="italic">ваш</span>: токен именно этого аккаунта вы
        вставляете на экране входа. Права нужно выдавать второму.
      </Note>

      <div>
        <H>Какие роли ставит чарт</H>
        <div className="overflow-hidden rounded-md border border-border">
          {[
            [
              "<имя>-viewer",
              "Просмотр всего, что показывает Vinaire, плюс набор фиксированных действий: перезапуск, масштабирование, пауза, откат, удаление пода и деплоймента, логи, терминал, проброс порта.",
            ],
            [
              "<имя>-installer",
              "Создание, изменение и удаление объектов, которые обычно входят в Helm-чарты (Deployment, Service, Ingress, NetworkPolicy, Secret и т. д.). Нужна для Marketplace и для создания ресурсов из YAML. Работает вместе с viewer.",
            ],
            [
              "<имя>-node-admin",
              "Обслуживание нод: cordon / uncordon, drain (вытеснение подов с учётом PodDisruptionBudget), метки, taints и удаление объекта Node. Отдельная роль, чтобы вывод ноды из работы был осознанным решением. Работает вместе с viewer.",
            ],
            [
              "<имя>-rbac-admin",
              "Управление доступом: создать и удалить RoleBinding и ClusterRoleBinding, спросить «что может X». Роли не создаются — выдаются только существующие; Kubernetes не даст выдать то, чего нет у вас самих (кроме ролей этого чарта — на них есть право bind). Работает вместе с viewer.",
            ],
            [
              "<имя>-admin",
              "Право менять настройки самого Vinaire: реестр кластеров и GitOps — а также чтение журнала аудита. ЭТО НЕ АДМИНИСТРАТОР КЛАСТЕРА: внутри Kubernetes эта роль не даёт ни одного права — ни читать под, ни ставить чарты. Привязывайте её вместе с viewer, а не вместо.",
            ],
          ].map(([role, what], i) => (
            <div
              key={role}
              className={`grid grid-cols-[10rem_1fr] gap-3 px-3 py-2.5 ${
                i > 0 ? "border-t border-border" : ""
              }`}
            >
              <div className="font-mono text-foreground">{role}</div>
              <div className="text-text-muted">{what}</div>
            </div>
          ))}
        </div>
        <div className="mt-1.5 text-xs text-text-faint">
          <Mono>&lt;имя&gt;</Mono> — имя релиза чарта (для релиза{" "}
          <Mono>vinaire</Mono> это <Mono>vinaire-viewer</Mono> и т. д.). Все
          роли создаются непривязанными: кому их выдать — решаете вы.
        </div>
      </div>

      <div>
        <H>Как завести себе учётную запись</H>
        <Code>{`# 1. ServiceAccount для вашего входа
kubectl create serviceaccount me -n vinaire

# 2. Просмотр и действия
kubectl create clusterrolebinding me-vinaire-viewer \\
  --clusterrole=vinaire-viewer --serviceaccount=vinaire:me

# 3. Установка приложений из Marketplace (по желанию)
kubectl create clusterrolebinding me-vinaire-installer \\
  --clusterrole=vinaire-installer --serviceaccount=vinaire:me

# 4. Обслуживание нод — cordon, drain, метки, taints (по желанию)
kubectl create clusterrolebinding me-vinaire-node-admin \\
  --clusterrole=vinaire-node-admin --serviceaccount=vinaire:me

# 5. Настройки самого Vinaire — кластеры, GitOps (по желанию)
kubectl create clusterrolebinding me-vinaire-admin \\
  --clusterrole=vinaire-admin --serviceaccount=vinaire:me

# 6. Токен для экрана входа (с ограниченным сроком)
kubectl create token me -n vinaire --duration=1h`}</Code>
        <div className="mt-1.5 text-xs text-text-faint">
          Для одноразового локального теста на своём dev-кластере можно
          привязать <Mono>cluster-admin</Mono>. На общем кластере так не
          делайте.
        </div>
      </div>

      <div>
        <H>Проверить, что права выданы</H>
        <Code>{`kubectl auth can-i list pods --as=system:serviceaccount:vinaire:me --all-namespaces
kubectl auth can-i create networkpolicies.networking.k8s.io \\
  --as=system:serviceaccount:vinaire:me -n postgresql`}</Code>
      </div>

      <div>
        <H>Что нужно для чего</H>
        <div className="overflow-hidden rounded-md border border-border">
          {[
            ["Смотреть ресурсы, логи", "viewer"],
            ["Перезапуск, масштабирование, откат", "viewer"],
            ["Терминал, проброс порта", "viewer (pods/exec, pods/portforward)"],
            [
              "Создать или изменить объект из YAML",
              "installer (на нужные виды)",
            ],
            ["Установить приложение из Marketplace", "viewer + installer"],
            ["Добавить кластер, настроить GitOps", "viewer + admin"],
            ["Добавить ноду по SSH", "право create на nodes"],
            [
              "Cordon, drain, метки и taints, удалить ноду",
              "viewer + node-admin",
            ],
          ].map(([what, role], i) => (
            <div
              key={what}
              className={`grid grid-cols-[1fr_16rem] gap-3 px-3 py-2 ${
                i > 0 ? "border-t border-border" : ""
              }`}
            >
              <div className="text-foreground">{what}</div>
              <div className="font-mono text-xs text-text-muted">{role}</div>
            </div>
          ))}
        </div>
      </div>
    </div>
  );
}

export function TroubleshootingHelp() {
  const items: { title: string; body: ReactNode }[] = [
    {
      title: "После ввода токена меня возвращает на экран входа",
      body: (
        <>
          Чаще всего это cookie. По умолчанию сессионная cookie помечена{" "}
          <Mono>Secure</Mono> — браузер отбрасывает такую cookie, если страница
          открыта по обычному HTTP. Откройте Vinaire по HTTPS. Только для
          локальной проверки по HTTP при установке чарта можно поставить{" "}
          <Mono>--set cookie.secure=false</Mono> (на реальном развёртывании так
          делать нельзя). Также проверьте режим <Mono>authMode</Mono> и не истёк
          ли токен.
        </>
      ),
    },
    {
      title: "403 Forbidden / «pods is forbidden»",
      body: (
        <>
          У учётной записи нет нужного права. Проверьте через{" "}
          <Mono>
            kubectl auth can-i …
            --as=system:serviceaccount:&lt;ns&gt;:&lt;имя&gt;
          </Mono>{" "}
          и убедитесь, что роль привязана к{" "}
          <span className="italic">вашему</span> ServiceAccount, а не к аккаунту
          самого Vinaire. См. «Права и роли».
        </>
      ),
    },
    {
      title: "Установка из Marketplace: «не хватает прав на …»",
      body: (
        <>
          Vinaire заранее проверяет, что чарт создаст и можете ли вы это
          сделать, и показывает список недостающего. Привяжите роль{" "}
          <Mono>-installer</Mono> (вместе с viewer). Если чарт создаёт ещё и
          ClusterRole или CRD — этих прав у installer нет по умолчанию: добавьте
          их через <Mono>rbac.installer.extraRules</Mono> или используйте более
          широкую роль на тестовом кластере. Помните, что роль{" "}
          <Mono>-admin</Mono> прав в кластере не даёт.
        </>
      ),
    },
    {
      title: "Страница установки: «Couldn't load the chart»",
      body: (
        <>
          Бэкенд не смог получить чарт: репозиторий недоступен, индекс ещё
          грузится, у чарта нет контрольной суммы или он слишком большой. Текст
          ошибки показывает причину; кнопка «Try again» повторяет запрос. Если в
          кластере старый образ Vinaire без Marketplace — пересоберите и
          перевыкатите образ.
        </>
      ),
    },
    {
      title: "ImagePullBackOff у самого Vinaire",
      body: (
        <>
          Образ <Mono>vinaire:latest</Mono> не опубликован в реестре — его нужно
          загрузить в кластер. k3d:{" "}
          <Mono>k3d image import vinaire:latest -c &lt;кластер&gt;</Mono>. K3s
          на сервере: <Mono>docker save</Mono>, <Mono>scp</Mono> и{" "}
          <Mono>sudo k3s ctr images import</Mono>; затем проверьте{" "}
          <Mono>sudo k3s ctr images list | grep vinaire</Mono> и что{" "}
          <Mono>image.pullPolicy</Mono> — <Mono>IfNotPresent</Mono>.
        </>
      ),
    },
    {
      title: "Нет графиков метрик («unavailable»)",
      body: (
        <>
          Нужен metrics-server для текущих значений и Prometheus для истории.
          Vinaire их не устанавливает; установить можно из Marketplace.
        </>
      ),
    },
    {
      title: "Кнопка «Add node» неактивна",
      body: (
        <>
          Для кластеров k3d, kind, minikube и managed нода по SSH не добавляется
          (это контейнеры или управляется провайдером). На странице Nodes
          написано, что делать для вашего типа. Если тип определился неверно,
          его можно сменить на странице Clusters.
        </>
      ),
    },
  ];
  return (
    <div className="flex flex-col gap-5">
      <div>
        <H>Частые проблемы</H>
      </div>
      {items.map((it) => (
        <div key={it.title}>
          <div className="font-semibold text-foreground">{it.title}</div>
          <div className="mt-1 text-text-muted">{it.body}</div>
        </div>
      ))}
    </div>
  );
}

export function AboutHelp() {
  return (
    <div className="flex flex-col gap-5">
      <div>
        <H>О проекте и лицензия</H>
        <div className="text-xs text-text-muted">
          Vinaire распространяется по лицензии{" "}
          <span className="font-semibold text-foreground">
            Apache License 2.0
          </span>
          : код можно использовать, изменять и распространять, в том числе в
          коммерческих проектах, при сохранении текста лицензии и уведомления об
          авторских правах. Текст — в файле <Mono>LICENSE</Mono> в корне
          репозитория, список сторонних компонентов — в <Mono>NOTICE</Mono>.
        </div>
      </div>
      <div className="text-xs text-text-muted">
        Встроенные шрифты (IBM Plex Sans, JetBrains Mono) распространяются по
        лицензии SIL Open Font License 1.1 и остаются под ней. Зависимости
        (Kubernetes client-go, Helm, go-git и др.) — под своими лицензиями
        (Apache-2.0, MIT, BSD).
      </div>
    </div>
  );
}
