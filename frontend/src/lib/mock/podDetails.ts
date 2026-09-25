import type { PodDetail } from "@/lib/types";

export const podDetailsMock: Record<string, PodDetail> = {
  "keycloak-6f9d-abc12": {
    name: "keycloak-6f9d-abc12",
    namespace: "auth",
    status: "Running",
    statusColor: "ok",
    node: "worker-2",
    podIp: "10.42.3.17",
    qosClass: "Burstable",
    restarts: 0,
    image: "bitnami/keycloak:24.0",
    age: "14d",
    deploymentName: "keycloak",
    labels: { app: "keycloak", tier: "auth", version: "24.0" },
    cpu: { used: "42m", limit: "250m", pct: 17 },
    memory: { used: "312Mi", limit: "512Mi", pct: 61 },
    containers: [{ name: "keycloak", statusColor: "ok", restarts: 0 }],
    logs: [
      {
        time: "12:04:31",
        level: "INFO",
        text: "Started KeycloakServer in 8.221s",
      },
      {
        time: "12:04:33",
        level: "INFO",
        text: 'Realm "auth" cached, 214 entries',
      },
      {
        time: "12:06:02",
        level: "WARN",
        text: "Connection pool near capacity (18/20)",
      },
      {
        time: "12:09:47",
        level: "INFO",
        text: "Health check /health/ready -> 200",
      },
      {
        time: "12:11:02",
        level: "ERROR",
        text: "Failed to refresh token for client svc-notifications",
      },
      {
        time: "12:11:03",
        level: "INFO",
        text: "Retry succeeded after 1 attempt",
      },
    ],
    events: [
      {
        statusColor: "ok",
        title: "Started container keycloak",
        meta: "kubelet · worker-2",
        time: "14d ago",
      },
      {
        statusColor: "ok",
        title: "Successfully pulled image bitnami/keycloak:24.0",
        meta: "kubelet · worker-2",
        time: "14d ago",
      },
      {
        statusColor: "ok",
        title: "Readiness probe succeeded",
        meta: "kubelet · worker-2",
        time: "14d ago",
      },
    ],
    yaml: `apiVersion: v1
kind: Pod
metadata:
  name: keycloak-6f9d-abc12
  namespace: auth
  labels:
    app: keycloak
    tier: auth
spec:
  containers:
    - name: keycloak
      image: bitnami/keycloak:24.0
      resources:
        requests:
          cpu: 100m
          memory: 256Mi
        limits:
          cpu: 250m
          memory: 512Mi
status:
  phase: Running
  podIP: 10.42.3.17`,
  },

  "keycloak-6f9d-xyz88": {
    name: "keycloak-6f9d-xyz88",
    namespace: "auth",
    status: "Running",
    statusColor: "warn",
    node: "worker-3",
    podIp: "10.42.4.9",
    qosClass: "Burstable",
    restarts: 3,
    image: "bitnami/keycloak:24.0",
    age: "14d",
    deploymentName: "keycloak",
    labels: { app: "keycloak", tier: "auth", version: "24.0" },
    cpu: { used: "38m", limit: "250m", pct: 15 },
    memory: { used: "298Mi", limit: "512Mi", pct: 58 },
    containers: [{ name: "keycloak", statusColor: "warn", restarts: 3 }],
    logs: [
      {
        time: "09:40:11",
        level: "WARN",
        text: "Liveness probe failed, restarting container",
      },
      {
        time: "09:40:19",
        level: "INFO",
        text: "Started KeycloakServer in 7.9s",
      },
      {
        time: "10:02:44",
        level: "INFO",
        text: "Health check /health/ready -> 200",
      },
    ],
    events: [
      {
        statusColor: "warn",
        title: "Liveness probe failed 3 times",
        meta: "kubelet · worker-3",
        time: "3h ago",
      },
      {
        statusColor: "ok",
        title: "Started container keycloak",
        meta: "kubelet · worker-3",
        time: "3h ago",
      },
    ],
    yaml: `apiVersion: v1
kind: Pod
metadata:
  name: keycloak-6f9d-xyz88
  namespace: auth
  labels:
    app: keycloak
    tier: auth
spec:
  containers:
    - name: keycloak
      image: bitnami/keycloak:24.0
status:
  phase: Running
  podIP: 10.42.4.9`,
  },

  "payments-api-7d9f4c-x2kpl": {
    name: "payments-api-7d9f4c-x2kpl",
    namespace: "default",
    status: "CrashLoopBackOff",
    statusColor: "err",
    node: "worker-1",
    podIp: "10.42.1.44",
    qosClass: "Burstable",
    restarts: 14,
    image: "ghcr.io/vinaire/payments-api:1.9.2",
    age: "2h",
    deploymentName: "payments-api",
    labels: { app: "payments-api", tier: "backend" },
    cpu: { used: "3m", limit: "500m", pct: 1 },
    memory: { used: "64Mi", limit: "256Mi", pct: 25 },
    containers: [{ name: "payments-api", statusColor: "err", restarts: 14 }],
    logs: [
      { time: "14:02:01", level: "INFO", text: "Booting payments-api server" },
      {
        time: "14:02:03",
        level: "ERROR",
        text: "connect ECONNREFUSED postgres:5432",
      },
      {
        time: "14:02:03",
        level: "ERROR",
        text: "Fatal: could not reach database",
      },
      { time: "14:02:04", level: "WARN", text: "Exiting with code 1" },
    ],
    events: [
      {
        statusColor: "err",
        title: "Back-off restarting failed container",
        meta: "kubelet · worker-1",
        time: "2m ago",
      },
      {
        statusColor: "err",
        title: "Liveness probe failed: connection refused",
        meta: "kubelet · worker-1",
        time: "4m ago",
      },
    ],
    yaml: `apiVersion: v1
kind: Pod
metadata:
  name: payments-api-7d9f4c-x2kpl
  namespace: default
  labels:
    app: payments-api
    tier: backend
spec:
  containers:
    - name: payments-api
      image: ghcr.io/vinaire/payments-api:1.9.2
status:
  phase: CrashLoopBackOff
  podIP: 10.42.1.44`,
  },

  "frontend-5c7b9-qq2r1": {
    name: "frontend-5c7b9-qq2r1",
    namespace: "default",
    status: "Running",
    statusColor: "ok",
    node: "worker-1",
    podIp: "10.42.1.12",
    qosClass: "BestEffort",
    restarts: 0,
    image: "ghcr.io/vinaire/frontend:2.4.0",
    age: "9d",
    deploymentName: "frontend",
    labels: { app: "frontend", tier: "web" },
    cpu: { used: "8m", limit: "200m", pct: 4 },
    memory: { used: "96Mi", limit: "256Mi", pct: 37 },
    containers: [{ name: "frontend", statusColor: "ok", restarts: 0 }],
    logs: [
      { time: "08:00:12", level: "INFO", text: "Ready on port 3000" },
      { time: "08:14:55", level: "INFO", text: "GET /dashboard 200 42ms" },
    ],
    events: [
      {
        statusColor: "ok",
        title: "Started container frontend",
        meta: "kubelet · worker-1",
        time: "9d ago",
      },
    ],
    yaml: `apiVersion: v1
kind: Pod
metadata:
  name: frontend-5c7b9-qq2r1
  namespace: default
  labels:
    app: frontend
    tier: web
spec:
  containers:
    - name: frontend
      image: ghcr.io/vinaire/frontend:2.4.0
status:
  phase: Running
  podIP: 10.42.1.12`,
  },

  "kafka-broker-0": {
    name: "kafka-broker-0",
    namespace: "messaging",
    status: "Running",
    statusColor: "ok",
    node: "worker-3",
    podIp: "10.42.4.31",
    qosClass: "Guaranteed",
    restarts: 1,
    image: "bitnami/kafka:3.7",
    age: "14d",
    deploymentName: "kafka-broker",
    labels: { app: "kafka-broker", tier: "messaging" },
    cpu: { used: "120m", limit: "1000m", pct: 12 },
    memory: { used: "780Mi", limit: "2Gi", pct: 38 },
    containers: [{ name: "kafka", statusColor: "ok", restarts: 1 }],
    logs: [
      {
        time: "03:11:02",
        level: "WARN",
        text: "Rebalancing consumer group notifications",
      },
      {
        time: "03:11:09",
        level: "INFO",
        text: "Rebalance complete, 3 partitions assigned",
      },
    ],
    events: [
      {
        statusColor: "ok",
        title: "Kafka broker-0 rejoined the cluster",
        meta: "kubelet · worker-3",
        time: "51m ago",
      },
    ],
    yaml: `apiVersion: v1
kind: Pod
metadata:
  name: kafka-broker-0
  namespace: messaging
  labels:
    app: kafka-broker
    tier: messaging
spec:
  containers:
    - name: kafka
      image: bitnami/kafka:3.7
status:
  phase: Running
  podIP: 10.42.4.31`,
  },
};
