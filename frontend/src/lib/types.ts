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

export type SecretKey = {
  id: string;
  label: string;
  value: string;
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

export type TopologyNodeId =
  "ingress" | "svc" | "deploy" | "secret" | "cm" | "pod1" | "pod2";

export type TopologyNode = {
  id: TopologyNodeId;
  kind: string;
  name: string;
  namespace: string;
  statusText: string;
  statusColor: StatusColor;
  extra: [string, string][];
  warn?: boolean;
};

export type TopologyData = {
  namespace: string;
  nodes: Record<TopologyNodeId, TopologyNode>;
};
