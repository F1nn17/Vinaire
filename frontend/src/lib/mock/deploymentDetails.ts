import type { DeploymentDetail } from "@/lib/types";

export const deploymentDetailsMock: Record<string, DeploymentDetail> = {
  keycloak: {
    name: "keycloak",
    namespace: "auth",
    status: "Healthy",
    statusColor: "ok",
    readySummary: "Healthy · 2/2",
    replicas: "2/2 ready",
    strategy: "RollingUpdate",
    image: "bitnami/keycloak:24.0",
    managedBy: "ArgoCD · in sync",
    age: "14d",
    pods: [
      {
        name: "keycloak-6f9d-abc12",
        node: "worker-2",
        ready: "1/1",
        status: "Running",
        statusColor: "ok",
        age: "14d",
      },
      {
        name: "keycloak-6f9d-xyz88",
        node: "worker-3",
        ready: "1/1",
        status: "Running",
        statusColor: "warn",
        age: "14d",
      },
    ],
    revisions: [
      {
        revision: 3,
        current: true,
        image: "bitnami/keycloak:24.0",
        age: "14d ago",
      },
      {
        revision: 2,
        current: false,
        image: "bitnami/keycloak:23.0",
        age: "29d ago",
      },
      {
        revision: 1,
        current: false,
        image: "bitnami/keycloak:22.0",
        age: "51d ago",
      },
    ],
    yaml: `apiVersion: apps/v1
kind: Deployment
metadata:
  name: keycloak
  namespace: auth
spec:
  replicas: 2
  strategy:
    type: RollingUpdate
  selector:
    matchLabels:
      app: keycloak
  template:
    spec:
      containers:
        - name: keycloak
          image: bitnami/keycloak:24.0
          ports:
            - containerPort: 8080`,
  },

  "payments-api": {
    name: "payments-api",
    namespace: "default",
    status: "Degraded",
    statusColor: "err",
    readySummary: "Degraded · 2/3",
    replicas: "2/3 ready",
    strategy: "RollingUpdate",
    image: "ghcr.io/vinaire/payments-api:1.9.2",
    managedBy: "ArgoCD · in sync",
    age: "6d",
    pods: [
      {
        name: "payments-api-7d9f4c-x2kpl",
        node: "worker-1",
        ready: "0/1",
        status: "CrashLoopBackOff",
        statusColor: "err",
        age: "2h",
      },
      {
        name: "payments-api-7d9f4c-a1b2c",
        node: "worker-2",
        ready: "1/1",
        status: "Running",
        statusColor: "ok",
        age: "6d",
      },
      {
        name: "payments-api-7d9f4c-d3e4f",
        node: "worker-3",
        ready: "1/1",
        status: "Running",
        statusColor: "ok",
        age: "6d",
      },
    ],
    revisions: [
      {
        revision: 5,
        current: true,
        image: "ghcr.io/vinaire/payments-api:1.9.2",
        age: "6d ago",
      },
      {
        revision: 4,
        current: false,
        image: "ghcr.io/vinaire/payments-api:1.8.0",
        age: "19d ago",
      },
    ],
    yaml: `apiVersion: apps/v1
kind: Deployment
metadata:
  name: payments-api
  namespace: default
spec:
  replicas: 3
  strategy:
    type: RollingUpdate
  selector:
    matchLabels:
      app: payments-api
  template:
    spec:
      containers:
        - name: payments-api
          image: ghcr.io/vinaire/payments-api:1.9.2
          ports:
            - containerPort: 8080`,
  },

  "notifications-svc": {
    name: "notifications-svc",
    namespace: "default",
    status: "OutOfSync",
    statusColor: "warn",
    readySummary: "OutOfSync · 1/1",
    replicas: "1/1 ready",
    strategy: "RollingUpdate",
    image: "ghcr.io/vinaire/notifications-svc:0.6.1",
    managedBy: "ArgoCD · OutOfSync",
    age: "3d",
    pods: [
      {
        name: "notifications-svc-59c7d-m4n5o",
        node: "worker-1",
        ready: "1/1",
        status: "Running",
        statusColor: "ok",
        age: "3d",
      },
    ],
    revisions: [
      {
        revision: 2,
        current: true,
        image: "ghcr.io/vinaire/notifications-svc:0.6.1",
        age: "3d ago",
      },
      {
        revision: 1,
        current: false,
        image: "ghcr.io/vinaire/notifications-svc:0.5.0",
        age: "12d ago",
      },
    ],
    yaml: `apiVersion: apps/v1
kind: Deployment
metadata:
  name: notifications-svc
  namespace: default
spec:
  replicas: 1
  strategy:
    type: RollingUpdate
  selector:
    matchLabels:
      app: notifications-svc
  template:
    spec:
      containers:
        - name: notifications-svc
          image: ghcr.io/vinaire/notifications-svc:0.6.1`,
  },

  frontend: {
    name: "frontend",
    namespace: "default",
    status: "Healthy",
    statusColor: "ok",
    readySummary: "Healthy · 2/2",
    replicas: "2/2 ready",
    strategy: "RollingUpdate",
    image: "ghcr.io/vinaire/frontend:2.4.0",
    managedBy: "ArgoCD · in sync",
    age: "9d",
    pods: [
      {
        name: "frontend-5c7b9-qq2r1",
        node: "worker-1",
        ready: "1/1",
        status: "Running",
        statusColor: "ok",
        age: "9d",
      },
      {
        name: "frontend-5c7b9-r8s9t",
        node: "worker-2",
        ready: "1/1",
        status: "Running",
        statusColor: "ok",
        age: "9d",
      },
    ],
    revisions: [
      {
        revision: 4,
        current: true,
        image: "ghcr.io/vinaire/frontend:2.4.0",
        age: "9d ago",
      },
      {
        revision: 3,
        current: false,
        image: "ghcr.io/vinaire/frontend:2.3.1",
        age: "25d ago",
      },
    ],
    yaml: `apiVersion: apps/v1
kind: Deployment
metadata:
  name: frontend
  namespace: default
spec:
  replicas: 2
  strategy:
    type: RollingUpdate
  selector:
    matchLabels:
      app: frontend
  template:
    spec:
      containers:
        - name: frontend
          image: ghcr.io/vinaire/frontend:2.4.0
          ports:
            - containerPort: 3000`,
  },

  "kafka-broker": {
    name: "kafka-broker",
    namespace: "messaging",
    status: "Healthy",
    statusColor: "ok",
    readySummary: "Healthy · 1/1",
    replicas: "1/1 ready",
    strategy: "RollingUpdate",
    image: "bitnami/kafka:3.7",
    managedBy: "ArgoCD · in sync",
    age: "14d",
    pods: [
      {
        name: "kafka-broker-0",
        node: "worker-3",
        ready: "1/1",
        status: "Running",
        statusColor: "ok",
        age: "14d",
      },
    ],
    revisions: [
      {
        revision: 2,
        current: true,
        image: "bitnami/kafka:3.7",
        age: "14d ago",
      },
      {
        revision: 1,
        current: false,
        image: "bitnami/kafka:3.6",
        age: "40d ago",
      },
    ],
    yaml: `apiVersion: apps/v1
kind: Deployment
metadata:
  name: kafka-broker
  namespace: messaging
spec:
  replicas: 1
  strategy:
    type: RollingUpdate
  selector:
    matchLabels:
      app: kafka-broker
  template:
    spec:
      containers:
        - name: kafka
          image: bitnami/kafka:3.7`,
  },
};
