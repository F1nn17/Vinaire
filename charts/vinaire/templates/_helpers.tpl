{{/*
Chart name, truncated and DNS-1123-safe.
*/}}
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
