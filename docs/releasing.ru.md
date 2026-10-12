# Выпуск версий

[English](./releasing.md)

Релиз — это один номер версии, который стоит в трёх местах и должен совпадать: `version` и `appVersion` в chart'е
(`charts/vinaire/Chart.yaml`), раздел `## [x.y.z]` в `CHANGELOG.md` и git-тег `vx.y.z`. Workflow **Release**
(`.github/workflows/release.yml`) проверяет, что они совпадают, собирает образ, публикует его и chart в реестре
GitHub, подписывает оба через cosign (без ключей), прикладывает SBOM и подтверждение происхождения и создаёт тег и
GitHub Release — всё за один запуск: ничего не забыть и не нужно пушить тег руками.

## Как сделать релиз

1. **Подготовка** — в отдельной ветке запустите скрипт с новой версией:

   ```bash
   git switch -c release/v1.0.1
   scripts/release-prepare.sh 1.0.1
   git commit -am "release: v1.0.1"
   git push -u origin release/v1.0.1
   ```

   Он ставит `version` и `appVersion` в chart'е и превращает `## [Unreleased]` в changelog'е в
   `## [1.0.1] - <сегодня>` (над ним остаётся новый пустой `[Unreleased]`). Пустой `[Unreleased]` он не принимает —
   выпускать нечего.
2. **Проверьте и смержите** pull request. Проверки (CI, Security) должны пройти; `main` защищена.
3. **Выпуск** — Actions → **Release** → *Run workflow*, ветка `main`, версия `1.0.1`; либо
   `gh workflow run release.yml --ref main -f version=1.0.1`.

Workflow не запустится с другой ветки, для версии, которая не равна версии chart'а, для версии без раздела в
changelog'е и для уже существующего тега (выпущенную версию не заменяют). Описание релиза — раздел changelog'а;
версия с суффиксом (`1.1.0-rc.1`) публикуется как предварительная.

Почему один workflow, а не «поставили тег — тег запустил релиз»: тег, созданный стандартным `GITHUB_TOKEN`, **не
запускает** другие workflow. Если тег создаёт сам workflow выпуска, не нужен ни отдельный токен, ни GitHub App.
Тег `v*`, отправленный руками, тоже работает и делает то же самое.

## Проверка выпуска

```bash
scripts/verify-release.sh 1.0.1             # подписи образа и chart'а, SBOM, chart рендерится
scripts/verify-release.sh 1.0.1 --install   # ... и ставит его во временный namespace
```

## Первый релиз (1.0.0) — чек-лист открытия репозитория

Уже сделано в репозитории: версия `1.0.0` везде, в changelog'е есть раздел `[1.0.0]`, workflow выпуска, скрипт
проверки, `SECURITY.md` и `CONTRIBUTING.md`, README, проверка всей истории на секреты (gitleaks: находок нет) и
путь Go-модуля, совпадающий с репозиторием (`github.com/f1nn17/vinaire`).

На GitHub, по порядку:

1. **Защитить `main`** — Settings → Rules → Rulesets → новый ruleset для ветки `main`: обязательный pull request,
   обязательные проверки (jobs *CI* и *Security*), запрет force-push и удаления.
2. **Разрешить workflow писать пакеты** — Settings → Actions → General → Workflow permissions: *Read and write*
   (сам job выпуска тоже запрашивает `packages: write`).
3. **Сделать репозиторий публичным** — Settings → General → Danger zone → *Change visibility*. Включите *Private
   vulnerability reporting* (Settings → Code security) — на него ссылается `SECURITY.md`.
4. **Выпустить** — запустить workflow, как выше, с версией `1.0.0`. (Первый запуск создаст тег `v1.0.0`.)
5. **Сделать пакеты публичными** — пакеты в ghcr.io сначала приватные: ваш профиль → Packages → `vinaire` и
   `charts/vinaire` → Package settings → *Change visibility* → Public. Пока этого нет, никто другой их не скачает.
6. **Проверить с чистой машины** — `scripts/verify-release.sh 1.0.0 --install` (или быстрый старт из README на
   одноразовом кластере).
7. По желанию: описание репозитория и темы (`kubernetes`, `k3s`, `dashboard`, `helm`), закрепить релиз.
