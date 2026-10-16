import type { Dashboard } from "./model";

// The built-in dashboards, grouped. Cluster, Nodes and Workloads read what a Prometheus with
// kube-state-metrics, cAdvisor and the node-exporter has (the Metrics stack page installs it). The
// database ones need that database's exporter (PostgreSQL, Redis) or its own metrics endpoint (MinIO,
// Keycloak) to be scraped — each says so when its series are missing. Metric names follow the
// exporters' documentation; a panel whose series a particular version doesn't have just shows
// "no data" and the others still draw.

const PODS = `container!="",container!="POD"`;

export const dashboardGroups = [
  "Cluster",
  "Nodes",
  "Workloads",
  "Databases",
  "Storage & identity",
] as const;

export const builtinDashboards: Dashboard[] = [
  {
    id: "cluster-overview",
    group: "Cluster",
    title: "Cluster overview",
    description: "What the whole cluster is doing right now.",
    requires: {
      metric: "kube_pod_info",
      hint: "Needs kube-state-metrics — install the metrics stack (Metrics stack page).",
    },
    panels: [
      { title: "Nodes", kind: "stat", unit: "short", queries: [{ expr: "count(kube_node_info)" }] },
      {
        title: "Pods running",
        kind: "stat",
        unit: "short",
        queries: [{ expr: `sum(kube_pod_status_phase{phase="Running"})` }],
      },
      {
        title: "Pods not running",
        kind: "stat",
        unit: "short",
        queries: [{ expr: `sum(kube_pod_status_phase{phase=~"Pending|Failed|Unknown"})` }],
        help: "Pending, failed or unknown.",
      },
      {
        title: "Container restarts (last hour)",
        kind: "stat",
        unit: "short",
        queries: [{ expr: "sum(increase(kube_pod_container_status_restarts_total[1h]))" }],
      },
      {
        title: "CPU used (cores)",
        kind: "line",
        unit: "short",
        queries: [{ expr: `sum(rate(container_cpu_usage_seconds_total{${PODS}}[5m]))`, legend: "used" }],
      },
      {
        title: "Memory used",
        kind: "line",
        unit: "bytes",
        queries: [{ expr: `sum(container_memory_working_set_bytes{${PODS}})`, legend: "working set" }],
      },
      {
        title: "Network",
        kind: "line",
        unit: "bytesps",
        queries: [
          { expr: "sum(rate(container_network_receive_bytes_total[5m]))", legend: "received" },
          { expr: "sum(rate(container_network_transmit_bytes_total[5m]))", legend: "sent" },
        ],
      },
      {
        title: "Pods by phase",
        kind: "line",
        unit: "short",
        queries: [{ expr: "sum by (phase) (kube_pod_status_phase)", legend: "{phase}" }],
      },
    ],
  },
  {
    id: "nodes",
    group: "Nodes",
    title: "Nodes",
    description: "CPU, memory, disk and network of one node or all of them.",
    requires: {
      metric: "node_uname_info",
      hint: "Needs the node-exporter — it comes with the metrics stack (Metrics stack page).",
    },
    variables: [{ name: "instance", label: "Node", metric: "node_uname_info", by: "instance" }],
    panels: [
      {
        title: "CPU busy",
        kind: "line",
        unit: "percentunit",
        queries: [
          {
            expr: `1 - avg by (instance) (rate(node_cpu_seconds_total{mode="idle",instance=~"$instance"}[5m]))`,
            legend: "{instance}",
          },
        ],
      },
      {
        title: "Memory used",
        kind: "line",
        unit: "percentunit",
        queries: [
          {
            expr: `1 - node_memory_MemAvailable_bytes{instance=~"$instance"} / node_memory_MemTotal_bytes{instance=~"$instance"}`,
            legend: "{instance}",
          },
        ],
      },
      {
        title: "Load average (1 min)",
        kind: "line",
        unit: "short",
        queries: [{ expr: `node_load1{instance=~"$instance"}`, legend: "{instance}" }],
      },
      {
        title: "Disk used (root)",
        kind: "line",
        unit: "percentunit",
        queries: [
          {
            expr: `1 - node_filesystem_avail_bytes{mountpoint="/",fstype!~"tmpfs|overlay|squashfs|ramfs",instance=~"$instance"} / node_filesystem_size_bytes{mountpoint="/",fstype!~"tmpfs|overlay|squashfs|ramfs",instance=~"$instance"}`,
            legend: "{instance} {device}",
          },
        ],
      },
      {
        title: "Disk read / write",
        kind: "line",
        unit: "bytesps",
        queries: [
          { expr: `sum by (instance) (rate(node_disk_read_bytes_total{instance=~"$instance"}[5m]))`, legend: "read {instance}" },
          { expr: `sum by (instance) (rate(node_disk_written_bytes_total{instance=~"$instance"}[5m]))`, legend: "write {instance}" },
        ],
      },
      {
        title: "Network",
        kind: "line",
        unit: "bytesps",
        queries: [
          {
            expr: `sum by (instance) (rate(node_network_receive_bytes_total{device!~"lo|veth.*|cni.*|flannel.*|docker.*",instance=~"$instance"}[5m]))`,
            legend: "in {instance}",
          },
          {
            expr: `sum by (instance) (rate(node_network_transmit_bytes_total{device!~"lo|veth.*|cni.*|flannel.*|docker.*",instance=~"$instance"}[5m]))`,
            legend: "out {instance}",
          },
        ],
      },
    ],
  },
  {
    id: "workloads",
    group: "Workloads",
    title: "Workloads by namespace",
    description: "The heaviest pods of one namespace or of all of them.",
    requires: {
      metric: "kube_pod_info",
      hint: "Needs kube-state-metrics and cAdvisor — install the metrics stack (Metrics stack page).",
    },
    variables: [{ name: "namespace", label: "Namespace", metric: "kube_pod_info", by: "namespace" }],
    panels: [
      {
        title: "CPU by pod (top 10)",
        kind: "line",
        unit: "short",
        queries: [
          {
            expr: `topk(10, sum by (pod) (rate(container_cpu_usage_seconds_total{namespace=~"$namespace",${PODS}}[5m])))`,
            legend: "{pod}",
          },
        ],
      },
      {
        title: "Memory by pod (top 10)",
        kind: "line",
        unit: "bytes",
        queries: [
          {
            expr: `topk(10, sum by (pod) (container_memory_working_set_bytes{namespace=~"$namespace",${PODS}}))`,
            legend: "{pod}",
          },
        ],
      },
      {
        title: "Restarts (last hour, top 10)",
        kind: "line",
        unit: "short",
        queries: [
          {
            expr: `topk(10, sum by (pod) (increase(kube_pod_container_status_restarts_total{namespace=~"$namespace"}[1h])))`,
            legend: "{pod}",
          },
        ],
      },
      {
        title: "Pods by phase",
        kind: "line",
        unit: "short",
        queries: [{ expr: `sum by (phase) (kube_pod_status_phase{namespace=~"$namespace"})`, legend: "{phase}" }],
      },
      {
        title: "Network in (top 10 pods)",
        kind: "line",
        unit: "bytesps",
        queries: [
          {
            expr: `topk(10, sum by (pod) (rate(container_network_receive_bytes_total{namespace=~"$namespace"}[5m])))`,
            legend: "{pod}",
          },
        ],
      },
    ],
  },
  {
    id: "postgresql",
    app: "postgresql",
    exporter: { repo: "prometheus-community", chart: "prometheus-postgres-exporter" },
    group: "Databases",
    title: "PostgreSQL",
    description: "From the postgres-exporter.",
    requires: {
      metric: "pg_up",
      hint: "Needs a PostgreSQL exporter scraped by Prometheus (the prometheus-postgres-exporter chart, or the database chart's metrics option).",
    },
    variables: [{ name: "instance", label: "Instance", metric: "pg_up", by: "instance" }],
    panels: [
      { title: "Up", kind: "stat", unit: "short", queries: [{ expr: `min(pg_up{instance=~"$instance"})` }], help: "1 = reachable." },
      {
        title: "Connections",
        kind: "line",
        unit: "short",
        queries: [{ expr: `sum by (datname) (pg_stat_database_numbackends{instance=~"$instance"})`, legend: "{datname}" }],
      },
      {
        title: "Transactions per second",
        kind: "line",
        unit: "ops",
        queries: [
          { expr: `sum(rate(pg_stat_database_xact_commit{instance=~"$instance"}[5m]))`, legend: "commit" },
          { expr: `sum(rate(pg_stat_database_xact_rollback{instance=~"$instance"}[5m]))`, legend: "rollback" },
        ],
      },
      {
        title: "Database size",
        kind: "line",
        unit: "bytes",
        queries: [{ expr: `pg_database_size_bytes{instance=~"$instance"}`, legend: "{datname}" }],
      },
      {
        title: "Rows per second",
        kind: "line",
        unit: "ops",
        queries: [
          { expr: `sum(rate(pg_stat_database_tup_fetched{instance=~"$instance"}[5m]))`, legend: "fetched" },
          { expr: `sum(rate(pg_stat_database_tup_inserted{instance=~"$instance"}[5m]))`, legend: "inserted" },
          { expr: `sum(rate(pg_stat_database_tup_updated{instance=~"$instance"}[5m]))`, legend: "updated" },
          { expr: `sum(rate(pg_stat_database_tup_deleted{instance=~"$instance"}[5m]))`, legend: "deleted" },
        ],
      },
      {
        title: "Cache hit ratio",
        kind: "line",
        unit: "percentunit",
        queries: [
          {
            expr: `sum(rate(pg_stat_database_blks_hit{instance=~"$instance"}[5m])) / (sum(rate(pg_stat_database_blks_hit{instance=~"$instance"}[5m])) + sum(rate(pg_stat_database_blks_read{instance=~"$instance"}[5m])))`,
            legend: "hit",
          },
        ],
      },
    ],
  },
  {
    id: "redis",
    app: "redis",
    exporter: { repo: "prometheus-community", chart: "prometheus-redis-exporter" },
    group: "Databases",
    title: "Redis / Valkey",
    description: "From the redis_exporter.",
    requires: {
      metric: "redis_up",
      hint: "Needs a Redis exporter scraped by Prometheus (the prometheus-redis-exporter chart, or the instance's metrics sidecar).",
    },
    variables: [{ name: "instance", label: "Instance", metric: "redis_up", by: "instance" }],
    panels: [
      { title: "Up", kind: "stat", unit: "short", queries: [{ expr: `min(redis_up{instance=~"$instance"})` }], help: "1 = reachable." },
      {
        title: "Connected clients",
        kind: "line",
        unit: "short",
        queries: [{ expr: `redis_connected_clients{instance=~"$instance"}`, legend: "{instance}" }],
      },
      {
        title: "Memory used",
        kind: "line",
        unit: "bytes",
        queries: [{ expr: `redis_memory_used_bytes{instance=~"$instance"}`, legend: "{instance}" }],
      },
      {
        title: "Commands per second",
        kind: "line",
        unit: "ops",
        queries: [{ expr: `rate(redis_commands_processed_total{instance=~"$instance"}[5m])`, legend: "{instance}" }],
      },
      {
        title: "Keys",
        kind: "line",
        unit: "short",
        queries: [{ expr: `sum by (instance) (redis_db_keys{instance=~"$instance"})`, legend: "{instance}" }],
      },
      {
        title: "Hit ratio",
        kind: "line",
        unit: "percentunit",
        queries: [
          {
            expr: `rate(redis_keyspace_hits_total{instance=~"$instance"}[5m]) / (rate(redis_keyspace_hits_total{instance=~"$instance"}[5m]) + rate(redis_keyspace_misses_total{instance=~"$instance"}[5m]))`,
            legend: "{instance}",
          },
        ],
      },
      {
        title: "Evicted keys per second",
        kind: "line",
        unit: "ops",
        queries: [{ expr: `rate(redis_evicted_keys_total{instance=~"$instance"}[5m])`, legend: "{instance}" }],
      },
    ],
  },
  {
    id: "minio",
    app: "minio",
    group: "Storage & identity",
    title: "MinIO",
    description: "From MinIO's own Prometheus endpoint (cluster metrics).",
    requires: {
      metric: "minio_cluster_capacity_usable_total_bytes",
      hint: "MinIO exposes these on /minio/v2/metrics/cluster; Prometheus has to scrape it (a ServiceMonitor, or a scrape job with MINIO_PROMETHEUS_AUTH_TYPE=public).",
    },
    panels: [
      {
        title: "Capacity used",
        kind: "stat",
        unit: "percentunit",
        queries: [
          {
            expr: `1 - sum(minio_cluster_capacity_usable_free_bytes) / sum(minio_cluster_capacity_usable_total_bytes)`,
          },
        ],
      },
      {
        title: "Nodes online",
        kind: "stat",
        unit: "short",
        queries: [{ expr: "max(minio_cluster_nodes_online_total)" }],
      },
      {
        title: "Capacity",
        kind: "line",
        unit: "bytes",
        queries: [
          { expr: "sum(minio_cluster_capacity_usable_total_bytes)", legend: "total" },
          { expr: "sum(minio_cluster_capacity_usable_free_bytes)", legend: "free" },
        ],
      },
      {
        title: "Bucket size",
        kind: "line",
        unit: "bytes",
        queries: [{ expr: "max by (bucket) (minio_bucket_usage_total_bytes)", legend: "{bucket}" }],
      },
      {
        title: "S3 requests per second",
        kind: "line",
        unit: "ops",
        queries: [{ expr: "sum by (api) (rate(minio_s3_requests_total[5m]))", legend: "{api}" }],
      },
      {
        title: "S3 traffic",
        kind: "line",
        unit: "bytesps",
        queries: [
          { expr: "sum(rate(minio_s3_traffic_received_bytes[5m]))", legend: "received" },
          { expr: "sum(rate(minio_s3_traffic_sent_bytes[5m]))", legend: "sent" },
        ],
      },
    ],
  },
  {
    id: "keycloak",
    app: "keycloak",
    group: "Storage & identity",
    title: "Keycloak",
    description: "From Keycloak's metrics endpoint (Quarkus).",
    requires: {
      metric: "jvm_memory_used_bytes",
      hint: "Keycloak has to run with metrics enabled (KC_METRICS_ENABLED=true) and Prometheus has to scrape its management port.",
    },
    panels: [
      {
        title: "Logins per second",
        kind: "line",
        unit: "ops",
        queries: [{ expr: `sum(rate(keycloak_user_events_total{event="login"}[5m]))`, legend: "login" }, { expr: `sum(rate(keycloak_user_events_total{event="login_error"}[5m]))`, legend: "failed" }],
        help: "Needs user event metrics enabled in Keycloak.",
      },
      {
        title: "HTTP requests per second",
        kind: "line",
        unit: "ops",
        queries: [{ expr: "sum by (status) (rate(http_server_requests_seconds_count[5m]))", legend: "{status}" }],
      },
      {
        title: "JVM heap used",
        kind: "line",
        unit: "bytes",
        queries: [{ expr: `sum by (pod) (jvm_memory_used_bytes{area="heap"})`, legend: "{pod}" }],
      },
      {
        title: "JVM threads",
        kind: "line",
        unit: "short",
        queries: [{ expr: "sum by (pod) (jvm_threads_live_threads)", legend: "{pod}" }],
      },
    ],
  },
];
