import type { DashboardData } from "@/lib/types";

export const dashboardMock: DashboardData = {
  statCards: [
    {
      title: "Pods down",
      dotColor: "err",
      value: 3,
      caption: "payments-api, kafka-broker-0",
    },
    {
      title: "PVC near capacity",
      dotColor: "warn",
      value: 1,
      caption: "postgres-data — 91% used",
    },
    {
      title: "Drifted from Git",
      dotColor: "warn",
      value: 2,
      caption: "ArgoCD OutOfSync",
    },
    {
      title: "Healthy pods",
      dotColor: "ok",
      value: 42,
      caption: "across 6 namespaces",
    },
  ],
  events: [
    {
      statusColor: "err",
      title: "Back-off restarting failed container",
      meta: "pod/payments-api-7d9f4c-x2kpl · default",
      time: "2m ago",
    },
    {
      statusColor: "warn",
      title: "PVC postgres-data is 91% full",
      meta: "pvc/postgres-data · default",
      time: "9m ago",
    },
    {
      statusColor: "ok",
      title: "Rollout of keycloak completed",
      meta: "deployment/keycloak · auth",
      time: "21m ago",
    },
    {
      statusColor: "warn",
      title: "Application drifted from Git: notifications-svc",
      meta: "argocd/notifications-svc · gitops",
      time: "38m ago",
    },
    {
      statusColor: "ok",
      title: "Kafka broker-0 rejoined the cluster",
      meta: "pod/kafka-broker-0 · messaging",
      time: "51m ago",
    },
  ],
  cpu: {
    used: "4.1",
    total: "8 cores",
    points: [30, 26, 28, 18, 22, 14, 20, 10, 16, 8, 12, 6],
  },
  memory: {
    used: "11.2",
    total: "16 GB",
    points: [20, 22, 16, 18, 12, 16, 10, 14, 8, 10, 6, 9],
  },
  namespaces: [
    { name: "default", pods: 18 },
    { name: "auth", pods: 3 },
    { name: "messaging", pods: 4 },
    { name: "observability", pods: 9 },
  ],
};
