export type StatusColor = "ok" | "warn" | "err";

// Matches the backend's api.ClusterRow (Roadmap Phase 11).
export type Cluster = {
  name: string;
  default: boolean;
  // false for the env-var-configured cluster(s) — cluster management (the
  // web UI's Settings page) can't remove those.
  removable: boolean;
  // Roadmap Phase 34 — detected (and user-correctable) kind of cluster.
  type: ClusterType;
  // Whether Phase 30's "Add node" makes sense for this type (k3s/kubeadm).
  canAddNode: boolean;
  status: "connected" | "unreachable";
  // Roadmap Phase 49: the health of the bearer token Vinaire uses for this cluster.
  credential?: "no-expiry" | "expiring" | "expired";
  credentialExpiresAt?: string;
};

export type ClusterType =
  | "k3s"
  | "k3d"
  | "kind"
  | "minikube"
  | "microk8s"
  | "kubeadm"
  | "managed"
  | "unknown";

// Matches the backend's api.SearchResult (Roadmap Phase 20).
export type SearchResult = {
  kind: string;
  name: string;
  namespace: string;
  href: string;
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

// Matches the backend's api.NodeRow (Roadmap Phase 19). cpu/memory are null
// when metrics-server isn't installed/reachable, same rule as everywhere
// else this shape appears.
export type NodeRow = {
  name: string;
  status: string;
  statusColor: StatusColor;
  roles: string;
  cpu: { used: string; limit: string; pct: number } | null;
  memory: { used: string; limit: string; pct: number } | null;
  age: string;
};

// Matches the backend's api.NodeResources (Roadmap Phase 19) — raw
// resource.Quantity strings, not a reformatted unit.
export type NodeResources = {
  cpu: string;
  memory: string;
  pods: string;
};

// Matches the backend's api.NodeConditionRow (Roadmap Phase 19).
export type NodeConditionRow = {
  type: string;
  status: string;
  reason: string;
  message: string;
};

// Matches the backend's api.NodeTaintRow (Roadmap Phase 19).
export type NodeTaintRow = {
  key: string;
  value: string;
  effect: string;
};

// Matches the backend's api.NodeScheduledPodRow (Roadmap Phase 19).
export type NodeScheduledPodRow = {
  name: string;
  namespace: string;
  ready: string;
  status: string;
  statusColor: StatusColor;
  age: string;
};

// Matches the backend's api.NodeDetailResponse (Roadmap Phase 19).
export type NodeDetail = {
  name: string;
  status: string;
  statusColor: StatusColor;
  roles: string;
  age: string;
  kubeletVersion: string;
  os: string;
  capacity: NodeResources;
  allocatable: NodeResources;
  cpu: { used: string; limit: string; pct: number } | null;
  memory: { used: string; limit: string; pct: number } | null;
  conditions: NodeConditionRow[];
  taints: NodeTaintRow[];
  pods: NodeScheduledPodRow[];
  labels: Record<string, string>;
  // cordoned: no new pods are scheduled onto the node
  unschedulable: boolean;
  yaml: string;
};

// Matches the backend's api.CapacityUsage (Roadmap Phase 22) — a
// requests-based figure (what's reserved by pods), not live metrics-server
// usage (what's actually running right now — see NodeRow's cpu/memory
// instead for that).
export type CapacityUsage = {
  used: string;
  total: string;
  pct: number;
  color: StatusColor;
};

// Matches the backend's api.NodeCapacityRow (Roadmap Phase 22).
export type NodeCapacityRow = {
  name: string;
  cpu: CapacityUsage;
  memory: CapacityUsage;
  pods: CapacityUsage;
};

// Matches the backend's api.CapacityResponse (Roadmap Phase 22).
export type CapacityResponse = {
  nodes: NodeCapacityRow[];
  cluster: NodeCapacityRow;
};

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
  // Roadmap Phase 33: true when this pod has an owning controller — the
  // action bar shows "Restart" (effectively a delete+recreate) only when
  // true, otherwise just "Delete" with an explicit warning.
  hasController: boolean;
  // Roadmap Phase 26: ports the pod's containers declare (a pod may serve more).
  ports: PodPort[];
};

export type PodPort = {
  container: string;
  name: string;
  port: number;
  protocol: string;
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
  // Roadmap Phase 33: desiredReplicas backs the Scale control;
  // paused backs the "Rollout paused" badge and Pause/Resume action.
  desiredReplicas: number;
  paused: boolean;
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
  // Roadmap Phase 33: backs the Scale control.
  desiredReplicas: number;
  serviceName: string;
  updateStrategy: string;
  image: string;
  age: string;
  labels: Record<string, string>;
  pods: DeploymentPodRow[];
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
  pods: DeploymentPodRow[];
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
  pods: DeploymentPodRow[];
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
  // null when metrics-server isn't installed/reachable — see ResourceUsage
  // in workloads.ts for the identical shape Pod detail already uses.
  cpu: { used: string; limit: string; pct: number } | null;
  memory: { used: string; limit: string; pct: number } | null;
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
  // null when metrics-server isn't installed/reachable — an honest
  // placeholder instead of a drawn (fabricated) line. No history is
  // stored, so this is a live snapshot (used/total/pct) rendered as a
  // percentage bar, not a Sparkline.
  cpu: { used: string; total: string; pct: number } | null;
  memory: { used: string; total: string; pct: number } | null;
  namespaces: NamespacePodCount[];
};

// Real graph shape (Roadmap Phase 7) — node count and edges vary per
// namespace, computed server-side from owner references and selectors.
export type TopologyNodeKind =
  | "ingress"
  | "service"
  | "deployment"
  | "statefulset"
  | "daemonset"
  | "job"
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

// Roadmap Phase 27 — match the backend's api.NamespaceRow / NamespaceDetailResponse.
export type QuotaUsageRow = {
  resource: string;
  used: string;
  hard: string;
  pct: number;
  color: StatusColor;
};

export type NamespaceRow = {
  name: string;
  status: string;
  statusColor: StatusColor;
  age: string;
  // null = couldn't list pods (RBAC); must not read as zero.
  pods: number | null;
  quota: QuotaUsageRow[];
  href: string;
  // Roadmap Phase 53: the namespace's vinaire.io/project label.
  project?: string;
};

export type QuotaDetail = {
  name: string;
  items: QuotaUsageRow[];
  yaml: string;
};

export type LimitRow = {
  type: string;
  resource: string;
  min: string;
  max: string;
  default: string;
  defaultRequest: string;
};

export type LimitRangeDetail = {
  name: string;
  limits: LimitRow[];
  yaml: string;
};

export type NamespaceDetail = {
  name: string;
  status: string;
  statusColor: StatusColor;
  age: string;
  labels: Record<string, string>;
  quotas: QuotaDetail[];
  limitRanges: LimitRangeDetail[];
  // kind -> count; null = couldn't list that kind (e.g. RBAC).
  resources: Record<string, number | null>;
  protected: boolean;
  yaml: string;
};

// Roadmap Phase 32 — matches the backend's api.ResourceMetricsResponse.
// `available: false` = Prometheus couldn't be asked (reason says why);
// `available: true, noData: true` = it answered with no samples. Neither is
// a chart with points, and neither should render as an empty chart.
export type MetricType = "cpu" | "memory" | "network" | "filesystem";
export type MetricRange = "1h" | "6h" | "24h" | "7d";
export type MetricResolution = "low" | "medium" | "high";
export type MetricResourceType =
  | "pods"
  | "deployments"
  | "statefulsets"
  | "daemonsets"
  | "jobs"
  | "nodes"
  | "cluster";

export type MetricsSeries = {
  name: string;
  // [unix seconds, value]
  points: [number, number][];
};

export type ResourceMetrics = {
  available: boolean;
  noData: boolean;
  reason?: string;
  type: MetricType;
  unit: "cores" | "bytes" | "bytes/s";
  range: MetricRange;
  stepSeconds: number;
  start: number;
  end: number;
  series: MetricsSeries[];
};
