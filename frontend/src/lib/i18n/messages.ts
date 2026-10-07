import { getLang } from "./state";
import { tr } from "./translate";

// Error text that comes from the backend or from Kubernetes. The static messages
// are exact entries in the dictionary; the ones with a variable part are matched
// here. Anything unrecognised is shown as received — a server message is never
// dropped or guessed at.
const patterns: [RegExp, (m: RegExpMatchArray) => string][] = [
  [
    /^(.+?) is forbidden: User "(.+?)" cannot (\w+) resource "(.+?)" in API group "(.*?)" (?:at the cluster scope|in the namespace "(.+?)")\.?$/,
    (m) =>
      `Запрещено: пользователь «${m[2]}» не может выполнить «${m[3]}» над ресурсом «${m[4]}»` +
      (m[5] ? ` (группа API «${m[5]}»)` : "") +
      (m[6] ? `, namespace «${m[6]}»` : ", на уровне кластера"),
  ],
  [/^(.+?) "(.+?)" not found$/, (m) => `${m[1]} «${m[2]}» не найден`],
  [/^(.+?) "(.+?)" already exists$/, (m) => `${m[1]} «${m[2]}» уже существует`],
  [
    /^you don't have permission to read the logs of pod (\S+) \(get pods\/log\)$/,
    (m) => `нет права читать логи пода ${m[1]} (get pods/log)`,
  ],
  [/^unknown cluster "(.+)"$/, (m) => `неизвестный кластер «${m[1]}»`],
  [/^no resource "(.+)" in (.+)$/, (m) => `в ${m[2]} нет ресурса «${m[1]}»`],
  [
    /^this cluster doesn't serve (.+)$/,
    (m) => `этот кластер не обслуживает ${m[1]}`,
  ],
  [
    /^the operation timed out: (.+)$/,
    (m) => `время ожидания операции вышло: ${m[1]}`,
  ],
  [
    /^A Prometheus is already running \((.+?)\)\. Installing a second one would split your metrics — use it instead\.$/,
    (m) =>
      `Prometheus уже работает (${m[1]}). Второй разделит ваши метрики — используйте существующий.`,
  ],
  [
    /^A Loki is already running \((.+?)\)\. Installing a second one would split your logs — use it instead\.$/,
    (m) =>
      `Loki уже работает (${m[1]}). Второй разделит ваши логи — используйте существующий.`,
  ],
  [
    /^Storage class "(.+?)" doesn't exist in this cluster\.$/,
    (m) => `Storage class «${m[1]}» в этом кластере не существует.`,
  ],
  [
    /^Needs about 0\.2 CPU and 0\.5–1\.5 GiB of memory in total .*? The cluster has (\d+) CPU\(s\) and (\d+) GiB allocatable\.$/,
    (m) =>
      `Нужно примерно 0,2 CPU и 0,5–1,5 ГиБ памяти суммарно (Prometheus ≈ 0,1 CPU / 256 МиБ–1 ГиБ, kube-state-metrics и node-exporter ≈ 20m / 32 МиБ на каждой ноде). В кластере доступно CPU: ${m[1]}, памяти: ${m[2]} ГиБ.`,
  ],
  [
    /^Needs about 0\.5 CPU and 1 GiB of memory in total .*? The cluster has (\d+) CPU\(s\) and (\d+) GiB allocatable\.$/,
    (m) =>
      `Нужно примерно 0,5 CPU и 1 ГиБ памяти суммарно (Loki ≈ 0,1 CPU / 256 МиБ–1 ГиБ, плюс Alloy ≈ 50m / 128 МиБ на каждой ноде). В кластере доступно CPU: ${m[1]}, памяти: ${m[2]} ГиБ.`,
  ],
  [
    /^Metrics are kept for (\d+) days\. .*$/,
    (m) =>
      `Метрики хранятся дней: ${m[1]}. Ставятся Prometheus, kube-state-metrics и node-exporter — без Grafana и Alertmanager. Для них установите kube-prometheus-stack из Marketplace.`,
  ],
  [
    /^Logs are kept for (\d+) days; .*$/,
    (m) =>
      `Логи хранятся дней: ${m[1]}; это Loki в одном поде для небольших кластеров, а не распределённый.`,
  ],
  [
    /^a cluster named (.+) already exists$/,
    (m) => `кластер с именем ${m[1]} уже есть`,
  ],
];

// "invalid request body: <detail>" — the part before the first ": " is looked up,
// the detail (often a Go/Kubernetes error) is kept as is.
export function tm(message: string): string {
  if (getLang() !== "ru" || !message) return message;
  const whole = tr(message);
  if (whole !== message) return whole;
  for (const [re, fn] of patterns) {
    const m = message.match(re);
    if (m) return fn(m);
  }
  const i = message.indexOf(": ");
  if (i > 0) {
    const head = tr(message.slice(0, i));
    if (head !== message.slice(0, i)) return head + message.slice(i);
  }
  return message;
}
