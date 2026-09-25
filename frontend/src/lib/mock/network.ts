import type { GatewayRow, IngressRow } from "@/lib/types";

export const ingressMock: IngressRow[] = [
  {
    name: "auth-ingress",
    namespace: "auth",
    hosts: "auth.example.com",
    backend: "keycloak-svc:8080",
    tls: "Valid",
    tlsColor: "ok",
  },
  {
    name: "app-ingress",
    namespace: "default",
    hosts: "app.example.com",
    backend: "frontend-svc:80",
    tls: "Valid",
    tlsColor: "ok",
  },
  {
    name: "api-ingress",
    namespace: "default",
    hosts: "api.example.com",
    backend: "payments-api-svc:8080",
    tls: "Expiring in 6d",
    tlsColor: "warn",
  },
];

export const gatewayMock: GatewayRow[] = [
  {
    name: "main-gateway",
    namespace: "network",
    kind: "Gateway",
    status: "Programmed",
    statusColor: "ok",
  },
  {
    name: "auth-route",
    namespace: "auth",
    kind: "HTTPRoute",
    status: "Accepted",
    statusColor: "ok",
  },
  {
    name: "api-route",
    namespace: "default",
    kind: "HTTPRoute",
    status: "Accepted",
    statusColor: "ok",
  },
];
