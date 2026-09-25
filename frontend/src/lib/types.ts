export type StatusColor = "ok" | "warn" | "err";

export type WorkloadRow = {
  name: string;
  namespace: string;
  ready: string;
  status: string;
  statusColor: StatusColor;
  restarts: string;
  age: string;
  href: string;
};

export type WorkloadKind =
  "deployments" | "pods" | "statefulsets" | "daemonsets" | "jobs";

export type ContainerSummary = {
  name: string;
  statusColor: StatusColor;
  restarts: number;
};

export type LogLine = {
  time: string;
  level: "INFO" | "WARN" | "ERROR";
  text: string;
};

export type ClusterEvent = {
  statusColor: StatusColor;
  title: string;
  meta: string;
  time: string;
};

export type PodDetail = {
  name: string;
  namespace: string;
  status: string;
  statusColor: StatusColor;
  node: string;
  podIp: string;
  qosClass: string;
  restarts: number;
  image: string;
  age: string;
  labels: Record<string, string>;
  cpu: { used: string; limit: string; pct: number };
  memory: { used: string; limit: string; pct: number };
  containers: ContainerSummary[];
  events: ClusterEvent[];
  yaml: string;
  deploymentName?: string;
};

export type Revision = {
  revision: number;
  current: boolean;
  image: string;
  age: string;
};

export type DeploymentPodRow = {
  name: string;
  node: string;
  ready: string;
  status: string;
  statusColor: StatusColor;
  age: string;
};

export type DeploymentDetail = {
  name: string;
  namespace: string;
  status: string;
  statusColor: StatusColor;
  readySummary: string;
  replicas: string;
  strategy: string;
  image: string;
  managedBy: string;
  age: string;
  pods: DeploymentPodRow[];
  revisions: Revision[];
  yaml: string;
};

export type ConfigMapKey = {
  label: string;
  value: string;
};

export type ConfigMapDetail = {
  id: string;
  name: string;
  namespace: string;
  age: string;
  keys: ConfigMapKey[];
};

// Note: no `value` field — the list/get endpoints never return secret
// values (see AGENTS.md's read-only/secrets rule). Use revealSecretKey()
// (lib/api/secrets.ts) to fetch one key's decoded value on demand.
export type SecretKey = {
  id: string;
  label: string;
};

export type SecretDetail = {
  id: string;
  name: string;
  namespace: string;
  type: string;
  age: string;
  keys: SecretKey[];
};

export type IngressRow = {
  name: string;
  namespace: string;
  hosts: string;
  backend: string;
  tls: string;
  tlsColor: StatusColor;
};

export type GatewayRow = {
  name: string;
  namespace: string;
  kind: "Gateway" | "HTTPRoute";
  status: string;
  statusColor: StatusColor;
};

// Gateway API CRDs aren't installed on every cluster (stock k3s/k3d don't
// ship them) — the backend reports that explicitly instead of an empty
// list or a 500, so the UI can show a clear "not installed" state.
export type GatewayResourcesResult =
  | { available: true; rows: GatewayRow[] }
  | { available: false; reason: string };

export type StatCardData = {
  title: string;
  dotColor: StatusColor;
  value: number;
  caption: string;
};

export type NamespacePodCount = {
  name: string;
  pods: number;
};

export type DashboardData = {
  statCards: StatCardData[];
  events: ClusterEvent[];
  cpu: { used: string; total: string; points: number[] };
  memory: { used: string; total: string; points: number[] };
  namespaces: NamespacePodCount[];
};

// Real graph shape (Roadmap Phase 7) — node count and edges vary per
// namespace, computed server-side from owner references and selectors.
export type TopologyNodeKind =
  | "ingress"
  | "service"
  | "deployment"
  | "pod"
  | "configmap"
  | "secret";

export type TopologyNode = {
  id: string;
  kind: TopologyNodeKind;
  name: string;
  namespace: string;
  statusText: string;
  statusColor: StatusColor;
  extra: [string, string][];
};

export type TopologyEdge = {
  from: string;
  to: string;
};

export type TopologyData = {
  namespace: string;
  nodes: TopologyNode[];
  edges: TopologyEdge[];
};
