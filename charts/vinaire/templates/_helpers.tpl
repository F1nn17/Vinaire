{{/*
Chart name, truncated and DNS-1123-safe.
*/}}
{{/*
Where sessions live: "auto" is memory for one replica and a sealed Secret for more (Roadmap Phase 48).
*/}}
{{- define "vinaire.sessionStore" -}}
{{- $s := (.Values.sessions).store | default "auto" -}}
{{- if eq $s "auto" -}}{{- if gt (int .Values.replicaCount) 1 -}}secret{{- else -}}memory{{- end -}}{{- else -}}{{- $s -}}{{- end -}}
{{- end }}

{{- define "vinaire.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{/*
Fully qualified app name — <release>-vinaire, or just the release name if
it already contains "vinaire".
*/}}
{{- define "vinaire.fullname" -}}
{{- if contains .Chart.Name .Release.Name -}}
{{- .Release.Name | trunc 63 | trimSuffix "-" -}}
{{- else -}}
{{- printf "%s-%s" .Release.Name .Chart.Name | trunc 63 | trimSuffix "-" -}}
{{- end -}}
{{- end -}}

{{/*
Common labels.
*/}}
{{- define "vinaire.labels" -}}
app.kubernetes.io/name: {{ include "vinaire.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
helm.sh/chart: {{ printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" }}
{{- end -}}

{{/*
Selector labels — kept separate from the full label set since these must
never change across an upgrade (they're immutable on Deployment/Service).
*/}}
{{- define "vinaire.selectorLabels" -}}
app.kubernetes.io/name: {{ include "vinaire.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end -}}

{{/*
The ServiceAccount name to use — either the one given in values, or one
generated from the release, matching AGENTS.md's expectation that a real
RBAC binding exists for whoever the backend runs as.
*/}}
{{- define "vinaire.serviceAccountName" -}}
{{- if .Values.serviceAccount.create -}}
{{- default (include "vinaire.fullname" .) .Values.serviceAccount.name -}}
{{- else -}}
{{- default "default" .Values.serviceAccount.name -}}
{{- end -}}
{{- end -}}

{{/*
Everything Vinaire's UI can browse (read-only) plus the fixed, audited write
actions AGENTS.md's core rule allows, as RBAC rules. Used by the "viewer"
ClusterRole a person is bound to, and — only when rbac.broad or authMode
"disabled" says so — by the ServiceAccount's own ClusterRole.
*/}}
{{- define "vinaire.broadRules" -}}
- apiGroups: ["", "apps", "batch", "networking.k8s.io"]
  resources:
    - pods
    - deployments
    - statefulsets
    - daemonsets
    - jobs
    - configmaps
    - secrets
    - services
    - endpoints
    - ingresses
    - ingressclasses
    # What charts commonly create beside workloads — readable so those pages (and
    # an install's own existence checks) don't 403 on a plain browse.
    - networkpolicies
    - cronjobs
    - events
    - replicasets
    - serviceaccounts
    - persistentvolumeclaims
    - persistentvolumes
    - nodes
    # Roadmap Phase 27's Namespaces page.
    - namespaces
    - resourcequotas
    - limitranges
  verbs: ["get", "list", "watch"]
# metrics-server's aggregated API — Pod/Node CPU/Memory gauges and the
# Dashboard's cluster-wide usage all degrade to an honest "—" without
# this rather than failing loudly, so it's easy to forget.
- apiGroups: ["metrics.k8s.io"]
  resources: ["pods", "nodes"]
  verbs: ["get", "list"]
# pods/log and pods/exec are separate RBAC subresources — granting
# "pods" above does NOT imply either of these.
- apiGroups: [""]
  resources: ["pods/log"]
  verbs: ["get"]
- apiGroups: [""]
  resources: ["pods/exec"]
  verbs: ["create"]
# Phase 26's Forward port (the /proxy reverse proxy) — another separate
# subresource; drop it to disable the feature.
- apiGroups: [""]
  resources: ["pods/portforward"]
  verbs: ["create"]
- apiGroups: ["policy", "autoscaling"]
  resources: ["poddisruptionbudgets", "horizontalpodautoscalers"]
  verbs: ["get", "list", "watch"]
# Phase 45: searching logs through the Loki found in the cluster goes through the API
# server's service proxy, as the caller — and only for Loki's own Services. The API server
# authorizes a service proxy by the name "scheme:service:port" (what Vinaire asks for), not by
# the bare Service name, so that is what is listed; a Loki on another port needs its own rule.
- apiGroups: [""]
  resources: ["services/proxy"]
  resourceNames: ["http:loki:3100", "http:loki-gateway:80", "http:loki-read:3100", "http:loki-query-frontend:3100"]
  verbs: ["get"]
# Phase 37: metrics are read from the Prometheus found in the cluster the same way —
# the community chart's server, kube-prometheus-stack's, an operator's, VictoriaMetrics.
- apiGroups: [""]
  resources: ["services/proxy"]
  resourceNames: ["http:prometheus-server:80", "http:prometheus:9090", "http:prometheus-operated:9090", "http:kube-prometheus-stack-prometheus:9090", "http:vmsingle:8429", "http:prometheus-alertmanager:9093", "http:alertmanager:9093", "http:kube-prometheus-stack-alertmanager:9093"]
  verbs: ["get"]
- apiGroups: ["storage.k8s.io"]
  resources: ["storageclasses"]
  verbs: ["get", "list", "watch"]
- apiGroups: ["rbac.authorization.k8s.io"]
  resources: ["roles", "rolebindings", "clusterroles", "clusterrolebindings"]
  verbs: ["get", "list", "watch"]
- apiGroups: ["gateway.networking.k8s.io"]
  resources: ["gateways", "httproutes"]
  verbs: ["get", "list", "watch"]
- apiGroups: ["apiextensions.k8s.io"]
  resources: ["customresourcedefinitions"]
  verbs: ["get", "list", "watch"]
# The fixed write actions AGENTS.md's core rule allows (Phase 9's
# restart/delete/rollback, Phase 33's pause/resume/scale/pod delete).
# Phase 25's generic YAML create/update is deliberately NOT granted here:
# add the create/update verbs you want on the kinds you want through
# rbac.extraRules, so what Vinaire may write stays an explicit decision.
- apiGroups: ["apps"]
  resources: ["deployments"]
  verbs: ["patch", "delete"]
- apiGroups: ["apps"]
  resources: ["deployments/scale", "statefulsets/scale"]
  verbs: ["get", "update"]
- apiGroups: [""]
  resources: ["pods"]
  verbs: ["delete"]
# Phase 30's "Add node" gate (a SelfSubjectAccessReview for create on
# nodes) — not an API call the join makes, only the permission to ask.
# Drop it to disable the feature for this identity.
- apiGroups: [""]
  resources: ["nodes"]
  verbs: ["create"]
# Phase 27's namespace delete — irreversible and cascading, so granted
# explicitly rather than folded into anything broader.
- apiGroups: [""]
  resources: ["namespaces"]
  verbs: ["delete"]
{{- end -}}
