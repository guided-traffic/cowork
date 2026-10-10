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
The headless Service of the backend's metrics port: <fullname>-backend-metrics.
*/}}
{{- define "cowork.metrics.fullname" -}}
{{- printf "%s-backend-metrics" (include "cowork.fullname" .) | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
The metrics values (docs/adr/0060 D2, D3): .Values.metrics, or — where the
release's values carry no metrics block, as after `helm upgrade --reuse-values`
from a release before the metrics (docs/operations/installation.md#upgrade) —
the defaults of values.yaml, repeated here because a template cannot read them
then. A block that is there is taken as it is: Helm has merged the chart's
defaults under it already. Keep the literal equal to values.yaml's block;
ci/reuse-values-values.yaml renders the chart without the block. Use as
  {{- $metrics := include "cowork.metrics" . | fromYaml }}
*/}}
{{- define "cowork.metrics" -}}
{{- if .Values.metrics -}}
{{- toYaml .Values.metrics -}}
{{- else -}}
enabled: true
port: 8081
podMonitor:
  enabled: false
  labels: {}
  interval: 30s
  scrapeTimeout: 10s
serviceMonitor:
  enabled: false
  labels: {}
  interval: 30s
  scrapeTimeout: 10s
prometheusRule:
  enabled: false
  labels: {}
  alertLabels: {}
  restoreWindow: 24h
  exportMaxAgeDays: 7
grafanaDashboard:
  enabled: false
  labels:
    grafana_dashboard: "1"
  annotations: {}
{{- end -}}
{{- end }}

{{/*
The frontend's metrics values (docs/adr/0060 D7), with the defaults of
values.yaml where the release's values carry no frontend.metrics block, as
above.
*/}}
{{- define "cowork.frontendMetrics" -}}
{{- if (.Values.frontend | default dict).metrics -}}
{{- toYaml .Values.frontend.metrics -}}
{{- else -}}
exporter:
  enabled: false
  image:
    repository: nginx/nginx-prometheus-exporter
    tag: "1.5.3"
    pullPolicy: IfNotPresent
  port: 9113
  resources:
    limits:
      memory: 32Mi
    requests:
      cpu: 5m
      memory: 16Mi
{{- end -}}
{{- end }}

{{/*
Whether the backend's metrics listener is on (docs/adr/0060 D1): metrics.enabled,
on a port of its own. A monitoring resource that needs the listener fails
rendering without it.
*/}}
{{- define "cowork.metricsEnabled" -}}
{{- $metrics := include "cowork.metrics" . | fromYaml -}}
{{- if $metrics.enabled -}}
{{- if eq (int $metrics.port) (int .Values.backend.containerPort) -}}
{{- fail "metrics.port must differ from backend.containerPort: the metrics are never served on the API's listener (docs/adr/0060 D1)" -}}
{{- end -}}
true
{{- end -}}
{{- end }}

{{/*
The labels of one alert: its severity, under metrics.prometheusRule.alertLabels,
which win. Call with (dict "root" . "severity" "warning").
*/}}
{{- define "cowork.alertLabels" -}}
{{- toYaml (merge (dict) (deepCopy ((include "cowork.metrics" .root | fromYaml).prometheusRule.alertLabels | default dict)) (dict "severity" .severity)) }}
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
Where one role's connection comes from (docs/adr/0058 D3, D4), as JSON. Call
with (dict "root" . "owner" false) for the runtime role, true for the owner
role (docs/adr/0021 D2).

  {"mode": "url", "secret": <name>, "key": <key>}
    the URL from database.existingSecret under keys.url — or under
    existingSecretKey, its earlier name, which wins while it is set — or, with
    no existing Secret, from the release Secret database.url renders;
  {"mode": "components", "secret": <name>, "configMap": <name>, "keys": {...}}
    keys.url empty, whatever existingSecretKey says: the user and the password
    from the Secret, the host, the port, the name and the sslmode from
    existingConfigMap when it is named and from the Secret otherwise; port and
    sslmode are read only when their key is named.

The existing Secret wins over the inline URL. A key block missing from the
values — an upgrade that reuses the values of a release before it — reads as
the defaults of values.yaml.
*/}}
{{- define "cowork.databaseSource" -}}
{{- $root := .root -}}
{{- $ref := ternary $root.Values.database.owner $root.Values.database .owner -}}
{{- $at := ternary "database.owner" "database" .owner -}}
{{- $keys := $ref.keys | default dict -}}
{{- $configMap := $ref.existingConfigMap | default "" -}}
{{- if $ref.existingSecret -}}
{{- $urlKey := dig "url" "databaseUrl" $keys -}}
{{- if $urlKey -}}
{{- $urlKey = $ref.existingSecretKey | default $urlKey -}}
{{- if $configMap -}}
{{- fail (printf "%s.existingConfigMap is read with the components only: set %s.keys.url to empty to read them" $at $at) -}}
{{- end -}}
{{- dict "mode" "url" "secret" $ref.existingSecret "key" $urlKey | toJson -}}
{{- else -}}
{{- $components := dict -}}
{{- range $c, $default := dict "host" "host" "port" "port" "name" "dbname" "user" "username" "password" "password" "sslmode" "" -}}
{{- $_ := set $components $c (dig $c $default $keys) -}}
{{- end -}}
{{- range $c := list "host" "name" "user" "password" -}}
{{- if not (index $components $c) -}}
{{- fail (printf "set %s.keys.%s: with %s.keys.url empty the connection is read as components, and it needs the host, the name, the user and the password" $at $c $at) -}}
{{- end -}}
{{- end -}}
{{- dict "mode" "components" "secret" $ref.existingSecret "configMap" $configMap "keys" $components | toJson -}}
{{- end -}}
{{- else if $ref.url -}}
{{- if $configMap -}}
{{- fail (printf "%s.existingConfigMap is read with the components of %s.existingSecret only, not with %s.url" $at $at $at) -}}
{{- end -}}
{{- dict "mode" "url" "secret" (printf "%s-%s" (include "cowork.fullname" $root) (ternary "database-owner" "database" .owner)) "key" "databaseUrl" | toJson -}}
{{- else if .owner -}}
{{- fail "set database.owner.existingSecret (preferred) or database.owner.url: the migrations run as the owner role" -}}
{{- else -}}
{{- fail "set database.existingSecret (preferred) or database.url" -}}
{{- end -}}
{{- end }}

{{/*
The environment of one role's connection, for nindent: COWORK_DATABASE_URL
(COWORK_DATABASE_OWNER_URL) from a Secret, or the component variables of
docs/adr/0058 D4 — the location from the ConfigMap when one is named, the
user and the password always from the Secret. Call as cowork.databaseSource.
*/}}
{{- define "cowork.databaseEnv" -}}
{{- include "cowork.databaseEnvEntries" . | trim -}}
{{- end }}

{{- define "cowork.databaseEnvEntries" -}}
{{- /* Under helm lint a fail of cowork.databaseSource is an info line and the
source is empty: then nothing is rendered. */ -}}
{{- $src := include "cowork.databaseSource" . | fromJson -}}
{{- $mode := toString $src.mode -}}
{{- $prefix := ternary "COWORK_DATABASE_OWNER" "COWORK_DATABASE" .owner -}}
{{- if eq $mode "url" -}}
- name: {{ $prefix }}_URL
  valueFrom:
    secretKeyRef:
      name: {{ $src.secret }}
      key: {{ $src.key }}
{{- else if eq $mode "components" -}}
{{- range $c := list "host" "port" "name" "user" "password" "sslmode" }}
{{- with index $src.keys $c }}
- name: {{ $prefix }}_{{ upper $c }}
  valueFrom:
    {{- if and $src.configMap (has $c (list "host" "port" "name" "sslmode")) }}
    configMapKeyRef:
      name: {{ $src.configMap }}
      key: {{ . }}
    {{- else }}
    secretKeyRef:
      name: {{ $src.secret }}
      key: {{ . }}
    {{- end }}
{{- end }}
{{- end }}
{{- end -}}
{{- end }}

{{/*
The migration mode (docs/adr/0057 D1, D2): onStart, the migrate init
container of every backend pod, or job, a Helm hook Job before every install
and upgrade. A values tree without the block — an upgrade that reuses the
values of a release before it — is onStart.
*/}}
{{- define "cowork.migrationsMode" -}}
{{- $mode := (.Values.migrations | default dict).mode | default "onStart" -}}
{{- if not (has $mode (list "onStart" "job")) -}}
{{- fail (printf "migrations.mode must be onStart or job, not %q" $mode) -}}
{{- end -}}
{{- $mode -}}
{{- end }}

{{/*
Whether the backend pods migrate in their init container: migrations.mode
onStart with backend.config.migrateOnStart true, the default. With
migrateOnStart false in onStart mode nothing in the release migrates.
*/}}
{{- define "cowork.initContainerMigrates" -}}
{{- if and (eq (include "cowork.migrationsMode" .) "onStart") .Values.backend.config.migrateOnStart -}}
true
{{- end -}}
{{- end }}

{{/*
What the migration Job reads must exist before the release does: a
pre-install hook runs before the Secrets the chart renders from inline values,
and a pre-upgrade hook before a changed inline value reaches its Secret. So
job mode takes Secret references only, for each credential the Job reads.
*/}}
{{- define "cowork.jobReferencesOnly" -}}
{{- $why := "the migration Job runs before the release creates or updates its own Secrets (a pre-install and pre-upgrade hook), so it takes no inline value" -}}
{{- if not .Values.database.existingSecret -}}
{{- fail (printf "migrations.mode job: set database.existingSecret; %s such as database.url" $why) -}}
{{- end -}}
{{- if not .Values.database.owner.existingSecret -}}
{{- fail (printf "migrations.mode job: set database.owner.existingSecret; %s such as database.owner.url" $why) -}}
{{- end -}}
{{- if and (include "cowork.localAdminEnabled" .) (not .Values.localAdmin.existingSecret) -}}
{{- fail (printf "migrations.mode job: set localAdmin.existingSecret, which the Job reads for the bootstrap (docs/adr/0057 D4); %s such as localAdmin.username and localAdmin.password" $why) -}}
{{- end -}}
{{- end }}

{{/*
The ConfigMap with the private authority the database server's certificate
chains to (docs/adr/0058 D3), and the key of its PEM: mounted read-only into
the migration run and the serving container and named by COWORK_DATABASE_CA,
which the backend sets as the sslrootcert of both roles' connections. A values
tree without the block — an upgrade that reuses the values of a release
before it — names none.
*/}}
{{- define "cowork.databaseCAConfigMap" -}}
{{- (.Values.database.tls | default dict).caConfigMap | default "" -}}
{{- end }}

{{- define "cowork.databaseCAKey" -}}
{{- dig "keys" "ca" "" (.Values.database.tls | default dict) | default "ca.crt" -}}
{{- end }}

{{/*
Whether the object storage is configured (docs/adr/0016 D1, docs/adr/0058
D3): a literal storage.endpoint, or storage.existingConfigMap, whose values
the chart cannot see. Without either the backend refuses uploads.
*/}}
{{- define "cowork.storageEnabled" -}}
{{- if or .Values.storage.endpoint .Values.storage.existingConfigMap -}}
true
{{- end -}}
{{- end }}

{{/*
The Secret that holds the server key; there is no inline path.
*/}}
{{- define "cowork.sessionSecretName" -}}
{{- required "set session.existingSecret: a Secret whose key session.keys.key holds the server key (openssl rand -base64 32)" .Values.session.existingSecret }}
{{- end }}

{{/*
The smallest body limit the Ingress controller may have, for the notes: the
largest backend limit — a JSON body, an attachment, an import's upload —
rounded up to MiB plus one MiB of headroom, so the backend answers its own
413; "0", no limit, when a backend limit is off (docs/adr/0039 D3,
docs/adr/0051 D7). The chart sets no controller's limit: it does not know the
controller.
*/}}
{{- define "cowork.ingressBodySize" -}}
{{- $json := int64 .Values.backend.config.maxJsonBody }}
{{- $upload := int64 .Values.backend.config.attachmentMaxBytes }}
{{- $import := int64 .Values.backend.config.maxImportBytes }}
{{- if or (eq $json 0) (eq $upload 0) (eq $import 0) }}
{{- "0" }}
{{- else }}
{{- $max := max $json $upload $import }}
{{- printf "%dm" (add1 (div (add $max 1048575) 1048576)) }}
{{- end }}
{{- end }}

{{/*
The smallest read timeout the Ingress controller may have, for the notes: the
backend's request timeout plus ten seconds, so the backend answers its own 504;
an hour when the backend has none. The two streams send something every twenty
seconds at the most, so they stay open within it.
*/}}
{{- define "cowork.ingressReadTimeout" -}}
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
The bootstrap team's slug and name as YAML, slug and name (docs/adr/0032 D6):
bootstrap.team, or bootstrap.tenant, its name before, which the chart reads for
one release where bootstrap.team leaves a value empty (docs/adr/0005 D1). The
two set to different values fail, naming both. A release upgraded with
--reuse-values from a chart without bootstrap.team reads bootstrap.tenant.
*/}}
{{- define "cowork.bootstrapTeam" -}}
{{- $bootstrap := .Values.bootstrap | default dict -}}
{{- $team := $bootstrap.team | default dict -}}
{{- $tenant := $bootstrap.tenant | default dict -}}
{{- $out := dict -}}
{{- range $key := list "slug" "name" -}}
{{- $now := get $team $key | default "" | toString -}}
{{- $before := get $tenant $key | default "" | toString -}}
{{- if and $now $before (ne $now $before) -}}
{{- fail (printf "bootstrap.team.%s and bootstrap.tenant.%s are both set and differ: bootstrap.tenant is the name before of bootstrap.team, read for one release (docs/adr/0005 D1); set bootstrap.team.%s alone" $key $key $key) -}}
{{- end -}}
{{- $_ := set $out $key (coalesce $now $before "") -}}
{{- end -}}
{{- toYaml $out -}}
{{- end }}

{{/*
What one team's attachments may hold together, in bytes, 0 for no quota
(docs/adr/0016 D6): backend.config.attachmentTeamQuota, or
attachmentTenantQuota, its name before, which the chart reads for one release
while attachmentTeamQuota is 0 or absent (docs/adr/0005 D1). The two set to
different values other than 0 fail, naming both.
*/}}
{{- define "cowork.attachmentTeamQuota" -}}
{{- $config := .Values.backend.config | default dict -}}
{{- $now := get $config "attachmentTeamQuota" | default 0 | int64 -}}
{{- $before := get $config "attachmentTenantQuota" | default 0 | int64 -}}
{{- if and $now $before (ne $now $before) -}}
{{- fail "backend.config.attachmentTeamQuota and backend.config.attachmentTenantQuota are both set and differ: attachmentTenantQuota is the name before of attachmentTeamQuota, read for one release (docs/adr/0005 D1); set attachmentTeamQuota alone" -}}
{{- end -}}
{{- if $now -}}
{{- $now -}}
{{- else -}}
{{- $before -}}
{{- end -}}
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
