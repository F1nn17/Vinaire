export type StatusColor = "ok" | "warn" | "err";

// Matches the backend's api.ClusterRow (Roadmap Phase 11).
export type Cluster = {
  name: string;
  default: boolean;
  // false for the env-var-configured cluster(s) — cluster management (the
  // web UI's Settings page) can't remove those.
  removable: boolean;
};

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

// Matches the backend's api.StatefulSetDetailResponse (Roadmap Phase 12).
export type StatefulSetDetail = {
  name: string;
  namespace: string;
  status: string;
  statusColor: StatusColor;
  ready: string;
  serviceName: string;
  updateStrategy: string;
  image: string;
  age: string;
  labels: Record<string, string>;
  events: ClusterEvent[];
  yaml: string;
};

// Matches the backend's api.DaemonSetDetailResponse (Roadmap Phase 12).
export type DaemonSetDetail = {
  name: string;
  namespace: string;
  status: string;
  statusColor: StatusColor;
  ready: string;
  desired: number;
  current: number;
  updateStrategy: string;
  image: string;
  age: string;
  labels: Record<string, string>;
  events: ClusterEvent[];
  yaml: string;
};

// Matches the backend's api.JobDetailResponse (Roadmap Phase 12). Scoped to
// plain batch/v1 Jobs only — CronJob isn't covered.
export type JobDetail = {
  name: string;
  namespace: string;
  status: string;
  statusColor: StatusColor;
  completions: string;
  active: number;
  failed: number;
  startTime: string;
  completionTime: string;
  image: string;
  age: string;
  labels: Record<string, string>;
  events: ClusterEvent[];
  yaml: string;
};

// Matches the backend's api.PVCRow (Roadmap Phase 13).
export type PVCRow = {
  name: string;
  namespace: string;
  status: string;
  statusColor: StatusColor;
  capacity: string;
  accessModes: string;
  storageClass: string;
  age: string;
  href: string;
};

// Matches the backend's api.PVRow (Roadmap Phase 13). PersistentVolumes are
// cluster-scoped — no namespace field.
export type PVRow = {
  name: string;
  status: string;
  statusColor: StatusColor;
  capacity: string;
  accessModes: string;
  reclaimPolicy: string;
  storageClass: string;
  claimRef: string;
  age: string;
};

// Matches the backend's api.StorageClassRow (Roadmap Phase 13).
// StorageClasses are cluster-scoped.
export type StorageClassRow = {
  name: string;
  provisioner: string;
  reclaimPolicy: string;
  volumeBindingMode: string;
  isDefault: boolean;
  age: string;
};

// Matches the backend's api.PVCMountingPod (Roadmap Phase 13).
export type PVCMountingPod = {
  name: string;
  namespace: string;
};

// Matches the backend's api.PVCDetailResponse (Roadmap Phase 13).
export type PVCDetail = {
  name: string;
  namespace: string;
  status: string;
  statusColor: StatusColor;
  capacity: string;
  accessModes: string;
  storageClass: string;
  volumeName: string;
  age: string;
  labels: Record<string, string>;
  mountedBy: PVCMountingPod[];
  events: ClusterEvent[];
  yaml: string;
};

// Matches the backend's api.RoleRow (Roadmap Phase 14).
export type RoleRow = {
  name: string;
  namespace: string;
  rules: number;
  age: string;
};

// Matches the backend's api.ClusterRoleRow (Roadmap Phase 14).
// ClusterRoles are cluster-scoped.
export type ClusterRoleRow = {
  name: string;
  rules: number;
  age: string;
};

// Matches the backend's api.RoleBindingRow (Roadmap Phase 14).
export type RoleBindingRow = {
  name: string;
  namespace: string;
  roleKind: string;
  roleName: string;
  subjects: string;
  age: string;
};

// Matches the backend's api.ClusterRoleBindingRow (Roadmap Phase 14).
// ClusterRoleBindings are cluster-scoped.
export type ClusterRoleBindingRow = {
  name: string;
  roleKind: string;
  roleName: string;
  subjects: string;
  age: string;
};

// Matches the backend's api.ServiceAccountRow (Roadmap Phase 14).
export type ServiceAccountRow = {
  name: string;
  namespace: string;
  secrets: number;
  age: string;
};

// Matches the backend's api.RBACSubject (Roadmap Phase 14).
export type RBACSubject = {
  kind: string;
  name: string;
  namespace?: string;
};

// Matches the backend's api.RBACRule (Roadmap Phase 14).
export type RBACRule = {
  apiGroups: string;
  resources: string;
  verbs: string;
};

// Matches the backend's api.RBACBindingSummary (Roadmap Phase 14) — a
// static "what's configured" rollup, never a live SubjectAccessReview/
// "can I do X" check.
export type RBACBindingSummary = {
  namespace: string;
  bindingKind: string;
  bindingName: string;
  roleKind: string;
  roleName: string;
  subjects: RBACSubject[];
  rules: RBACRule[];
};

// Matches the backend's api.CRDRow (Roadmap Phase 15).
export type CRDRow = {
  name: string;
  group: string;
  version: string;
  kind: string;
  plural: string;
  namespaced: boolean;
  age: string;
};

// Matches the backend's api.CRDInstanceRow (Roadmap Phase 15) — a generic
// fallback shape (name/namespace/age), not each CRD's own printer columns.
export type CRDInstanceRow = {
  name: string;
  namespace: string;
  age: string;
};

// Matches the backend's api.CRDInstanceDetailResponse (Roadmap Phase 15).
export type CRDInstanceDetail = {
  name: string;
  namespace: string;
  kind: string;
  age: string;
  labels: Record<string, string>;
  yaml: string;
};

// Matches the backend's api.AlertRow (Roadmap Phase 16) — a simplified view
// of an Alertmanager alert.
export type AlertRow = {
  name: string;
  severity: string;
  summary: string;
  statusColor: StatusColor;
  startsAt: string;
};

// Discriminated union every Phase 16 observability proxy returns — the
// same "explicit unavailable, never a fabricated result" pattern as
// GatewayResourcesResult (Phase 8).
export type AlertsResult =
  | { available: true; alerts: AlertRow[] }
  | { available: false; reason: string };

export type LogsSearchResult =
  { available: true; lines: LogLine[] } | { available: false; reason: string };

// Raw Prometheus response, forwarded as-is under `result` — this app never
// evaluates PromQL itself, so there's no typed shape to declare beyond
// Prometheus's own (see https://prometheus.io/docs/prometheus/latest/querying/api/).
export type MetricsQueryResult =
  { available: true; result: unknown } | { available: false; reason: string };

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
  // Roadmap Phase 18: null means "not available yet" (e.g. PVC near
  // capacity, stubbed until real byte-usage data exists) — rendered
  // visibly differently from a real 0, never a fabricated zero.
  value: number | null;
  caption: string;
};

export type NamespacePodCount = {
  name: string;
  pods: number;
};

// Matches the backend's api.DashboardSummaryResponse (Roadmap Phase 18).
export type DashboardSummary = {
  podsDown: number;
  podsDownSample: string[];
  healthyPods: number;
  namespaces: NamespacePodCount[];
  argoCDAvailable: boolean;
  driftedCount: number;
  driftedSample: string[];
};

export type DashboardData = {
  statCards: StatCardData[];
  // null when ArgoCD isn't installed — the Drifted-from-Git card hides
  // entirely rather than showing a fabricated zero.
  driftedFromGit: {
    count: number;
    dotColor: StatusColor;
    caption: string;
  } | null;
  events: ClusterEvent[];
  // null until Phase 16's Prometheus proxy is wired into the Dashboard —
  // shows an honest placeholder instead of a drawn (fabricated) line.
  cpu: { used: string; total: string; points: number[] } | null;
  memory: { used: string; total: string; points: number[] } | null;
  namespaces: NamespacePodCount[];
};

// Real graph shape (Roadmap Phase 7) — node count and edges vary per
// namespace, computed server-side from owner references and selectors.
export type TopologyNodeKind =
  "ingress" | "service" | "deployment" | "pod" | "configmap" | "secret";

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
