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
The Secret and key that hold the database URL: database.existingSecret, or
the release Secret rendered from database.url when no existing Secret is
named. One of the two must be set; the existing Secret wins.
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

{{/*
The Secret and key that hold the owner role's URL (docs/adr/0021 D2):
database.owner.existingSecret, or the release Secret rendered from
database.owner.url. One of the two must be set where the init container
migrates; the existing Secret wins.
*/}}
{{- define "cowork.ownerSecretName" -}}
{{- if .Values.database.owner.existingSecret }}
{{- .Values.database.owner.existingSecret }}
{{- else if .Values.database.owner.url }}
{{- printf "%s-database-owner" (include "cowork.fullname" .) }}
{{- else }}
{{- fail "set database.owner.existingSecret (preferred) or database.owner.url: the migrations run as the owner role" }}
{{- end }}
{{- end }}

{{- define "cowork.ownerSecretKey" -}}
{{- if .Values.database.owner.existingSecret }}
{{- .Values.database.owner.existingSecretKey }}
{{- else }}
{{- "databaseUrl" }}
{{- end }}
{{- end }}

{{/*
The Secret that holds the server key; there is no inline path.
*/}}
{{- define "cowork.sessionSecretName" -}}
{{- required "set session.existingSecret: a Secret whose key session.keys.key holds the server key (openssl rand -base64 32)" .Values.session.existingSecret }}
{{- end }}

{{/*
nginx's body limit: the larger backend limit rounded up to MiB plus one MiB of
headroom; a backend without a limit leaves nginx without one
(docs/adr/0039 D3).
*/}}
{{- define "cowork.nginxBodySize" -}}
{{- $json := int64 .Values.backend.config.maxJsonBody }}
{{- $upload := int64 .Values.backend.config.attachmentMaxBytes }}
{{- if or (eq $json 0) (eq $upload 0) }}
{{- "0" }}
{{- else }}
{{- $max := max $json $upload }}
{{- printf "%dm" (add1 (div (add $max 1048575) 1048576)) }}
{{- end }}
{{- end }}

{{/*
nginx's read timeout: the backend's request timeout plus ten seconds, so the
backend answers its own 504; an hour when the backend has none.
*/}}
{{- define "cowork.nginxReadTimeout" -}}
{{- $t := int64 .Values.backend.config.requestTimeout }}
{{- if eq $t 0 }}
{{- "3600s" }}
{{- else }}
{{- printf "%ds" (add $t 10) }}
{{- end }}
{{- end }}
