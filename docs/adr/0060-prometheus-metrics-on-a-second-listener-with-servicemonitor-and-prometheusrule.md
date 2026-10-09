# ADR 0060: Prometheus Metrics on a Second Backend Listener Without Authentication, Not in the Service, With `ServiceMonitor` and `PrometheusRule` in the Chart — OpenTelemetry When Tracing Comes

## Status

Accepted, amended 2026-10-03 (D2: the chart's backend NetworkPolicy), amended 2026-10-04 by the
owner's decision on the routing recorded in
[ADR 0001](0001-two-containers-a-go-backend-and-an-nginx-frontend-installed-by-one-helm-chart.md)
D3 (D1: the Ingress, not nginx, routes `/api/`; D2: the chart ships no NetworkPolicy, so the rule
for the metrics port is the installation's again), amended 2026-10-06 for what was built (below:
D1–D7 but for the `consistency` family; what the record left open is made concrete in place, in
D1, D3, D4, D6 and D7, by the implementer, open to the owner's objection), amended 2026-10-06 a
second time for the `consistency` family's two counts and their alert, built with the check (D4,
D5, D6, made concrete in place the same way). Date: 2026-10-01. Decided
by the owner as the answer to the catalog question "metrics?": Prometheus on a separate port with `client_golang`, together with the
kube-prometheus custom resources (`ServiceMonitor`/`PodMonitor`, `PrometheusRule`) rendered
by the chart, over metrics on the main port behind authentication, over OpenTelemetry push,
and over OpenTelemetry instruments with a Prometheus exporter. The rules of D5–D8 were put to
the owner with the question and not objected to.

~~**Not built.** No metrics listener, no instruments, no monitoring resources in the chart.~~

**Built** (2026-10-06), but for the `consistency` family of D4 and its two alerts of D6 —
consistency counts above zero, the last export too old —, which come with the consistency check of
[ADR 0059](0059-backups-belong-to-the-operators-cowork-provides-the-export-and-makes-a-restores-inconsistency-visible.md)
D4 that sets them:

- **D1** — the registry and every instrument in [`internal/metrics`](../../backend/internal/metrics/metrics.go),
  `client_golang` v1.24.1, a registry of its own and never the library's default, which no other
  package imports (`TestOnlyThisPackageImportsTheClientLibrary`); the listener's handler
  `httpserver.NewMetrics` and the one lifecycle of both listeners `httpserver.ServeAll`
  ([`server.go`](../../backend/internal/httpserver/server.go)); `serve` and `bind` in
  [`main.go`](../../backend/cmd/cowork/main.go) bind both before either serves.
- **D2, D3** — `metrics.*` in the chart: the container port `metrics` on the backend pods and on no
  Service; [`podmonitor.yaml`](../../deploy/helm/cowork/templates/podmonitor.yaml),
  [`servicemonitor.yaml`](../../deploy/helm/cowork/templates/servicemonitor.yaml) with its headless
  Service, [`prometheusrule.yaml`](../../deploy/helm/cowork/templates/prometheusrule.yaml) and
  [`grafana-dashboard.yaml`](../../deploy/helm/cowork/templates/grafana-dashboard.yaml), each behind its
  switch, all rendered by `ci/metrics-values.yaml`.
- **D4, D5** — every instrument of the table but `consistency`, recorded by the HTTP pipeline, the
  store, the hub and the API; the reference with every name and label is
  [README.md, Metrics](../../README.md#metrics). A unit test walks every family and fails on a label
  named for a person, a ticket, a key, a token or a request id, and on `tenant` outside a
  `cowork_consistency_` family (`TestNoInstrumentCarriesAForbiddenLabel`); the route label is the
  document's pattern (`TestTheRouteLabelIsTheDocumentsPattern`).
- **D6** — four alerts, each linking its section of [docs/operations/metrics.md](../operations/metrics.md).
- **D7** — `stub_status` on `127.0.0.1:8082` in [`default.conf`](../../frontend/nginx/default.conf),
  `frontend.metrics.exporter.*` in the chart; run against the built image on 2026-10-06.
- The integration tier runs the built binary with both listeners and scrapes it
  (`TestServeAnswersAScrapeOnItsMetricsListener`). Not run: a Prometheus or a Grafana reading the
  resources, and the chart in a cluster with the Prometheus Operator — `helm lint` and
  `helm template` render them, nothing more.

**Built** (2026-10-06, with the consistency check of
[ADR 0059](0059-backups-belong-to-the-operators-cowork-provides-the-export-and-makes-a-restores-inconsistency-visible.md)
D4): the `consistency` family's two counts, `cowork_consistency_dangling_attachments` and
`cowork_consistency_orphaned_objects`, the dashboard's row for them and the alert
`CoworkAttachmentsOutOfStep`; made concrete in D4, D5 and D6 by the implementer, open to the owner's
objection. The seconds since the last export and the alert on its age come with the export, which
is not part of it. `TestNoInstrumentCarriesAForbiddenLabel` admits the tenant on those two families
alone, as an id, and holds them to that one label; the integration tier scrapes the counts of a
check through a second store (`TestTheConsistencyCheckFindsWhatARestoreLeftAndTheAdministratorSettlesIt`);
`promtool` of Prometheus 3.5.0 parsed the rendered rules and fired the alert in a rule test after the
window, for a tenant out of step on two replicas and not for a clean one. Not run: a Prometheus
scraping a release.

## Context

Several records promised numbers: the event stream's open streams, drops and replays
([ADR 0054](0054-server-sent-events-per-tenant-carry-keys-not-content-polling-is-the-fallback.md)),
the consistency check's dangling metadata and orphans and the last export time
([ADR 0059](0059-backups-belong-to-the-operators-cowork-provides-the-export-and-makes-a-restores-inconsistency-visible.md)
D4), the pool and a tracer slot ([ADR 0027](0027-data-access-is-sqlc-over-pgx-behind-a-tenant-transaction-and-a-mutation-wrapper.md)
D8), the per-token view of the audit ([ADR 0026](0026-one-append-only-audit-table-written-by-the-request-layer.md)).
The API port is ~~proxied by nginx~~ *(since 2026-10-04 routed by the Ingress)* under `/api/`, so
a metrics path on it would be one proxy mistake away from the internet; a second listener that the Service does not expose is the
pattern the sibling operator uses and every Prometheus scrapes. The owner runs
kube-prometheus and wants the resources that wire scraping and alerting rendered by the chart.

## Decision

**D1 — Metrics are Prometheus text on a second backend listener,** `COWORK_METRICS_ADDR`
(default `:8081`), path `/metrics`, no authentication, implemented with `client_golang`. The
listener shares the lifecycle of the API listener and is never ~~proxied by nginx~~ *(amended
2026-10-04: routed by the Ingress)*. The health probes stay on `:8080`. *(Made concrete
2026-10-06 by the implementer, open to the owner's objection: the variable set to an empty value
switches the listener off — the one variable where empty is not unset, so that an installation that
scrapes nothing opens no unauthenticated port, and the chart renders it so with
`metrics.enabled: false` —; a value is `host:port` and must differ from `COWORK_LISTEN_ADDR`, or
the start is refused. Off, nothing is recorded. Both listeners are bound before either serves, so a
taken port refuses the start; a signal shuts both down within `COWORK_SHUTDOWN_TIMEOUT`, and one
that fails stops the other. The metrics listener answers `/metrics` and nothing else, writes no
request log, serves at most four scrapes at once, and is no route of the HTTP instruments.)*

**D2 — The chart exposes the port on the pod, not on the Service.** `metrics.enabled`
(default `true`) adds the container port `metrics`; the backend Service does not list it. The
operations page names the NetworkPolicy an installation should add: port 8081 reachable from
the monitoring namespace only. ~~*(Amended 2026-10-03: the chart now renders a NetworkPolicy of its
own for the backend pods, which admits the frontend's pods on the API port and nothing else
([ADR 0001](0001-two-containers-a-go-backend-and-an-nginx-frontend-installed-by-one-helm-chart.md)
D3). Policies add up, so the rule for port 8081 is a policy beside it — and with the chart's in
place it is needed, not only advisable: a scrape of the metrics port is refused where policies
are enforced until the monitoring namespace is admitted.)*~~ *(Amended 2026-10-04 by the owner,
ADR 0001 D3: the chart ships no NetworkPolicy — network policies are the cluster administrator's —,
so the rule for port 8081 is the installation's policy, as this decision first said.)*

**D3 — The chart renders the kube-prometheus resources, each behind a switch, default off:**

| Resource | Value | Content |
|---|---|---|
| `PodMonitor` | `metrics.podMonitor.enabled` | scrapes the `metrics` port of the backend pods; labels and interval configurable; `PodMonitor` rather than `ServiceMonitor` because the port is not on the Service |
| `PrometheusRule` | `metrics.prometheusRule.enabled` | the alerts of D6, with configurable labels for routing |
| Grafana dashboard `ConfigMap` | `metrics.grafanaDashboard.enabled` | one dashboard JSON with the sidecar label, covering the metrics of D4 |

A `ServiceMonitor` variant exists for installations whose Prometheus discovers only Services
(`metrics.serviceMonitor.enabled`), which then adds a separate headless Service for the
metrics port alone, so the API Service still never carries it. *(Made concrete 2026-10-06 by the
implementer, open to the owner's objection: the port is `metrics.port`, 8081, and must differ from
`backend.containerPort`; a monitor's labels, interval and scrape timeout are values, the
PrometheusRule's own labels and the labels every alert gets for the routing —
`metrics.prometheusRule.alertLabels`, whose `severity` replaces an alert's — too; the
`ServiceMonitor` variant scrapes the backend alone, the nginx exporter is the `PodMonitor`'s; a
resource that needs the listener fails rendering without `metrics.enabled`. The dashboard's
ConfigMap carries `grafana_dashboard: "1"` by default — kube-prometheus-stack's sidecar label — and
its JSON is generated from [`internal/metrics/dashboard.go`](../../backend/internal/metrics/dashboard.go)
into `deploy/helm/cowork/files/` by `make generate`, which `make generate-check` holds.)*

**D4 — The instruments of the first release,** named `cowork_<subsystem>_<name>_<unit>`:

| Subsystem | Instruments |
|---|---|
| `http` | requests total by route pattern, method and status; latency histogram by route; in-flight |
| `db` | pool connections (idle, in use, waiting), acquire latency, query errors by kind |
| `jobs` | runs, duration and failures by job (purge, cleanup, consistency, idempotency expiry) |
| `events` | open streams, events published, subscribers dropped, replay hits and misses |
| `consistency` | dangling attachment metadata, orphaned objects, seconds since last export — the only instruments with a `tenant` label |
| `audit` | acts total by action and by actor kind (person, agent, system) |
| `auth` | logins by method and outcome, lockouts, token refusals by reason |
| `migrations` | current schema version, dirty flag |
| Go runtime and process | the `client_golang` defaults |

*(Made concrete 2026-10-06 by the implementer, open to the owner's objection; the names and labels
are [README.md, Metrics](../../README.md#metrics):)* the latency histogram is by route **and
method**, an operation being both, on the library's default buckets; a request no route matches is
`route="unmatched"`, and a method HTTP does not define is `other`, so no client chooses a label
value; the in-flight gauge counts the open event streams and turns of the chat, and a turn's tool
calls are requests of their own. pgx tells no number of acquires waiting at a moment: the pool's
"waiting" is the acquires that had to wait for a connection and the time they waited
(`cowork_db_pool_wait_duration_seconds`, whose rate is the mean number waiting), and the acquire
latency a count and a sum, read from the pool at the scrape; a query error's kind is a closed set
of SQLSTATE meanings and client causes. The jobs' label is `name`, never `job`, which Prometheus
gives the target and would rename the backend's to `exported_job`; a job is every `RunJob` — the
five hourly ones and the bootstrap —, a run is one that took the job's lock or failed before it,
and `cowork_jobs_consecutive_failures` is what D6's alert reads. The events' drops are by reason —
`behind` (D4 of [ADR 0054](0054-server-sent-events-per-tenant-carry-keys-not-content-polling-is-the-fallback.md)),
`limit` (its D8), `resync` (the listener's recovery) — and a shutdown is none. An act counts once
its transaction committed; its actor is `system` for a system actor's, `agent` for an act marked
as an agent's, `person` otherwise. A login is `local` or `oidc` and ends `success`, `failure`,
`locked`, `throttled` or `refused`; a token is refused `malformed`, `unknown`, `revoked`,
`expired`, `not_allowed` or `session_only`. The schema's version and dirty flag are read at a
scrape, at most once every ten seconds and within two, so a migration of a newer release that
failed halfway beside a serving pod shows. *(Made concrete 2026-10-06 for the `consistency`
family, by the implementer, open to the owner's objection:)* two gauges,
`cowork_consistency_dangling_attachments` — the files of a tenant whose bytes are missing and whose
loss nobody accepted — and `cowork_consistency_orphaned_objects`, per tenant, as its latest check
left them; read from the stored results at a scrape, at most once a minute and within two seconds,
not set in the process that ran the check, so that every replica answers the same counts and one
that starts answers them at once. A tenant without a result has no series.

**D5 — Cardinality discipline.** No label carries a person, a ticket key, a token or a
request id; route labels are the pattern (`/tenants/{slug}/projects/{KEY}/tickets/{number}`),
never the instance; the `tenant` label appears on the `consistency` family only. *(Made concrete
2026-10-06 by the implementer; confirmed by the owner 2026-10-09, over the slug:)* its value is the tenant's id, never
its slug: the listener has no authentication, a slug names a client, and the list of the clients is
a session's view even for a global administrator
([ADR 0035](0035-personal-access-tokens.md) D5). The scrape then tells how many tenants have a result
and each one's counts by an id, which [docs/security/metrics.md](../security/metrics.md) says.

**D6 — The alerts of the first release:** schema dirty; consistency counts above zero for
longer than the restore window; last export older than a configurable number of days;
sustained event-stream drops; pool exhaustion sustained; a background job failing twice in
a row. Each alert's annotation links to the operations page's section for it. *(Made concrete
2026-10-06 by the implementer, open to the owner's objection; the two of the consistency family come
with it:)* `CoworkSchemaDirty`, critical, the dirty flag for ten minutes; `CoworkEventStreamDrops`,
warning, a stream dropped `behind` in every ten minutes for fifteen; `CoworkDatabasePoolExhausted`,
warning, every connection in use while acquires wait, for ten minutes; `CoworkJobFailing`, warning,
two failures of a job in a row on one replica. Each is narrowed to the release's backend pods by
namespace and pod name; the thresholds are the template's, not values; `runbook_url` is
[docs/operations/metrics.md](../operations/metrics.md) on GitHub at the tag of the chart's
`appVersion`, so a runbook speaks of the release installed. *(Made concrete 2026-10-06 for the
consistency counts, by the implementer, open to the owner's objection:)* `CoworkAttachmentsOutOfStep`,
warning, per tenant, the two counts of its latest check above zero together for
`metrics.prometheusRule.restoreWindow`, a day by default — the one threshold that is a value,
because how long a restore takes to settle is the installation's; an accepted loss counts in
neither, so the alert ends with an acceptance, a removal or bytes put back. The alert on the last
export's age comes with the export.

**D7 — nginx metrics are opt-in.** `stub_status` on `127.0.0.1` inside the frontend
container; `frontend.metrics.exporter.enabled` adds the nginx exporter sidecar and the
corresponding `PodMonitor` entry; default off. *(Made concrete 2026-10-06 by the implementer, open to
the owner's objection: `stub_status` is on `127.0.0.1:8082` in every image — the configuration is a
plain file, and nothing outside the pod reaches the loopback address —; the sidecar is
`nginx/nginx-prometheus-exporter` 1.5.3, NGINX's own, on the container port `nginx-metrics`, 9113,
run as the pod's user with a read-only root filesystem and no probe; it counts toward the pod's
readiness like any container, so one that cannot start keeps the frontend pod out of its Service.)*

**D8 — OpenTelemetry is the tracing record's.** When traces come, the OpenTelemetry SDK is
introduced for them; whether metrics then move to it is decided there, not here.

## Consequences

- Every number an earlier record promised has a name and a scrape path; the owner's
  kube-prometheus wires it with two values.
- A second listener in the backend with its own shutdown; the metrics port stays off the
  Service, so the proxy never sees it.
- Three optional CRD-dependent templates in the chart; `helm lint` with the `ci/` values
  renders them with the CRDs assumed present, and the chart's `Chart.yaml` lists the
  Prometheus Operator CRDs as an optional dependency in its notes, not as a hard one.
- A dashboard JSON to maintain beside the metrics; it is generated from a small source file
  so a renamed metric breaks a test, not a panel.

## Alternatives Considered

- **Metrics on the API port behind a token.** One port; Prometheus becomes an API client
  with a Secret, and `/api/` is proxied — a misrouted path exposes it. Lost.
- **OpenTelemetry push over OTLP.** Future-proof, traces from the same SDK; a collector as a
  prerequisite and push configuration where most installations scrape. Deferred to D8.
- **OpenTelemetry instruments with a Prometheus exporter.** Prometheus today, OTLP tomorrow
  without re-instrumentation; a heavier SDK with exporter quirks for some forty instruments.
  Lost; `client_golang` is the standard the sibling project uses.

## Residual risks

- The monitoring resources depend on CRDs the installation may not have; each is off by
  default and the operations page says what each requires.
- Route-pattern labels depend on the router naming patterns consistently; the generated
  server of [ADR 0046](0046-spec-first-the-openapi-document-is-the-contract.md) supplies the
  `operationId`, which is the label's source.

## References

- [ADR 0054](0054-server-sent-events-per-tenant-carry-keys-not-content-polling-is-the-fallback.md), [ADR 0059](0059-backups-belong-to-the-operators-cowork-provides-the-export-and-makes-a-restores-inconsistency-visible.md) D4, [ADR 0027](0027-data-access-is-sqlc-over-pgx-behind-a-tenant-transaction-and-a-mutation-wrapper.md) D5, D8, [ADR 0026](0026-one-append-only-audit-table-written-by-the-request-layer.md) — the promised numbers
- [ADR 0001](0001-two-containers-a-go-backend-and-an-nginx-frontend-installed-by-one-helm-chart.md) D3 — why the port must stay off the proxy
- [`deploy/helm/cowork/`](../../deploy/helm/cowork/), [`backend/cmd/cowork/main.go`](../../backend/cmd/cowork/main.go) — where the listener and the templates land
