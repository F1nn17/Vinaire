# Vinaire

[English](./README.md) · **Русский**

Vinaire — собственный веб-интерфейс для управления кластерами Kubernetes: более
дружелюбная и более «со своим мнением» альтернатива Rancher/Headlamp, рассчитанная на
кластеры **k3s**.

По умолчанию интерфейс работает **только на чтение**; исключения — небольшой, намеренно
узкий и полностью аудируемый набор действий (перезапуск, откат и удаление Deployment и
другие — см. главное правило в [AGENTS.md](./AGENTS.md)), а не универсальный путь записи.
Вход (Roadmap, Phase 10) — это либо вставленный токен ServiceAccount Kubernetes, либо
Keycloak/OIDC; в любом случае то, что может конкретный вход, решает настоящий Kubernetes RBAC
этой учётной записи, а не отдельная система прав внутри приложения. Полное обоснование и
соглашения для тех, кто пишет здесь код (человек или ИИ), — в [AGENTS.md](./AGENTS.md).

Дизайн-концепт: интерактивный макет основных экранов (Dashboard, Topology, Workloads,
ConfigMaps, Secrets, Network) лежит вне этого репозитория. Если перед реализацией экрана
нужен контекст UX-решений, попросите владельца проекта рассказать о них.

## Стек

- **Фронтенд** — [`/frontend`](./frontend): Next.js (App Router), React, TypeScript,
  Tailwind CSS. Тёмная тема по умолчанию, акцентный цвет `#8B5FBF`. Шрифты: JetBrains Mono
  (заголовки/моноширинный) и IBM Plex Sans (основной текст), подключены локально.
- **Бэкенд** — [`/backend`](./backend): Go, [client-go](https://github.com/kubernetes/client-go)
  для работы с API Kubernetes, небольшой HTTP-сервер (chi), отдающий REST/JSON фронтенду.

## Быстрый старт

### Что нужно

- Node.js 20+ и npm
- Go 1.27+
- Доступный кластер Kubernetes (для разработки задуман локальный [k3d](https://k3d.io/)) и
  рабочий `~/.kube/config`

### Запустить всё сразу (рекомендуется)

Из корня репозитория:

```bash
npm install   # один раз: ставит корневой оркестратор (concurrently)
npm run dev
```

Запускаются Go-бэкенд (`:8080`) и фронтенд Next.js (`:3100`) в одном терминале, с метками
`BACKEND` / `FRONTEND`; Ctrl+C останавливает оба. В VS Code `dev` виден и как ▶ в панели
NPM Scripts — это версия «одной кнопки».

> Фронтенд работает на порту `3100`, а не на стандартном для Next.js `3000` — выберите
> свободный порт для вашей машины и поправьте `-p` в `frontend/package.json`, если `3100`
> тоже занят.
>
> **Windows:** нужен `go` в `PATH`. Если `npm run dev` падает с `'go' is not recognized`,
> добавьте `C:\Program Files\Go\bin` (или каталог установки Go) в пользовательский PATH и
> откройте новый терминал.

### Только бэкенд

```bash
cd backend
go run .
```

Сервер слушает `:8080`. Когда он запущен, проверьте `GET /healthz`. Если кластер недоступен,
сервер всё равно стартует (просто пишет предупреждение), так что над фронтендом можно
работать без живого кластера.

### Только фронтенд

```bash
cd frontend
npm install
npm run dev
```

Откройте [http://localhost:3100](http://localhost:3100).

> **Известная проблема:** на этой Windows-машине стандартный компилятор Turbopack из Next.js
> 16 падает при обработке `globals.css` (`evaluate_webpack_loader` / «node process exited
> before we could connect to it»). `dev` и `build` в `package.json` закреплены на `--webpack`
> как обходной путь. Если на вашей платформе Turbopack работает, флаг можно убрать.

## Docker (один общий образ)

Для боевого/тестового развёртывания — для повседневной разработки не нужен, она по-прежнему
идёт через `npm run dev` + `go run .`:

```bash
docker build -t vinaire .
docker run -p 8080:8080 --env AUTH_MODE=disabled vinaire
```

Один образ, один процесс: Go-бэкенд отдаёт и API, и статический экспорт фронтенда
(`output: "export"` у `next build`) с одного порта — Node/npm в финальном слое нет. Как это
устроено — в разделах AGENTS.md «Rendering model» и «Authentication (Phase 10)»; для
настоящего развёртывания вместо `disabled` задайте `AUTH_MODE`/`OIDC_*`.

## Установка (Helm)

Для установки на настоящий кластер, а не сборки из исходников. Helm-чарт в
[`/charts/vinaire`](./charts/vinaire) упаковывает тот же единый образ и создаёт **всё, что
нужно Vinaire** — Deployment, Service, собственный ServiceAccount и его RBAC. Писать вручную
Deployment, Service или роль не нужно.

Выберите подходящий сценарий, затем прочтите [заметки, общие для всех](#things-every-install-needs-to-know-ru).

| | Сценарий | Откуда образ | Вход | Доступ |
| --- | --- | --- | --- | --- |
| **1** | [Быстрая локальная проба — K3s / k3d / домашний сервер](#scenario-1-ru) | собран у вас, импортирован вручную | вставленный токен | обычный HTTP |
| **2** | [Обычный Kubernetes](#scenario-2-ru) | реестр | вставленный токен | Ingress |
| **3** | [Production](#scenario-3-ru) | реестр, закреплённая версия | OIDC | HTTPS Ingress |

<a id="scenario-1-ru"></a>

### Сценарий 1 — быстрая локальная проба (K3s, k3d, домашний сервер)

**1. Соберите образ** (на вашей машине, в этом репозитории):

```bash
docker build -t vinaire:latest .
```

**2. Доставьте образ на ноду кластера.** `vinaire:latest` нет ни в одном реестре, поэтому
ноде его нужно передать. Команда зависит от того, где работает кластер:

```bash
# k3d (кластер работает в Docker на этой машине)
k3d image import vinaire:latest -c <имя-кластера>

# K3s на другой машине (домашний сервер): отправьте образ туда и импортируйте
docker save vinaire:latest -o vinaire.tar
scp vinaire.tar user@server:/tmp/vinaire.tar
ssh user@server 'sudo k3s ctr images import /tmp/vinaire.tar'
ssh user@server 'sudo k3s ctr images list | grep vinaire'     # убедиться, что он там
```

`k3d image import` достаёт только кластер k3d в *этом* Docker — для сервера K3s в другом
месте он ничего не делает. Поэтому `image.pullPolicy` в чарте по умолчанию
`IfNotPresent`: с `Always` нода попыталась бы скачать `vinaire:latest` из реестра, не
смогла и застряла бы в `ImagePullBackOff`. (`Never` для импортированного образа тоже
подходит.)

**3. Установите.** В чарте есть готовый файл значений именно для этого случая:

```bash
helm upgrade --install vinaire ./charts/vinaire \
  --namespace vinaire --create-namespace \
  -f ./charts/vinaire/examples/values-local.yaml

kubectl rollout status deployment/vinaire -n vinaire
kubectl get pods -n vinaire
kubectl get svc -n vinaire
```

[`examples/values-local.yaml`](./charts/vinaire/examples/values-local.yaml) использует
импортированный образ, `authMode: token`, Ingress для Traefik (он идёт в составе K3s) на
`vinaire.k3s.home` и **`cookie.secure: false`** (см. [заметку про cookie](#cookie-secure-ru)).
Сделайте так, чтобы имя разрешалось, — добавьте `<ip-сервера> vinaire.k3s.home` в файл
hosts — и откройте `http://vinaire.k3s.home`. Нет Ingress? Выполните
`kubectl port-forward -n vinaire svc/vinaire 8080:8080` и откройте `http://localhost:8080`.

> **Где `kubectl` на сервере K3s?** K3s хранит kubeconfig в `/etc/rancher/k3s/k3s.yaml`,
> читаемом для root. От root выполните `export KUBECONFIG=/etc/rancher/k3s/k3s.yaml` или
> добавляйте к каждой команде `sudo k3s kubectl …`. **Helm CLI в K3s не входит** — поставьте
> его отдельно (<https://helm.sh/docs/intro/install/>) или запускайте Helm со своей машины,
> скопировав туда kubeconfig.

**4. Войдите** — см. [Два разных ServiceAccount](#two-serviceaccounts-ru).

<a id="scenario-2-ru"></a>

### Сценарий 2 — обычный Kubernetes (образ из реестра + Ingress + токен)

Загрузите образ в реестр, из которого кластер может его скачать, затем укажите его чарту и
опубликуйте через ваш Ingress-контроллер (в примере Traefik; используйте `nginx` и т. п. —
что есть у вашего кластера):

```bash
docker build -t <реестр>/<владелец>/vinaire:<версия> .
docker push <реестр>/<владелец>/vinaire:<версия>

helm upgrade --install vinaire ./charts/vinaire \
  --namespace vinaire --create-namespace \
  --set image.repository=<реестр>/<владелец>/vinaire \
  --set image.tag=<версия> \
  --set ingress.enabled=true \
  --set ingress.className=traefik \
  --set 'ingress.hosts[0].host=vinaire.example.com' \
  --set 'ingress.tls[0].secretName=vinaire-tls' \
  --set 'ingress.tls[0].hosts[0]=vinaire.example.com'
```

Оставьте `cookie.secure` по умолчанию (`true`) — TLS завершается на Ingress. Для частного
реестра нужны `imagePullSecrets`. Вход по токену, как в сценарии 1.

<a id="scenario-3-ru"></a>

### Сценарий 3 — production (реестр, HTTPS, OIDC)

Всё из сценария 2, плюс настоящий вход через Keycloak (или другой OIDC-провайдер), секрет
OIDC-клиента в Secret, который вы создаёте сами, и NetworkPolicy. Начните с
[`examples/values-production.yaml`](./charts/vinaire/examples/values-production.yaml), замените
`<заглушки>` и выполните:

```bash
kubectl create namespace vinaire
kubectl create secret generic vinaire-oidc -n vinaire --from-literal=clientSecret='<client secret>'

helm upgrade --install vinaire ./charts/vinaire \
  --namespace vinaire \
  -f values-production.yaml
```

API-сервер самого кластера тоже **должен** доверять OIDC-издателю, иначе вход пройдёт, а все
обращения к кластеру будут возвращать 401 — это разобрано в Vinaire: *Settings → Справка →
Вход по логину и паролю*. Прочтите [docs/security.ru.md](./docs/security.ru.md), прежде чем
выставлять его куда-либо по-настоящему.

<a id="things-every-install-needs-to-know-ru"></a>

### Что нужно знать при любой установке

<a id="two-serviceaccounts-ru"></a>

#### Два разных ServiceAccount — не путайте

1. **Собственный ServiceAccount Vinaire** — создаётся чартом (называется как релиз). Он
   нужен только для наблюдателей живых обновлений и намеренно минимален. Не трогайте его и не
   заменяйте ServiceAccount Deployment на `default`.
2. **Ваш** — учётная запись, *токен которой вы вставляете на экране входа*. Vinaire действует
   от её имени, поэтому RBAC кластера для неё и решает, что вам можно. Создайте её и привяжите
   роли из чарта (они называются `<fullname>-…`; для релиза `vinaire` это `vinaire-…`):

```bash
kubectl create serviceaccount me -n vinaire

# просмотр + фиксированные действия (перезапуск, масштабирование, логи, терминал, проброс порта)
kubectl create clusterrolebinding me-vinaire-viewer \
  --clusterrole=vinaire-viewer --serviceaccount=vinaire:me

# по желанию: установка из Marketplace / применение YAML — вдобавок к viewer
kubectl create clusterrolebinding me-vinaire-installer \
  --clusterrole=vinaire-installer --serviceaccount=vinaire:me

# по желанию: обслуживание нод — cordon, drain, метки, taints, удаление (вдобавок к viewer)
kubectl create clusterrolebinding me-vinaire-node-admin \
  --clusterrole=vinaire-node-admin --serviceaccount=vinaire:me

# по желанию: управление доступом — привязка ролей, «что может X?» (вдобавок к viewer)
kubectl create clusterrolebinding me-vinaire-rbac-admin \
  --clusterrole=vinaire-rbac-admin --serviceaccount=vinaire:me

# по желанию: менять настройки самого Vinaire (кластеры, GitOps) — это НЕ администратор кластера, см. ниже
kubectl create clusterrolebinding me-vinaire-admin \
  --clusterrole=vinaire-admin --serviceaccount=vinaire:me

kubectl create token me -n vinaire --duration=1h      # вставьте это на экране входа
```

Роли делают разное, и **`-admin` — это не администратор кластера**: он лишь позволяет менять
собственные настройки Vinaire (реестр кластеров, GitOps) и читать журнал аудита и не даёт
никаких прав внутри Kubernetes, поэтому всегда выдаётся *вдобавок к* `-viewer`. `-installer`
даёт create / update / delete на namespaced-виды, которые обычно содержат Helm-чарты; чарту,
который создаёт ещё и ClusterRole или CRD, нужно больше, и Vinaire скажет об этом *до* начала
установки (добавьте права через `rbac.installer.extraRules`).

Проверьте, что права именно такие, как вы думаете:

```bash
kubectl auth can-i list pods --as=system:serviceaccount:vinaire:me --all-namespaces        # yes
kubectl auth can-i create clusterrolebindings --as=system:serviceaccount:vinaire:me        # no
```

> **`-viewer` включает чтение Secret.** Интерфейс Vinaire показывает у Secret только
> метаданные, пока вы не воспользуетесь явным, аудируемым действием *reveal*, — но на уровне
> API Kubernetes эта учётная запись может делать `get`/`list` (а Helm хранит каждый релиз как
> Secret, поэтому роли они и нужны). Если учётная запись *не должна* читать Secret, не
> привязывайте к ней `-viewer`: дайте ей собственную роль без них.

#### Режимы входа (`authMode`)

| `authMode` | Что это | Для чего |
| --- | --- | --- |
| `token` *(по умолчанию)* | Вставить токен ServiceAccount на экране входа | Пробы, домашние лаборатории, небольшие команды |
| `oidc` | Настоящий вход через Keycloak / любой OIDC-провайдер | **Production** |
| `disabled` | Входа нет; каждый запрос идёт с правами собственного ServiceAccount Vinaire, которому тогда выдаются широкие права | Только одноразовый локальный кластер — действовать может любой, кто достучится до адреса. Это **не** «использовать права ServiceAccount по умолчанию» |

Никогда не рекомендуйте `cluster-admin` для повседневной работы. Неизвестное значение
(`authMode: tokens`) `values.schema.json` чарта отвергает сообщением со списком допустимых.

<a id="cookie-secure-ru"></a>

#### Почему вас возвращает на экран входа (`cookie.secure`)

Cookie сессии по умолчанию помечена `Secure`. Браузер сохраняет `Secure`-cookie только для
страницы по **HTTPS** (Chrome и Firefox принимают её и на `http://localhost`), поэтому по
обычному HTTP на любом другом адресе вы вводите верный токен и сразу попадаете обратно на
экран входа. Для HTTPS значение по умолчанию правильное — оставьте его. Для **локальной
пробы только по HTTP**:

```bash
helm upgrade --install vinaire ./charts/vinaire … --set cookie.secure=false
```

> ⚠️ С `cookie.secure=false` cookie сессии тоже идёт без шифрования — любой на пути по сети
> может её прочитать. Никогда не делайте так на общем или настоящем кластере.

#### Больше одной реплики

`replicaCount: 2` (и больше) хранит сеансы запечатанными в Secret, общем для всех реплик (`sessions.store: auto`
выбирает это сам; `memory` при нескольких репликах отвергается), выбирает одну реплику для опроса GitOps, включает
привязку по IP клиента у Service и добавляет PodDisruptionBudget. Нужны `rbac.clusterManagement.create: true` и ключ
шифрования (`CLUSTER_STORE_KEY` или управляемый `vinaire-clusters-key`). Settings → Sign-in показывает активные сеансы
и может их завершить.

#### Обновление

`helm upgrade --install` — одна и та же команда и для первой установки, и для каждого
обновления. Чарт 0.2.0 поменял значения по умолчанию (входить как ServiceAccount с привязкой
к `-viewer`, а не как собственный аккаунт Vinaire; `SameSite=Strict`); 0.3.0 добавляет Ingress,
`values.schema.json`, роль `-installer` и примеры и ничего из существовавшего не меняет.

Все параметры — в `charts/vinaire/values.yaml`, прежде всего `authMode`, `ingress`, `cookie`,
`networkPolicy` и `rbac.extraRules` для чтения CRD вашего кластера (Custom Resources; после
входа см. Settings → Справка).

### Устранение неполадок

| Симптом | Вероятная причина и что делать |
| --- | --- |
| `ImagePullBackOff` у пода Vinaire | На ноде нет `vinaire:latest`. Импортируйте его (см. сценарий 1) и проверьте `sudo k3s ctr images list \| grep vinaire`; убедитесь, что `image.pullPolicy` — `IfNotPresent`/`Never`, а тег в `kubectl describe pod` — тот, что вы импортировали. |
| `clusterrole "vinaire-viewer" not found` при привязке | Vinaire развёрнут без чарта (рукописный Deployment), поэтому роли не создавались. Установите через `helm upgrade --install` — чарт их создаёт. |
| После ввода токена вас возвращает на экран входа | (1) `cookie.secure` равен `true`, а вы на обычном HTTP — используйте HTTPS или `--set cookie.secure=false` для локальной пробы; (2) `authMode` не `token`; (3) токен истёк (`--duration`). |
| `pods is forbidden` / `403` везде | У учётной записи, токен которой вы вставили, нет прав. Проверьте `kubectl auth can-i … --as=system:serviceaccount:<ns>:<имя>` и убедитесь, что роль **viewer** привязана к *вашему* ServiceAccount, а не к собственному аккаунту Vinaire. |
| Установка из Marketplace: «you don't have the permissions this chart needs» | Привяжите `-installer` (вдобавок к `-viewer`). Помните, что `-admin` прав в кластере не даёт. |
| Ingress отвечает 404 / не разрешается | `ingress.className` должен совпадать с вашим контроллером (`kubectl get ingressclass`); хост должен разрешаться в него (hosts / DNS). |
| Helm пишет `values don't meet the specifications of the schema` | У значения неверный тип или недопустимый вариант (например, `authMode: tokens`); сообщение называет ключ и допустимые значения. |

## Безопасность

Прочтите [docs/security.ru.md](./docs/security.ru.md), прежде чем ставить Vinaire куда-либо
по-настоящему: краткая модель угроз, проверенные свойства (и как проверить их снова) и
оставшиеся риски. Вкратце:

- **Не выставляйте Vinaire напрямую в интернет.** Ходите к нему через VPN (WireGuard,
  NetBird, Tailscale). Если он обязан быть публичным — только за HTTPS с настоящим
  сертификатом (cert-manager).
- **Vinaire — единая точка с доступом ко всем зарегистрированным в нём кластерам**: относитесь
  к нему как к бастиону и держите `CLUSTER_STORE_KEY` вне репозитория и образа.
- Почти всё выполняется от имени вошедшего пользователя, поэтому решает RBAC самого кластера;
  роли в чарте (`viewer`, `installer`, `node-admin`, `rbac-admin`, `admin`) создаются без
  привязок — привязывайте их осознанно.
- Workflow `Security` (govulncheck, gosec, npm audit, gitleaks, Trivy, базовый прогон OWASP
  ZAP и браузерные проверки CSP) запускается на каждый pull request и раз в неделю.

## Структура репозитория

```text
.
├── frontend/     приложение Next.js
├── backend/      Go HTTP-сервер + клиент Kubernetes
├── charts/       Helm-чарт для установки на настоящий кластер
├── package.json  Корневой оркестратор (`npm run dev` запускает всё сразу)
├── AGENTS.md     Соглашения и архитектурные правила для ИИ-агентов
├── ROADMAP.md    Поэтапный план (от MVP до записи через GitOps)
└── CHANGELOG.md
```

## Лицензия

Лицензия [Apache License 2.0](./LICENSE) ([перевод на русский](./LICENSE.ru.md), справочный).
Сторонние компоненты (включая встроенные шрифты под SIL Open Font License) перечислены в
[NOTICE](./NOTICE).
