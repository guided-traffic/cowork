# Metrics

How the backend records its Prometheus instruments: one registry, made in `main.go` and passed to
every package that records, never the client library's global one; the typed methods each package
records through; the listener and the lifecycle it shares with the API's; the Grafana dashboard
generated from Go; the tests; and the consistency family, read from the database at a scrape. The decision is
[ADR 0060](../adr/0060-prometheus-metrics-on-a-second-listener-with-servicemonitor-and-prometheusrule.md);
the names and labels are [README.md, Metrics](../../README.md#metrics); scraping and the alerts are
[docs/operations/metrics.md](../operations/metrics.md); what the port tells is
[docs/security/metrics.md](../security/metrics.md). Read against the tree on 2026-10-06.

```
runServe ─► metricsOf(cfg) ─► *metrics.Metrics, or nil while COWORK_METRICS_ADDR is empty
   ├─► store.Open(Options{Metrics})      the pool, the schema state, the consistency counts and last exports, read at a scrape;
   │                                     failed statements; every RunJob; the acts, after their commit; the lockouts
   ├─► events.New(window, limit, m)      open streams, notifications received, streams dropped, replays
   ├─► api.New(Options{Metrics})         the route's pattern for every request; logins; refused tokens
   ├─► httpserver.New(Options{Metrics})  every request: in flight, then its route, method, status, duration
   └─► serve ─► bind (both ports, before either serves) ─► httpserver.ServeAll(
          the API listener (hub.Close at its shutdown),
          the metrics listener: httpserver.NewMetrics(m.Handler(logger)) — GET /metrics, nothing else)
```

## The registry

[`internal/metrics`](../../backend/internal/metrics/metrics.go) is the only package that imports
`client_golang` (`TestOnlyThisPackageImportsTheClientLibrary` lists every package of the module,
the integration tier included, and fails on another importer of `github.com/prometheus/`). `New`
makes a `prometheus.Registry` of its own with the Go runtime and process collectors and every
instrument; nothing registers with the library's default registry, so a test makes as many as it
likes and reads one back with `Samples`, `Sum` and `Has`
([`samples.go`](../../backend/internal/metrics/samples.go)), without importing the library either.

Every method may be called on a nil `*Metrics` and does nothing then: a package records
unconditionally, and a test or a caller that does not care passes nil. `cowork serve` passes nil
when `COWORK_METRICS_ADDR` is empty, so nothing is recorded that nobody can scrape.

A label value comes from one of three places and from nowhere else: a closed set of typed constants
(`Actor`, `LoginMethod`, `LoginOutcome`, `TokenRefusal`, `StreamDrop`, the store's query error
kinds), the API document's route patterns, or a name in the code — a job's, an audit action, which
the database's enum bounds. No value a request carries reaches a label: a route nobody names is
`Unmatched`, a method HTTP does not define `other` (D5). The one exception is the consistency
family's `team` — and `tenant` beside it, the same value, for one release —, a team's id read from the database ([below](#the-consistency-family)). The closed sets are made at zero
(`initialise`), so a rate over them is defined from the first scrape.

## Who records what

| Where | Records |
|---|---|
| [`httpserver/server.go`](../../backend/internal/httpserver/server.go) `instrument`, between `withRequestID` and `requestLog` | `Metrics.Request`: in flight while the handler runs, then the route, the method, the status and the duration; `handleGet` names `/healthz` and `/readyz` |
| [`api/api.go`](../../backend/internal/api/api.go) `ServeHTTP` | `metrics.SetRoute(ctx, route.Path)` once the router found the operation: the pattern the document writes — for a request to a deprecated twin under `/api/v1/tenants`, its team path's, since the pipeline read its path as the team path first ([api.md](api.md#the-pipeline)). The chat's tool calls come through the root handler as requests of their own |
| [`api/authn.go`](../../backend/internal/api/authn.go), [`identity.go`](../../backend/internal/api/identity.go) | `TokenRefused`: `malformed` and `unknown` in `authenticateToken`, `revoked` and `expired` through `recordRefusal`, `not_allowed` through it from `tokenGate`, `session_only` in `authenticate` |
| [`api/login.go`](../../backend/internal/api/login.go), [`oidc.go`](../../backend/internal/api/oidc.go) | `Login`: the local form's outcome after `RecordLoginAttempt` — `throttled` in `throttled` —, the identity provider's in `OidcCallback`'s `fail` and at its success |
| [`events/hub.go`](../../backend/internal/events/hub.go) | `OpenStreams` in `Subscribe`, `remove` and `endAll`; `EventPublished` in `Publish`; `SubscriberDropped` — `behind` in `send`, `limit` in `limit`, `resync` in `SetUp` — only for a stream `end` actually ended; `Replay` in `Subscribe` with a `Last-Event-ID` |
| [`store/store.go`](../../backend/internal/store/store.go) | `ObservePool(poolStats)`, `ObserveSchema(schemaForMetrics)` and `ObserveConsistency(consistencyForMetrics)` in `Open`; `QueryError(queryErrorKind(err))` in the slow-query tracer, for every failed statement but `pgx.ErrNoRows` |
| [`store/jobs.go`](../../backend/internal/store/jobs.go) `RunJob` | `JobRun(name, took, failed)` for a run that took the lock or failed before it, unless the context ended; the bootstrap is a `RunJob` too |
| [`store/tx.go`](../../backend/internal/store/tx.go) | `writeEvents` keeps each act's action and actor (`actorOf`), and `countActs` records them after the commit of `Mutate`, `RunJob`, `RecordLoginAttempt` and the identity provider's transactions — a rolled-back act is never counted |
| [`store/login.go`](../../backend/internal/store/login.go) `RecordLoginAttempt` | `Lockout` for a committed `locked` act |

The pool's statistics and the schema state are read at a scrape: `poolCollector` calls the pool's
`Stat`, and `schemaCollector` the store's `SchemaState` at most once every ten seconds, within two,
keeping the last answer for the scrapes in between and leaving both families out after a failed read.
pgx counts the acquires that waited and their time, but not how many wait at a moment; the
dashboard's "waiting" is the rate of the wait time, the mean number waiting.

## The listener

`httpserver.NewMetrics` is the metrics listener's handler: `GET` and `HEAD /metrics` to
`Metrics.Handler` — `promhttp` over the registry, at most four scrapes at once, a failing collector
left out —, every other path a `404` problem and another method `405`, with a request id and no
request log; nothing on it is instrumented. `httpserver.ServeAll` serves several listeners until the
context ends or one of them stops, then shuts all of them down, each within the timeout; `serve` and
`bind` in [`main.go`](../../backend/cmd/cowork/main.go) bind both before either serves and register
`hub.Close` on the API listener's shutdown. `TestServeAllSharesOneLifecycle` holds the lifecycle.

## The dashboard

[`dashboard.go`](../../backend/internal/metrics/dashboard.go) is the Grafana dashboard as data: rows of
panels, each a title, a unit and its PromQL, where `$sel` stands for the namespace the dashboard's
variable picks. `Dashboard` renders the JSON; `make generate` writes it with
[`tools/dashboard`](../../backend/tools/dashboard/main.go) into
[`deploy/helm/cowork/files/grafana-dashboard.json`](../../deploy/helm/cowork/files/grafana-dashboard.json),
which [`grafana-dashboard.yaml`](../../deploy/helm/cowork/templates/grafana-dashboard.yaml) puts into
its ConfigMap, and `make generate-check` fails on a file that differs. Never edit the JSON:
`TestTheDashboardNamesOnlyInstrumentsThatExist` holds every metric a query names to one the
registry answers, so a renamed instrument fails the test and not a panel.

## Tests

| Test | Holds |
|---|---|
| [`metrics_test.go`](../../backend/internal/metrics/metrics_test.go) | the naming rule; no forbidden label on any family, every family exercised (`exercise`), the team on the consistency family's three gauges alone, an id, as `team` and as `tenant` with the same value and nothing else beside them (`teamLabelled`); a request recorded by its pattern, `unmatched` and `other`; a nil registry; a job's consecutive failures; the pool read at a scrape; the schema read at most every ten seconds and the consistency family at most once a minute, each left out after a failure, and the age of the last export counted at every scrape; the closed sets at zero; the text format; the dashboard; the import boundary |
| [`httpserver/server_test.go`](../../backend/internal/httpserver/server_test.go) | the route through the outer handler, the metrics listener's paths, `ServeAll` |
| [`api/metrics_test.go`](../../backend/internal/api/metrics_test.go) | the route label is the document's pattern, never the team, project or number sent, and a twin's its team path's, never `{tenant}`; a malformed token; a failed callback |
| [`events/hub_test.go`](../../backend/internal/events/hub_test.go) `TestTheHubRecordsItsInstruments` | open streams, published, the three drops — a shutdown none —, hits and misses |
| [`store/metrics_test.go`](../../backend/internal/store/metrics_test.go) | the kinds of a failed statement, `ErrNoRows` none; an act's actor |
| [`config_test.go`](../../backend/internal/config/config_test.go) `TestLoadMetricsAddr` | the default, the empty value that switches the listener off, an address without a port, the API's address |
| [`api_consistency_test.go`](../../backend/test/integration/api_consistency_test.go) `TestTheConsistencyCheckFindsWhatARestoreLeftAndTheAdministratorSettlesIt`, `TestTheLastExportIsReadFromTheAuditRecord` | a second store with a registry of its own answers the counts a check stored, by team id, a clean team's zero included; and every team's age of its last export — from its creation before any, untouched by a ticket's Markdown, reset by a project's export —, the job's read across the teams admitted to the export acts alone |
| [`metrics_test.go`](../../backend/test/integration/metrics_test.go) `TestServeAnswersAScrapeOnItsMetricsListener` | the built binary against a database of its own: both listeners, a scrape after a few API requests — routes, acts by actor, a refused token, the pool, the schema, the jobs, no forbidden label —, the dirty flag seen while it serves, `SIGTERM`, and the empty variable |

How to add an instrument is [adding-things.md](adding-things.md#an-instrument).

## The consistency family

`cowork_consistency_dangling_attachments` and `cowork_consistency_orphaned_objects` are the counts of
each team's latest consistency check of
[ADR 0059](../adr/0059-backups-belong-to-the-operators-cowork-provides-the-export-and-makes-a-restores-inconsistency-visible.md)
D4 ([storage.md](storage.md#the-consistency-check)); `cowork_consistency_last_export_age_seconds` is
the seconds since each team's last export, the second line of its backup (D2). They are the only
family with a team's label, `team` — the team's id, never its slug, which names a team on a port
without authentication (ADR 0060 D5) — and `tenant` beside it, the same id under the label's name
before, for one release ([api.md](api.md#deprecated-names)). An operator's own dashboards and
recording rules move to `team`; an operator's own alert groups by `team` only where no alert routing
label of theirs has that name, and otherwise copies the id to another name with `label_replace`, as
the contract release plans with `cowork_team`. The chart's own two alerts and the generated
dashboard keep `tenant` in this release (below).

**Read, not recorded.** The check runs on one replica, once a day, and an export on whichever replica
serves it; a gauge set in that process would stay behind there and be missing on the others. So
`consistencyCollector` reads the database at a scrape through `ObserveConsistency(read)` — the
store's `consistencyForMetrics`, one `jobRead` that names the job `consistency-check` and no team
and reads `metrics.Consistency`: the stored results (`ListConsistencyCounts`, admitted by the policy
`consistency_checks_counts`) and every team's last export (`ListLastExports`, admitted by
`audit_exports_read` of migration 46, which lets the job read the acts of a project's or a team's
export and no other row of the audit record, over the partial index `audit_exports_by_tenant`) —, at
most once a minute and within two seconds, keeping the last answer between, and leaves the family
out after a failed read, as `schemaCollector` does. Every replica answers the same, one that starts
answers at once, and an acceptance, a removal or an export shows within a minute. A team without a
result has no counts; every team has the age.

**The age.** `ListLastExports` answers, per team, the latest `exported` act on the entity
`project` or `tenant` — a ticket's Markdown and context record `exported` on the ticket and do not
count —, or the team's `created_at` where there is none, so that a team nobody ever exported
ages from its creation and the alert sees a schedule that was never set up. The collector keeps that
time (`TenantExport.Since`) and counts the age at every scrape from its own clock, never below zero,
so the age grows between two reads.

`TestNoInstrumentCarriesAForbiddenLabel` holds the labels `team` and `tenant` to these three gauges
alone — their names in `teamLabelled` —, each value to an id, `tenant` to `team`'s value, and the
gauges to those two labels;
`TestTheLastExportsAgeIsCountedAtEveryScrape` the age between reads, after an export, against a
clock ahead and after a failed read. The alert `CoworkAttachmentsOutOfStep` in
[`prometheusrule.yaml`](../../deploy/helm/cowork/templates/prometheusrule.yaml) sums the two counts
per team, `max` over the replicas, for `metrics.prometheusRule.restoreWindow`; `CoworkExportOverdue`
takes the age per team, `max` over the replicas, above `metrics.prometheusRule.exportMaxAgeDays`
days, which the template holds to a whole number of at least 1 and defaults to 7 where the value is
missing. Both group by `tenant` and name the team's id by it, as the release before did, while
their texts say team, and the dashboard's row *Consistency and export* shows all three by `tenant`
(`teamLabelOfDashboard` in [`dashboard.go`](../../backend/internal/metrics/dashboard.go)): every image
of an image rollback's window emits `tenant`
([ADR 0028](../adr/0028-migrations-only-go-forward-no-down-files-expand-before-contract.md) D4),
and an alert's own label `team` would collide with a routing label of that name —
`metrics.prometheusRule.alertLabels`, which wins, gives `team` as its example —, so that two teams'
alerts would share one label set, which Prometheus refuses to evaluate. The contract release moves
the alerts to the team's label under a name no routing label collides with, `cowork_team` through
`label_replace`, and the dashboard to `team`.
