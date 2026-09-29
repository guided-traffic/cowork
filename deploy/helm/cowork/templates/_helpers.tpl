{{/*
Expand the name of the chart.
*/}}
{{- define "cowork.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Create a default fully qualified app name (63 chars max, DNS naming).
*/}}
{{- define "cowork.fullname" -}}
{{- if .Values.fullnameOverride }}
{{- .Values.fullnameOverride | trunc 63 | trimSuffix "-" }}
{{- else }}
{{- $name := default .Chart.Name .Values.nameOverride }}
{{- if contains $name .Release.Name }}
{{- .Release.Name | trunc 63 | trimSuffix "-" }}
{{- else }}
{{- printf "%s-%s" .Release.Name $name | trunc 63 | trimSuffix "-" }}
{{- end }}
{{- end }}
{{- end }}

{{/*
Component names: <fullname>-backend and <fullname>-frontend.
*/}}
{{- define "cowork.backend.fullname" -}}
{{- printf "%s-backend" (include "cowork.fullname" .) | trunc 63 | trimSuffix "-" }}
{{- end }}

{{- define "cowork.frontend.fullname" -}}
{{- printf "%s-frontend" (include "cowork.fullname" .) | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Create chart name and version as used by the chart label.
*/}}
{{- define "cowork.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Common labels, without the component.
*/}}
{{- define "cowork.labels" -}}
helm.sh/chart: {{ include "cowork.chart" . }}
app.kubernetes.io/name: {{ include "cowork.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- if .Chart.AppVersion }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end }}

{{/*
Selector labels for one component. Call with (dict "root" . "component" "backend").
*/}}
{{- define "cowork.componentSelectorLabels" -}}
app.kubernetes.io/name: {{ include "cowork.name" .root }}
app.kubernetes.io/instance: {{ .root.Release.Name }}
app.kubernetes.io/component: {{ .component }}
{{- end }}

{{/*
Create the name of the service account to use
*/}}
{{- define "cowork.serviceAccountName" -}}
{{- if .Values.serviceAccount.create }}
{{- default (include "cowork.fullname" .) .Values.serviceAccount.name }}
{{- else }}
{{- default "default" .Values.serviceAccount.name }}
{{- end }}
{{- end }}

{{/*
The Secret and key that hold the database URL. Exactly one of
database.existingSecret and database.url must be set.
*/}}
{{- define "cowork.databaseSecretName" -}}
{{- if .Values.database.existingSecret }}
{{- .Values.database.existingSecret }}
{{- else if .Values.database.url }}
{{- printf "%s-database" (include "cowork.fullname" .) }}
{{- else }}
{{- fail "set database.existingSecret (preferred) or database.url" }}
{{- end }}
{{- end }}

{{- define "cowork.databaseSecretKey" -}}
{{- if .Values.database.existingSecret }}
{{- .Values.database.existingSecretKey }}
{{- else }}
{{- "databaseUrl" }}
{{- end }}
{{- end }}

{{/*
The URL the frontend proxies /api/ to: the backend Service inside the cluster.
*/}}
{{- define "cowork.backendURL" -}}
{{- printf "http://%s:%d" (include "cowork.backend.fullname" .) (int .Values.backend.service.port) }}
{{- end }}
