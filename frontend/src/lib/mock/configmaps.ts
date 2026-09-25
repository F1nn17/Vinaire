import type { ConfigMapDetail } from "@/lib/types";

export const configMapsMock: ConfigMapDetail[] = [
  {
    id: "cm1",
    name: "keycloak-realm-config",
    namespace: "auth",
    age: "14d",
    keys: [
      {
        label: "realm.json",
        value:
          '{\n  "realm": "auth",\n  "enabled": true,\n  "sslRequired": "external",\n  "accessTokenLifespan": 300\n}',
      },
    ],
  },
  {
    id: "cm2",
    name: "app-config",
    namespace: "default",
    age: "9d",
    keys: [
      { label: "APP_ENV", value: "production" },
      { label: "LOG_LEVEL", value: "info" },
      { label: "FEATURE_FLAGS", value: "beta-search=true" },
    ],
  },
  {
    id: "cm3",
    name: "nginx-ingress-config",
    namespace: "network",
    age: "44d",
    keys: [
      { label: "proxy-body-size", value: "32m" },
      { label: "ssl-redirect", value: "true" },
    ],
  },
  {
    id: "cm4",
    name: "grafana-dashboards",
    namespace: "observability",
    age: "21d",
    keys: [
      {
        label: "cluster-overview.json",
        value: '{\n  "title": "Cluster overview",\n  "panels": 12\n}',
      },
    ],
  },
];
