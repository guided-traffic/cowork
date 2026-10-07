# Metrics

How the backend records its Prometheus instruments: one registry, made in `main.go` and passed to
every package that records, never the client library's global one; the typed methods each package
records through; the listener and the lifecycle it shares with the API's; the Grafana dashboard
generated from Go; the tests; and where the consistency family goes. The decision is
[ADR 0060](../adr/0060-prometheus-metrics-on-a-second-listener-with-servicemonitor-and-prometheusrule.md);
the names and labels are [README.md, Metrics](../../README.md#metrics); scraping and the alerts are
[docs/operations/metrics.md](../operations/metrics.md); what the port tells is
[docs/security/metrics.md](../security/metrics.md). Read against the tree on 2026-10-06.

```
runServe ─► metricsOf(cfg) ─► *metrics.Metrics, or nil while COWORK_METRICS_ADDR is empty
   ├─► store.Open(Options{Metrics})      the pool and the schema state, read at a scrape; failed statements;
   │                                     every RunJob; the acts, after their commit; the lockouts
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
`Unmatched`, a method HTTP does not define `other` (D5). The closed sets are made at zero
(`initialise`), so a rate over them is defined from the first scrape.

## Who records what

| Where | Records |
|---|---|
| [`httpserver/server.go`](../../backend/internal/httpserver/server.go) `instrument`, between `withRequestID` and `requestLog` | `Metrics.Request`: in flight while the handler runs, then the route, the method, the status and the duration; `handleGet` names `/healthz` and `/readyz` |
| [`api/api.go`](../../backend/internal/api/api.go) `ServeHTTP` | `metrics.SetRoute(ctx, route.Path)` once the router found the operation: the pattern the document writes. The chat's tool calls come through the root handler as requests of their own |
| [`api/authn.go`](../../backend/internal/api/authn.go), [`identity.go`](../../backend/internal/api/identity.go) | `TokenRefused`: `malformed` and `unknown` in `authenticateToken`, `revoked` and `expired` through `recordRefusal`, `not_allowed` through it from `tokenGate`, `session_only` in `authenticate` |
| [`api/login.go`](../../backend/internal/api/login.go), [`oidc.go`](../../backend/internal/api/oidc.go) | `Login`: the local form's outcome after `RecordLoginAttempt` — `throttled` in `throttled` —, the identity provider's in `OidcCallback`'s `fail` and at its success |
| [`events/hub.go`](../../backend/internal/events/hub.go) | `OpenStreams` in `Subscribe`, `remove` and `endAll`; `EventPublished` in `Publish`; `SubscriberDropped` — `behind` in `send`, `limit` in `limit`, `resync` in `SetUp` — only for a stream `end` actually ended; `Replay` in `Subscribe` with a `Last-Event-ID` |
| [`store/store.go`](../../backend/internal/store/store.go) | `ObservePool(poolStats)` and `ObserveSchema(schemaForMetrics)` in `Open`; `QueryError(queryErrorKind(err))` in the slow-query tracer, for every failed statement but `pgx.ErrNoRows` |
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
| [`metrics_test.go`](../../backend/internal/metrics/metrics_test.go) | the naming rule; no forbidden label on any family, every family exercised (`exercise`); a request recorded by its pattern, `unmatched` and `other`; a nil registry; a job's consecutive failures; the pool read at a scrape; the schema read at most every ten seconds and left out after a failure; the closed sets at zero; the text format; the dashboard; the import boundary |
| [`httpserver/server_test.go`](../../backend/internal/httpserver/server_test.go) | the route through the outer handler, the metrics listener's paths, `ServeAll` |
| [`api/metrics_test.go`](../../backend/internal/api/metrics_test.go) | the route label is the document's pattern, never the tenant, project or number sent; a malformed token; a failed callback |
| [`events/hub_test.go`](../../backend/internal/events/hub_test.go) `TestTheHubRecordsItsInstruments` | open streams, published, the three drops — a shutdown none —, hits and misses |
| [`store/metrics_test.go`](../../backend/internal/store/metrics_test.go) | the kinds of a failed statement, `ErrNoRows` none; an act's actor |
| [`config_test.go`](../../backend/internal/config/config_test.go) `TestLoadMetricsAddr` | the default, the empty value that switches the listener off, an address without a port, the API's address |
| [`metrics_test.go`](../../backend/test/integration/metrics_test.go) `TestServeAnswersAScrapeOnItsMetricsListener` | the built binary against a database of its own: both listeners, a scrape after a few API requests — routes, acts by actor, a refused token, the pool, the schema, the jobs, no forbidden label —, the dirty flag seen while it serves, `SIGTERM`, and the empty variable |

How to add an instrument is [adding-things.md](adding-things.md#an-instrument).

## The consistency family

Not built. Its instruments — dangling attachment metadata, orphaned objects, seconds since the last
export — belong to the consistency check of
[ADR 0059](../adr/0059-backups-belong-to-the-operators-cowork-provides-the-export-and-makes-a-restores-inconsistency-visible.md)
D4 that sets them, and come with it: into `internal/metrics` as `cowork_consistency_*`, the one family
whose `tenant` label `TestNoInstrumentCarriesAForbiddenLabel` admits, recorded by the check through
typed methods like every other; its two alerts — counts above zero for longer than the restore
window, the last export too old — into
[`prometheusrule.yaml`](../../deploy/helm/cowork/templates/prometheusrule.yaml) with their sections in
[docs/operations/metrics.md](../operations/metrics.md); its panels into `dashboard.go`.
