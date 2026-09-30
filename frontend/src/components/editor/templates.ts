export type ResourceTemplate = { label: string; yaml: string };

// Starter manifests for the "+ Create" buttons (Roadmap Phase 25). Kept
// minimal — just enough that the cluster's own dry-run has something valid to
// start from.

const deployment: ResourceTemplate = {
  label: "Deployment",
  yaml: `apiVersion: apps/v1
kind: Deployment
metadata:
  name: my-app
  namespace: default
spec:
  replicas: 1
  selector:
    matchLabels:
      app: my-app
  template:
    metadata:
      labels:
        app: my-app
    spec:
      containers:
        - name: app
          image: nginx:alpine
`,
};

const statefulset: ResourceTemplate = {
  label: "StatefulSet",
  yaml: `apiVersion: apps/v1
kind: StatefulSet
metadata:
  name: my-db
  namespace: default
spec:
  serviceName: my-db
  replicas: 1
  selector:
    matchLabels:
      app: my-db
  template:
    metadata:
      labels:
        app: my-db
    spec:
      containers:
        - name: db
          image: nginx:alpine
`,
};

const daemonset: ResourceTemplate = {
  label: "DaemonSet",
  yaml: `apiVersion: apps/v1
kind: DaemonSet
metadata:
  name: my-agent
  namespace: default
spec:
  selector:
    matchLabels:
      app: my-agent
  template:
    metadata:
      labels:
        app: my-agent
    spec:
      containers:
        - name: agent
          image: nginx:alpine
`,
};

const job: ResourceTemplate = {
  label: "Job",
  yaml: `apiVersion: batch/v1
kind: Job
metadata:
  name: my-job
  namespace: default
spec:
  template:
    spec:
      restartPolicy: Never
      containers:
        - name: job
          image: busybox
          command: ["sh", "-c", "echo hello"]
`,
};

const pod: ResourceTemplate = {
  label: "Pod",
  yaml: `apiVersion: v1
kind: Pod
metadata:
  name: my-pod
  namespace: default
spec:
  containers:
    - name: app
      image: nginx:alpine
`,
};

const configMap: ResourceTemplate = {
  label: "ConfigMap",
  yaml: `apiVersion: v1
kind: ConfigMap
metadata:
  name: my-config
  namespace: default
data:
  key: value
`,
};

const secret: ResourceTemplate = {
  label: "Secret",
  yaml: `apiVersion: v1
kind: Secret
metadata:
  name: my-secret
  namespace: default
type: Opaque
stringData:
  key: value
`,
};

const service: ResourceTemplate = {
  label: "Service",
  yaml: `apiVersion: v1
kind: Service
metadata:
  name: my-service
  namespace: default
spec:
  selector:
    app: my-app
  ports:
    - port: 80
      targetPort: 80
`,
};

const ingress: ResourceTemplate = {
  label: "Ingress",
  yaml: `apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: my-ingress
  namespace: default
spec:
  rules:
    - host: my-app.example.com
      http:
        paths:
          - path: /
            pathType: Prefix
            backend:
              service:
                name: my-service
                port:
                  number: 80
`,
};

export const workloadTemplates: Record<string, ResourceTemplate[]> = {
  deployments: [deployment],
  statefulsets: [statefulset],
  daemonsets: [daemonset],
  jobs: [job],
  pods: [pod],
};

export const configMapTemplates = [configMap];
export const secretTemplates = [secret];
export const networkTemplates: Record<string, ResourceTemplate[]> = {
  ingress: [ingress, service],
  gateway: [service, ingress],
};
