import { stringify as dump } from "yaml";

// Kubernetes reads YAML 1.1, where a bare yes / no / on / off is a boolean: this makes strings that
// look like one come out quoted ("--appendonly yes" must stay a string).
const stringify = (v: unknown): string => dump(v, { version: "1.1" });

// Roadmap Phase 67: one instance of a program from its publisher's own Docker image — for when there
// is no maintained chart, or when a chart is more than is needed. This file only *describes* what to
// create (a Secret, a Service, a StatefulSet) and builds the YAML; applying it is the generic YAML
// create of Phase 25, as the signed-in user, so nothing here is a write path of its own. The images
// named by the presets are the publishers' official ones; a free-form image is typed by the user.
//
// Strings that reach the screen go through `T` (a marker the dictionary check can find) and are
// translated where they are drawn.
export const T = (s: string): string => s;

export type PresetField = {
  id: string;
  label: string;
  hint: string;
  default: string;
  // what a value must look like (it ends up in YAML, so it is always checked)
  pattern: RegExp;
  // an empty value is allowed
  optional?: boolean;
  multiline?: boolean;
};

// A value, a key of the instance's own Secret (`secret`), or a key of a Secret that already exists
// (`from`, for the password of a database that runs elsewhere).
export type EnvVar = {
  name: string;
  value?: string;
  secret?: string;
  from?: { name: string; key: string };
};

export type Preset = {
  id: string;
  title: string;
  summary: string;
  // the repository part of the image; "" means the user types it
  image: string;
  tagHint: string;
  defaultTag: string;
  ports: { name: string; port: number }[];
  dataPath?: string;
  fields: PresetField[];
  // Secret keys that get a generated random password
  secrets: string[];
  env: (v: Record<string, string>) => EnvVar[];
  args?: (v: Record<string, string>) => string[];
  probe: { tcp: number } | { exec: string[] };
  resources: { cpu: string; memory: string; memoryLimit: string };
  connect: (
    v: Record<string, string>,
    host: string,
    secret: string,
  ) => [string, string][];
  note?: string;
  // the group that owns the volume, for images that run as a non-root user
  fsGroup?: number;
  free?: boolean;
};

const word = /^[a-z_][a-z0-9_]{0,30}$/;

const userField = (def = "app"): PresetField => ({
  id: "user",
  label: T("User name"),
  hint: T(
    "The account the application will log in with (letters, digits, underscore).",
  ),
  default: def,
  pattern: word,
});
const dbField = (): PresetField => ({
  id: "database",
  label: T("Database name"),
  hint: T("The database created on first start."),
  default: "app",
  pattern: word,
});

const connectSql =
  (port: number) => (v: Record<string, string>, host: string, secret: string) =>
    [
      [T("Address"), `${host}:${port}`],
      [T("User"), v.user],
      [T("Database"), v.database],
      [T("Password"), `${T("Secret")} ${secret}, ${T("key")} password`],
    ] as [string, string][];

export const presets: Preset[] = [
  {
    id: "postgres",
    title: "PostgreSQL",
    summary: T("A relational database. Official image `postgres`."),
    image: "postgres",
    tagHint: T(
      "A major version, e.g. 17. Pin it: changing it later means a data migration.",
    ),
    defaultTag: "17",
    ports: [{ name: "postgres", port: 5432 }],
    dataPath: "/var/lib/postgresql/data",
    fields: [userField(), dbField()],
    secrets: ["password"],
    env: (v) => [
      { name: "POSTGRES_USER", value: v.user },
      { name: "POSTGRES_DB", value: v.database },
      { name: "POSTGRES_PASSWORD", secret: "password" },
      // a sub-directory, because a fresh volume has a lost+found in its root
      { name: "PGDATA", value: "/var/lib/postgresql/data/pgdata" },
    ],
    probe: {
      exec: ["sh", "-c", 'pg_isready -U "$POSTGRES_USER" -d "$POSTGRES_DB"'],
    },
    resources: { cpu: "100m", memory: "256Mi", memoryLimit: "1Gi" },
    connect: connectSql(5432),
    note: T(
      "One instance, no replicas and no backups. For data you can't lose, use the CloudNativePG operator.",
    ),
  },
  {
    id: "mysql",
    title: "MySQL",
    summary: T("A relational database. Official image `mysql`."),
    image: "mysql",
    tagHint: T("A version, e.g. 8.4 (long-term support)."),
    defaultTag: "8.4",
    ports: [{ name: "mysql", port: 3306 }],
    dataPath: "/var/lib/mysql",
    fields: [userField(), dbField()],
    secrets: ["root-password", "password"],
    env: (v) => [
      { name: "MYSQL_ROOT_PASSWORD", secret: "root-password" },
      { name: "MYSQL_DATABASE", value: v.database },
      { name: "MYSQL_USER", value: v.user },
      { name: "MYSQL_PASSWORD", secret: "password" },
    ],
    probe: {
      exec: [
        "sh",
        "-c",
        'mysqladmin ping -h 127.0.0.1 -uroot -p"$MYSQL_ROOT_PASSWORD"',
      ],
    },
    resources: { cpu: "100m", memory: "512Mi", memoryLimit: "1Gi" },
    connect: connectSql(3306),
    note: T(
      "One instance, no replicas and no backups. The administrator's password is in the key root-password.",
    ),
  },
  {
    id: "mariadb",
    title: "MariaDB",
    summary: T(
      "A relational database (MySQL's fork). Official image `mariadb`.",
    ),
    image: "mariadb",
    tagHint: T("A version, e.g. 11.4 (long-term support)."),
    defaultTag: "11.4",
    ports: [{ name: "mysql", port: 3306 }],
    dataPath: "/var/lib/mysql",
    fields: [userField(), dbField()],
    secrets: ["root-password", "password"],
    env: (v) => [
      { name: "MARIADB_ROOT_PASSWORD", secret: "root-password" },
      { name: "MARIADB_DATABASE", value: v.database },
      { name: "MARIADB_USER", value: v.user },
      { name: "MARIADB_PASSWORD", secret: "password" },
    ],
    probe: { tcp: 3306 },
    resources: { cpu: "100m", memory: "512Mi", memoryLimit: "1Gi" },
    connect: connectSql(3306),
    note: T(
      "One instance, no replicas and no backups. The administrator's password is in the key root-password.",
    ),
  },
  {
    id: "redis",
    title: "Redis",
    summary: T("An in-memory store and cache. Official image `redis`."),
    image: "redis",
    tagHint: T(
      "A version, e.g. 7.4. (From 8 the licence changed; Valkey is the free fork.)",
    ),
    defaultTag: "7.4",
    ports: [{ name: "redis", port: 6379 }],
    dataPath: "/data",
    fields: [],
    secrets: ["password"],
    env: () => [{ name: "REDIS_PASSWORD", secret: "password" }],
    args: () => [
      "redis-server",
      "--requirepass",
      "$(REDIS_PASSWORD)",
      "--appendonly",
      "yes",
    ],
    probe: { tcp: 6379 },
    resources: { cpu: "50m", memory: "128Mi", memoryLimit: "512Mi" },
    connect: (_v, host, secret) => [
      [T("Address"), `${host}:6379`],
      [T("Password"), `${T("Secret")} ${secret}, ${T("key")} password`],
    ],
    note: T(
      "One instance with the append-only file on. No user name: Redis asks for the password only.",
    ),
  },
  {
    id: "valkey",
    title: "Valkey",
    summary: T(
      "A free fork of Redis, compatible with it. Official image `valkey/valkey`.",
    ),
    image: "valkey/valkey",
    tagHint: T("A version, e.g. 8."),
    defaultTag: "8",
    ports: [{ name: "redis", port: 6379 }],
    dataPath: "/data",
    fields: [],
    secrets: ["password"],
    env: () => [{ name: "VALKEY_PASSWORD", secret: "password" }],
    args: () => [
      "valkey-server",
      "--requirepass",
      "$(VALKEY_PASSWORD)",
      "--appendonly",
      "yes",
    ],
    probe: { tcp: 6379 },
    resources: { cpu: "50m", memory: "128Mi", memoryLimit: "512Mi" },
    connect: (_v, host, secret) => [
      [T("Address"), `${host}:6379`],
      [T("Password"), `${T("Secret")} ${secret}, ${T("key")} password`],
    ],
    note: T(
      "Speaks the Redis protocol: use it wherever an application asks for Redis.",
    ),
  },
  {
    id: "rabbitmq",
    title: "RabbitMQ",
    summary: T(
      "A message broker. Official image `rabbitmq` (with the management page).",
    ),
    image: "rabbitmq",
    tagHint: T("A version with the management page, e.g. 4-management."),
    defaultTag: "4-management",
    ports: [
      { name: "amqp", port: 5672 },
      { name: "management", port: 15672 },
    ],
    dataPath: "/var/lib/rabbitmq",
    fields: [userField()],
    secrets: ["password"],
    env: (v) => [
      { name: "RABBITMQ_DEFAULT_USER", value: v.user },
      { name: "RABBITMQ_DEFAULT_PASS", secret: "password" },
    ],
    probe: { tcp: 5672 },
    resources: { cpu: "100m", memory: "256Mi", memoryLimit: "1Gi" },
    connect: (v, host, secret) => [
      [T("Address"), `${host}:5672`],
      [T("User"), v.user],
      [T("Password"), `${T("Secret")} ${secret}, ${T("key")} password`],
      [
        T("Management page"),
        `${host}:15672 (${T("use Forward port on the pod")})`,
      ],
    ],
    note: T(
      "One node. The management page is not published outside; open it with Forward port on the pod.",
    ),
  },
  {
    id: "mongo",
    title: "MongoDB",
    summary: T("A document database. Official image `mongo`."),
    image: "mongo",
    tagHint: T("A version, e.g. 8."),
    defaultTag: "8",
    ports: [{ name: "mongo", port: 27017 }],
    dataPath: "/data/db",
    fields: [userField("root")],
    secrets: ["password"],
    env: (v) => [
      { name: "MONGO_INITDB_ROOT_USERNAME", value: v.user },
      { name: "MONGO_INITDB_ROOT_PASSWORD", secret: "password" },
    ],
    probe: { tcp: 27017 },
    resources: { cpu: "100m", memory: "512Mi", memoryLimit: "1Gi" },
    connect: (v, host, secret) => [
      [T("Address"), `${host}:27017`],
      [T("User"), v.user],
      [T("Password"), `${T("Secret")} ${secret}, ${T("key")} password`],
    ],
    note: T(
      "One instance, no replica set and no backups. The account is the administrator of the whole server.",
    ),
  },
  {
    id: "keycloak",
    title: "Keycloak",
    summary: T(
      "Sign-in and identity server. Image `quay.io/keycloak/keycloak`, connected to a PostgreSQL that already exists.",
    ),
    image: "quay.io/keycloak/keycloak",
    tagHint: T("A version, e.g. 26.2.5. Pick one from the list."),
    defaultTag: "",
    ports: [{ name: "http", port: 8080 }],
    fields: [
      {
        id: "dbHost",
        label: T("Database address"),
        hint: T(
          "The Service of your PostgreSQL, e.g. cluster-rw (CloudNativePG). Same namespace, or a full name like cluster-rw.cluster.svc.",
        ),
        default: "",
        pattern: /^[a-z0-9]([-a-z0-9.]{0,250}[a-z0-9])?$/,
      },
      {
        id: "dbPort",
        label: T("Database port"),
        hint: T("PostgreSQL's usual port is 5432."),
        default: "5432",
        pattern: /^[0-9]{1,5}$/,
      },
      {
        id: "dbName",
        label: T("Database name"),
        hint: T("An existing, empty database for Keycloak."),
        default: "app",
        pattern: word,
      },
      {
        id: "dbUser",
        label: T("Database user"),
        hint: T("The account that owns that database."),
        default: "app",
        pattern: word,
      },
      {
        id: "dbSecret",
        label: T("Secret with the database password"),
        hint: T(
          "An existing Secret in this namespace (CloudNativePG makes <name>-app). Only its name and key are used; the password is never read by Vinaire.",
        ),
        default: "",
        pattern: /^[a-z0-9]([-a-z0-9.]{0,250}[a-z0-9])?$/,
      },
      {
        id: "dbSecretKey",
        label: T("Key in that Secret"),
        hint: T("Usually password."),
        default: "password",
        pattern: /^[-._a-zA-Z0-9]{1,253}$/,
      },
    ],
    secrets: ["admin-password"],
    env: (v) => [
      { name: "KC_DB", value: "postgres" },
      { name: "KC_DB_URL_HOST", value: v.dbHost },
      { name: "KC_DB_URL_PORT", value: v.dbPort },
      { name: "KC_DB_URL_DATABASE", value: v.dbName },
      { name: "KC_DB_USERNAME", value: v.dbUser },
      {
        name: "KC_DB_PASSWORD",
        from: { name: v.dbSecret, key: v.dbSecretKey },
      },
      { name: "KC_BOOTSTRAP_ADMIN_USERNAME", value: "admin" },
      { name: "KC_BOOTSTRAP_ADMIN_PASSWORD", secret: "admin-password" },
      // behind an Ingress or a proxy: plain HTTP inside, the proxy's headers say what the outside saw
      { name: "KC_HTTP_ENABLED", value: "true" },
      { name: "KC_HOSTNAME_STRICT", value: "false" },
      { name: "KC_PROXY_HEADERS", value: "xforwarded" },
    ],
    args: () => ["start"],
    probe: { tcp: 8080 },
    resources: { cpu: "250m", memory: "768Mi", memoryLimit: "1536Mi" },
    connect: (_v, host, secret) => [
      [T("Address"), `${host}:8080`],
      [T("Administrator"), "admin"],
      [T("Password"), `${T("Secret")} ${secret}, ${T("key")} admin-password`],
    ],
    note: T(
      "Keeps nothing on its own volume: everything is in the database. Put an Ingress in front of it for real use; the first start takes a minute while it builds its tables.",
    ),
  },
  {
    id: "nginx",
    title: "nginx",
    summary: T(
      "A web server. Official image `nginx` (shows its welcome page).",
    ),
    image: "nginx",
    tagHint: T("A version, e.g. 1.27."),
    defaultTag: "1.27",
    ports: [{ name: "http", port: 80 }],
    fields: [],
    secrets: [],
    env: () => [],
    probe: { tcp: 80 },
    resources: { cpu: "20m", memory: "32Mi", memoryLimit: "128Mi" },
    connect: (_v, host) => [[T("Address"), `${host}:80`]],
    note: T(
      "Serves the default page. To serve your own content, change the object afterwards on its YAML tab.",
    ),
  },
  {
    id: "memcached",
    title: "Memcached",
    summary: T("A simple in-memory cache. Official image `memcached`."),
    image: "memcached",
    tagHint: T("A version, e.g. 1.6."),
    defaultTag: "1.6",
    ports: [{ name: "memcached", port: 11211 }],
    fields: [],
    secrets: [],
    env: () => [],
    args: () => ["-m", "64"],
    probe: { tcp: 11211 },
    resources: { cpu: "20m", memory: "80Mi", memoryLimit: "128Mi" },
    connect: (_v, host) => [[T("Address"), `${host}:11211`]],
    note: T(
      "No password and nothing is kept: use it only for a cache that other pods in the cluster reach.",
    ),
  },
  {
    id: "nats",
    title: "NATS",
    summary: T(
      "A fast message bus with JetStream persistence. Official image `nats`.",
    ),
    image: "nats",
    tagHint: T("A version, e.g. 2.10."),
    defaultTag: "2",
    ports: [
      { name: "client", port: 4222 },
      { name: "monitor", port: 8222 },
    ],
    dataPath: "/data",
    fields: [],
    secrets: [],
    env: () => [],
    args: () => ["-js", "-sd", "/data", "-m", "8222"],
    probe: { tcp: 4222 },
    resources: { cpu: "50m", memory: "64Mi", memoryLimit: "256Mi" },
    connect: (_v, host) => [[T("Address"), `nats://${host}:4222`]],
    note: T("One server, no password: for pods inside the cluster."),
  },
  {
    id: "minio",
    title: "MinIO",
    summary: T("S3-compatible object storage. Image `quay.io/minio/minio`."),
    image: "quay.io/minio/minio",
    tagHint: T(
      "A release tag like RELEASE.2025-…; pick the newest from the list.",
    ),
    defaultTag: "",
    ports: [
      { name: "api", port: 9000 },
      { name: "console", port: 9001 },
    ],
    dataPath: "/data",
    fields: [
      {
        id: "user",
        label: T("Access key (administrator)"),
        hint: T("The administrator's name; at least 3 characters."),
        default: "admin",
        pattern: /^[a-z][a-z0-9_]{2,30}$/,
      },
    ],
    secrets: ["password"],
    env: (v) => [
      { name: "MINIO_ROOT_USER", value: v.user },
      { name: "MINIO_ROOT_PASSWORD", secret: "password" },
    ],
    args: () => ["server", "/data", "--console-address", ":9001"],
    probe: { tcp: 9000 },
    resources: { cpu: "100m", memory: "256Mi", memoryLimit: "1Gi" },
    connect: (v, host, secret) => [
      [T("S3 address"), `http://${host}:9000`],
      [T("Console"), `${host}:9001 (${T("use Forward port on the pod")})`],
      [T("Access key"), v.user],
      [T("Password"), `${T("Secret")} ${secret}, ${T("key")} password`],
    ],
    note: T("One server on one volume. The console is not published outside."),
  },
  {
    id: "grafana",
    title: "Grafana",
    summary: T("Dashboards. Official image `grafana/grafana`."),
    image: "grafana/grafana",
    tagHint: T("A version, e.g. 11.6.1. Pick one from the list."),
    defaultTag: "",
    ports: [{ name: "http", port: 3000 }],
    dataPath: "/var/lib/grafana",
    fsGroup: 472,
    fields: [
      {
        id: "user",
        label: T("Administrator name"),
        hint: T("The name to sign in with."),
        default: "admin",
        pattern: /^[a-z][a-z0-9_]{2,30}$/,
      },
    ],
    secrets: ["password"],
    env: (v) => [
      { name: "GF_SECURITY_ADMIN_USER", value: v.user },
      { name: "GF_SECURITY_ADMIN_PASSWORD", secret: "password" },
    ],
    probe: { tcp: 3000 },
    resources: { cpu: "100m", memory: "256Mi", memoryLimit: "512Mi" },
    connect: (v, host, secret) => [
      [T("Address"), `${host}:3000`],
      [T("User"), v.user],
      [T("Password"), `${T("Secret")} ${secret}, ${T("key")} password`],
    ],
    note: T("Add a data source (Prometheus, Loki) after the first sign-in."),
  },
  {
    id: "adminer",
    title: "Adminer",
    summary: T("A web page to look into a database. Official image `adminer`."),
    image: "adminer",
    tagHint: T("A version, e.g. 5."),
    defaultTag: "5",
    ports: [{ name: "http", port: 8080 }],
    fields: [],
    secrets: [],
    env: () => [],
    probe: { tcp: 8080 },
    resources: { cpu: "20m", memory: "64Mi", memoryLimit: "256Mi" },
    connect: (_v, host) => [
      [T("Address"), `${host}:8080 (${T("use Forward port on the pod")})`],
    ],
    note: T(
      "It has no sign-in of its own: don't publish it outside; open it with Forward port on the pod and type the database's address and password there.",
    ),
  },
  {
    id: "uptime-kuma",
    title: "Uptime Kuma",
    summary: T(
      "A status page and uptime monitor. Image `louislam/uptime-kuma`.",
    ),
    image: "louislam/uptime-kuma",
    tagHint: T("A version, e.g. 1."),
    defaultTag: "1",
    ports: [{ name: "http", port: 3001 }],
    dataPath: "/app/data",
    fields: [],
    secrets: [],
    env: () => [],
    probe: { tcp: 3001 },
    resources: { cpu: "50m", memory: "128Mi", memoryLimit: "512Mi" },
    connect: (_v, host) => [[T("Address"), `${host}:3001`]],
    note: T(
      "The administrator account is created by whoever opens the page first: open it right after the install.",
    ),
  },
  {
    id: "custom",
    title: T("Any image"),
    summary: T(
      "Any image from a registry, with the settings you type. Nothing is guessed for you.",
    ),
    image: "",
    tagHint: T("A tag. Avoid latest: it changes under you."),
    defaultTag: "",
    ports: [],
    fields: [
      {
        id: "image",
        label: T("Image"),
        hint: T(
          "Without the tag, e.g. nginx or ghcr.io/owner/app. Take images from their publisher.",
        ),
        default: "",
        pattern:
          /^[a-z0-9]+(?:[._-][a-z0-9]+)*(?::[0-9]{1,5})?(?:\/[a-z0-9]+(?:[._-][a-z0-9]+)*)*$/,
      },
      {
        id: "port",
        label: T("Port"),
        hint: T(
          "The port the program listens on; the Service will publish it inside the cluster. Empty: none.",
        ),
        default: "",
        pattern: /^[0-9]{1,5}$/,
        optional: true,
      },
      {
        id: "mountPath",
        label: T("Keep data at"),
        hint: T(
          "A directory inside the container to keep on a volume (e.g. /data). Empty: nothing is kept after a restart.",
        ),
        default: "",
        pattern: /^\/[A-Za-z0-9._\/-]{0,200}$/,
        optional: true,
      },
      {
        id: "env",
        label: T("Environment variables"),
        hint: T(
          "One NAME=value per line. Don't put passwords here: they would be visible in the object.",
        ),
        default: "",
        pattern: /^[A-Za-z_][A-Za-z0-9_]*=[^\n\r]*$/,
        optional: true,
        multiline: true,
      },
    ],
    secrets: [],
    env: (v) =>
      (v.env ?? "")
        .split("\n")
        .map((l) => l.trim())
        .filter(Boolean)
        .map((l) => ({
          name: l.slice(0, l.indexOf("=")),
          value: l.slice(l.indexOf("=") + 1),
        })),
    probe: { tcp: 0 },
    resources: { cpu: "50m", memory: "128Mi", memoryLimit: "512Mi" },
    connect: (v, host) => (v.port ? [[T("Address"), `${host}:${v.port}`]] : []),
    free: true,
  },
];

export type Form = {
  preset: Preset;
  name: string;
  namespace: string;
  tag: string;
  values: Record<string, string>;
  persistent: boolean;
  size: string;
  storageClass: string;
  cpu: string;
  memory: string;
  memoryLimit: string;
};

const dns = /^[a-z0-9]([-a-z0-9]{0,38}[a-z0-9])?$/;
const nsName = /^[a-z0-9]([-a-z0-9]{0,61}[a-z0-9])?$/;
const tagRe = /^[A-Za-z0-9_][A-Za-z0-9._-]{0,127}$/;
const sizeRe = /^[1-9][0-9]{0,5}(Mi|Gi|Ti)$/;
const cpuRe = /^([0-9]{1,5}m|[0-9]{1,3}(\.[0-9]{1,3})?)$/;
const memRe = /^[1-9][0-9]{0,6}(Mi|Gi)$/;
const classRe = /^[a-z0-9]([-a-z0-9.]{0,251}[a-z0-9])?$/;

export function newForm(preset: Preset): Form {
  return {
    preset,
    name: preset.id === "custom" ? "" : preset.id,
    namespace: "default",
    tag: preset.defaultTag,
    values: Object.fromEntries(preset.fields.map((f) => [f.id, f.default])),
    persistent: true,
    size: "8Gi",
    storageClass: "",
    cpu: preset.resources.cpu,
    memory: preset.resources.memory,
    memoryLimit: preset.resources.memoryLimit,
  };
}

// What is wrong with the form, by field id — empty when it can be applied.
export function validate(f: Form): Record<string, string> {
  const e: Record<string, string> = {};
  if (!dns.test(f.name))
    e.name = T("Lower-case letters, digits and dashes, up to 40.");
  if (!nsName.test(f.namespace)) e.namespace = T("A valid namespace name.");
  if (!tagRe.test(f.tag)) e.tag = T("A tag, e.g. 17 or 1.2.3.");
  for (const fld of f.preset.fields) {
    const v = f.values[fld.id] ?? "";
    if (!v && fld.optional) continue;
    const lines = fld.multiline ? v.split("\n").filter((l) => l.trim()) : [v];
    if (!lines.every((l) => fld.pattern.test(l.trim())))
      e[fld.id] = T("Not a valid value.");
  }
  if (f.preset.free && f.values.port && Number(f.values.port) > 65535)
    e.port = T("Not a valid value.");
  if (
    (f.persistent && f.preset.dataPath !== undefined) ||
    (f.preset.free && f.values.mountPath)
  ) {
    if (!sizeRe.test(f.size)) e.size = T("A size like 8Gi.");
    if (f.storageClass && !classRe.test(f.storageClass))
      e.storageClass = T("A valid class name.");
  }
  if (!cpuRe.test(f.cpu)) e.cpu = T("Like 100m or 0.5.");
  if (!memRe.test(f.memory)) e.memory = T("Like 256Mi.");
  if (!memRe.test(f.memoryLimit)) e.memoryLimit = T("Like 1Gi.");
  return e;
}

export function imageOf(f: Form): string {
  const repo = f.preset.free ? f.values.image : f.preset.image;
  return `${repo}:${f.tag}`;
}

function labels(f: Form): Record<string, string> {
  return {
    "app.kubernetes.io/name": f.preset.id,
    "app.kubernetes.io/instance": f.name,
    "app.kubernetes.io/managed-by": "vinaire",
  };
}

export type Doc = {
  kind: "Secret" | "Service" | "StatefulSet";
  name: string;
  yaml: string;
};

// The objects, in the order they are applied. `passwords` holds the generated values, or a mask
// for the preview on screen.
// `ownerUid` (known once the StatefulSet exists) makes the Secret and the Service belong to it, so
// deleting the StatefulSet removes them too — there is no other way to delete a Service from here.
export function build(
  f: Form,
  passwords: Record<string, string>,
  ownerUid?: string,
): Doc[] {
  const owner = ownerUid
    ? {
        ownerReferences: [
          {
            apiVersion: "apps/v1",
            kind: "StatefulSet",
            name: f.name,
            uid: ownerUid,
          },
        ],
      }
    : {};
  const p = f.preset;
  const lab = labels(f);
  const selector = {
    "app.kubernetes.io/name": lab["app.kubernetes.io/name"],
    "app.kubernetes.io/instance": f.name,
  };
  const docs: Doc[] = [];
  if (p.secrets.length > 0) {
    docs.push({
      kind: "Secret",
      name: f.name,
      yaml: stringify({
        apiVersion: "v1",
        kind: "Secret",
        metadata: {
          name: f.name,
          namespace: f.namespace,
          labels: lab,
          ...owner,
        },
        type: "Opaque",
        stringData: Object.fromEntries(
          p.secrets.map((k) => [k, passwords[k] ?? ""]),
        ),
      }),
    });
  }
  const ports = p.free
    ? f.values.port
      ? [{ name: "main", port: Number(f.values.port) }]
      : []
    : p.ports;
  if (ports.length > 0) {
    docs.push({
      kind: "Service",
      name: f.name,
      yaml: stringify({
        apiVersion: "v1",
        kind: "Service",
        metadata: {
          name: f.name,
          namespace: f.namespace,
          labels: lab,
          ...owner,
        },
        spec: {
          type: "ClusterIP",
          selector,
          ports: ports.map((x) => ({
            name: x.name,
            port: x.port,
            targetPort: x.port,
          })),
        },
      }),
    });
  }
  const mountPath = p.free ? f.values.mountPath : p.dataPath;
  const keep = !!mountPath && (p.free ? true : f.persistent);
  const env = p.env(f.values).map((e) =>
    e.secret || e.from
      ? {
          name: e.name,
          valueFrom: {
            secretKeyRef: e.from ?? { name: f.name, key: e.secret as string },
          },
        }
      : { name: e.name, value: e.value ?? "" },
  );
  const probe =
    !("tcp" in p.probe) || p.probe.tcp > 0
      ? "tcp" in p.probe
        ? {
            tcpSocket: { port: p.probe.tcp },
            initialDelaySeconds: 10,
            periodSeconds: 10,
          }
        : {
            exec: { command: p.probe.exec },
            initialDelaySeconds: 15,
            periodSeconds: 10,
          }
      : p.free && ports.length > 0
        ? {
            tcpSocket: { port: ports[0].port },
            initialDelaySeconds: 10,
            periodSeconds: 10,
          }
        : undefined;
  const container: Record<string, unknown> = {
    name: "app",
    image: imageOf(f),
    ...(p.args ? { args: p.args(f.values) } : {}),
    ...(ports.length
      ? { ports: ports.map((x) => ({ name: x.name, containerPort: x.port })) }
      : {}),
    ...(env.length ? { env } : {}),
    resources: {
      requests: { cpu: f.cpu, memory: f.memory },
      limits: { memory: f.memoryLimit },
    },
    ...(probe ? { readinessProbe: probe } : {}),
    ...(keep ? { volumeMounts: [{ name: "data", mountPath }] } : {}),
  };
  docs.push({
    kind: "StatefulSet",
    name: f.name,
    yaml: stringify({
      apiVersion: "apps/v1",
      kind: "StatefulSet",
      metadata: { name: f.name, namespace: f.namespace, labels: lab },
      spec: {
        serviceName: f.name,
        replicas: 1,
        selector: { matchLabels: selector },
        template: {
          metadata: { labels: lab },
          spec: {
            containers: [container],
            ...(p.fsGroup ? { securityContext: { fsGroup: p.fsGroup } } : {}),
          },
        },
        ...(keep
          ? {
              volumeClaimTemplates: [
                {
                  metadata: { name: "data" },
                  spec: {
                    accessModes: ["ReadWriteOnce"],
                    resources: { requests: { storage: f.size } },
                    ...(f.storageClass
                      ? { storageClassName: f.storageClass }
                      : {}),
                  },
                },
              ],
            }
          : {}),
      },
    }),
  });
  return docs;
}
