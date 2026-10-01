// `formType` (Roadmap Phase 31) names the form schema/converter that can
// assemble this kind without hand-written YAML; templates without one are
// YAML-only.
export type ResourceTemplate = {
  label: string;
  yaml: string;
  formType?: string;
};

// Starter manifests for the "+ Create" buttons (Roadmap Phase 25). Kept
// minimal — just enough that the cluster's own dry-run has something valid to
// start from.

const deployment: ResourceTemplate = {
  label: "Deployment",
  formType: "deployment",
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
  formType: "configmap",
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
  formType: "secret",
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

const namespace: ResourceTemplate = {
  label: "Namespace",
  formType: "namespace",
  yaml: `apiVersion: v1
kind: Namespace
metadata:
  name: my-namespace
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
  deployments: [deployment, namespace],
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

export const namespaceTemplates = [namespace];

// Starter manifests for a namespace's quota / limit range (Roadmap Phase 27),
// pre-filled with the namespace they're being added to. Both are
// namespaced single objects, so they go through the same Phase 25
// dry-run -> apply path as everything else.
export function quotaTemplate(namespace: string): ResourceTemplate {
  return {
    label: "ResourceQuota",
    yaml: `apiVersion: v1
kind: ResourceQuota
metadata:
  name: compute-quota
  namespace: ${namespace}
spec:
  hard:
    pods: "10"
    requests.cpu: "2"
    requests.memory: 4Gi
    limits.cpu: "4"
    limits.memory: 8Gi
`,
  };
}

export function limitRangeTemplate(namespace: string): ResourceTemplate {
  return {
    label: "LimitRange",
    yaml: `apiVersion: v1
kind: LimitRange
metadata:
  name: container-defaults
  namespace: ${namespace}
spec:
  limits:
    - type: Container
      defaultRequest:
        cpu: 100m
        memory: 128Mi
      default:
        cpu: 500m
        memory: 256Mi
`,
  };
}
