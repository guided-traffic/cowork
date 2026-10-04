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

{{/*
Whether the local administrator is configured (docs/adr/0032 D1): an existing
Secret, or the inline username and password, which come together.
*/}}
{{- define "cowork.localAdminEnabled" -}}
{{- if or .Values.localAdmin.existingSecret .Values.localAdmin.username .Values.localAdmin.password -}}
{{- if and (not .Values.localAdmin.existingSecret) (not (and .Values.localAdmin.username .Values.localAdmin.password)) -}}
{{- fail "set localAdmin.username and localAdmin.password together, or localAdmin.existingSecret" -}}
{{- end -}}
{{- if not .Values.backend.config.baseURL -}}
{{- fail "set backend.config.baseURL: a cookie login needs the origin the browser sees (docs/adr/0037 D6)" -}}
{{- end -}}
true
{{- end -}}
{{- end }}

{{/*
The Secret and keys that hold the local administrator: localAdmin.existingSecret,
or the release Secret rendered from the inline values. The existing Secret wins.
*/}}
{{- define "cowork.localAdminSecretName" -}}
{{- if .Values.localAdmin.existingSecret }}
{{- .Values.localAdmin.existingSecret }}
{{- else }}
{{- printf "%s-local-admin" (include "cowork.fullname" .) }}
{{- end }}
{{- end }}

{{- define "cowork.localAdminUsernameKey" -}}
{{- if .Values.localAdmin.existingSecret }}
{{- .Values.localAdmin.keys.username }}
{{- else }}
{{- "username" }}
{{- end }}
{{- end }}

{{- define "cowork.localAdminPasswordKey" -}}
{{- if .Values.localAdmin.existingSecret }}
{{- .Values.localAdmin.keys.password }}
{{- else }}
{{- "password" }}
{{- end }}
{{- end }}

{{/*
Whether the login through an identity provider is configured (docs/adr/0029
D4): auth.oidc.issuer is set. It needs the public URL for the redirect URI, a
client id and a client secret. Without the issuer the other auth.oidc values
are not rendered, so emptying the issuer alone switches the provider off. A
group name with a comma would split in COWORK_OIDC_ALLOWED_GROUPS and admit a
group nobody listed.
*/}}
{{- define "cowork.oidcEnabled" -}}
{{- $oidc := .Values.auth.oidc -}}
{{- if $oidc.issuer -}}
{{- if not .Values.backend.config.baseURL -}}
{{- fail "set backend.config.baseURL: the identity provider redirects to <baseURL>/auth/callback (docs/adr/0029 D4)" -}}
{{- end -}}
{{- if not (or $oidc.clientId (and $oidc.existingSecret $oidc.keys.clientId)) -}}
{{- fail "set auth.oidc.clientId, or auth.oidc.existingSecret with auth.oidc.keys.clientId" -}}
{{- end -}}
{{- if not $oidc.existingSecret -}}
{{- fail "set auth.oidc.existingSecret: the client secret comes from a Secret only, never from the values (docs/adr/0058 D3)" -}}
{{- end -}}
{{- range toStrings $oidc.allowedGroups -}}
{{- if contains "," . -}}
{{- fail (printf "auth.oidc.allowedGroups: %q holds a comma, which separates the groups in COWORK_OIDC_ALLOWED_GROUPS and cannot be part of one" .) -}}
{{- end -}}
{{- end -}}
true
{{- end -}}
{{- end }}

{{/*
The key of auth.oidc.existingSecret that holds the client secret
(docs/adr/0058 D3).
*/}}
{{- define "cowork.oidcClientSecretKey" -}}
{{- .Values.auth.oidc.keys.clientSecret }}
{{- end }}

{{/*
Whether the chat is configured (docs/adr/0076): chat.providers holds at least
one provider. Each needs an id the backend takes — the variables are named
after it —, a kind the backend speaks, its URL and its model, and for
anthropic its key, which comes from a Secret of its own and never from the
values (docs/adr/0058 D3). Without a provider the limits are not rendered, so
emptying the list alone switches the chat off; the backend refuses to start on
a chat variable without it.
*/}}
{{- define "cowork.chatEnabled" -}}
{{- $providers := .Values.chat.providers | default list -}}
{{- if not (kindIs "slice" $providers) -}}
{{- fail "chat.providers must be a list of providers, each with id, kind, url and model" -}}
{{- end -}}
{{- $seen := dict -}}
{{- range $i, $p := $providers -}}
{{- $at := printf "chat.providers[%d]" $i -}}
{{- if not (kindIs "map" $p) -}}
{{- fail (printf "%s must be a provider with id, kind, url and model" $at) -}}
{{- end -}}
{{- $id := toString ($p.id | default "") -}}
{{- if not (regexMatch "^[a-z0-9]([a-z0-9-]{0,30}[a-z0-9])?$" $id) -}}
{{- fail (printf "%s.id must be 1 to 32 lowercase letters, digits and dashes, a dash neither first nor last" $at) -}}
{{- end -}}
{{- if hasKey $seen $id -}}
{{- fail (printf "%s.id: %q names two providers" $at $id) -}}
{{- end -}}
{{- $_ := set $seen $id true -}}
{{- $kind := lower (toString ($p.kind | default "")) -}}
{{- if not (has $kind (list "openai" "anthropic")) -}}
{{- fail (printf "%s.kind must be openai or anthropic" $at) -}}
{{- end -}}
{{- if not $p.url -}}
{{- fail (printf "set %s.url: the provider's base URL, e.g. https://api.anthropic.com or http://ollama.ai.svc:11434/v1" $at) -}}
{{- end -}}
{{- if not $p.model -}}
{{- fail (printf "set %s.model: the model by the name its provider knows it" $at) -}}
{{- end -}}
{{- if hasKey $p "apiKey" -}}
{{- fail (printf "%s.apiKey: a provider's key comes from a Secret only, never from the values — set %s.existingSecret (docs/adr/0058 D3)" $at $at) -}}
{{- end -}}
{{- if and (eq $kind "anthropic") (not $p.existingSecret) -}}
{{- fail (printf "set %s.existingSecret: anthropic needs an API key, and the key comes from a Secret only, never from the values (docs/adr/0058 D3)" $at) -}}
{{- end -}}
{{- end -}}
{{- if $providers -}}
true
{{- end -}}
{{- end }}

{{/*
COWORK_CHAT_PROVIDERS: the providers' ids in their order.
*/}}
{{- define "cowork.chatProviderIds" -}}
{{- $ids := list -}}
{{- range .Values.chat.providers }}{{ $ids = append $ids (toString .id) }}{{ end -}}
{{- join "," $ids -}}
{{- end }}

{{/*
The variable prefix of a chat provider: COWORK_CHAT_ and the id upper-cased,
its dashes as underscores (config.ChatEnv).
*/}}
{{- define "cowork.chatEnv" -}}
{{- printf "COWORK_CHAT_%s" (upper (replace "-" "_" (toString .))) -}}
{{- end }}
