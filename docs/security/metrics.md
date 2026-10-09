# What the metrics tell, and whom

What the backend's metrics listener and the frontend's nginx numbers reveal to whoever reaches them,
what they never carry, and what a scrape costs, as built on 2026-10-09. Who reaches the other ports
of the two pods, and what they trust, is [trust-boundaries.md](trust-boundaries.md); how to scrape
and how to close the port is [docs/operations/metrics.md](../operations/metrics.md); the decision is
[ADR 0060](../adr/0060-prometheus-metrics-on-a-second-listener-with-servicemonitor-and-prometheusrule.md).

## Who reaches them

| Port | Reached by | Verified in |
|---|---|---|
| `COWORK_METRICS_ADDR`, `:8081` by default, of the backend pods: `/metrics` and nothing else, **no authentication** | every pod of the cluster that can reach the pod's address — the chart ships no NetworkPolicy — and the node. It is on no Service the Ingress routes: the backend Service lists the API's port alone, and the chart's Ingress sends `/api/` and `/auth/` to that Service and nothing else. With `metrics.serviceMonitor.enabled` a headless Service `<fullname>-backend-metrics` names the pods' addresses on the port; it adds a DNS name, not a path from outside | [`main.go`](../../backend/cmd/cowork/main.go) `serve`, [`httpserver.NewMetrics`](../../backend/internal/httpserver/server.go), [`backend-service.yaml`](../../deploy/helm/cowork/templates/backend-service.yaml), [`servicemonitor.yaml`](../../deploy/helm/cowork/templates/servicemonitor.yaml); `TestServeAnswersAScrapeOnItsMetricsListener` |
| nginx's `stub_status` on `127.0.0.1:8082` of the frontend pods | the pod's own containers: nginx and, when it is on, the exporter sidecar, which share the pod's loopback address | [`default.conf`](../../frontend/nginx/default.conf); the built image on 2026-10-06 — the loopback address answers inside the pod, and a peer outside the pod, at the pod's own address, is refused |
| the exporter sidecar's `nginx-metrics`, `:9113`, with `frontend.metrics.exporter.enabled` | like the backend's metrics port: every pod of the cluster that reaches the frontend pod's address | [`frontend-deployment.yaml`](../../deploy/helm/cowork/templates/frontend-deployment.yaml) |

The API's listener has no `/metrics`, and the metrics listener has no health, API or login path: a
mistake in the Ingress that sends a path to the backend's API port exposes no metrics, and the
metrics port exposes nothing of the API.

## What they tell

Whoever reads a scrape learns how the installation is used, not what it holds:

| Family | Tells |
|---|---|
| `cowork_http_*` | requests per route pattern, method and status, their latency, and how many are in flight — which features are used, how often, when, and how well they answer |
| `cowork_db_*` | the pool's size and use, the waits for a connection, failed statements by kind — whether the database keeps up |
| `cowork_jobs_*` | whether and how long the background jobs run, and their failures in a row |
| `cowork_events_*` | open event streams — about the number of browser tabs open on the replica —, events published — the rate of writes —, drops and replays |
| `cowork_audit_acts_total` | acts by action and kind of actor: how many comments, logins, deletions, purges, grants … were committed, and whether people, agents or the system made them |
| `cowork_auth_*` | logins by method and outcome, lockouts, refused tokens by reason — a guesser in the cluster can watch whether its guesses lock usernames, and an operator can watch the guessing |
| `cowork_migrations_*` | the schema version, which tells the release within a few, and whether a migration failed |
| `cowork_consistency_*` | per tenant, by its id, how many of its files lost their bytes and no administrator accepted the loss of, and how many objects no file names, as its latest consistency check found them — and with the series, how many tenants have a result —; and for every tenant of the installation, by its id, how long ago a project of it or the whole tenant was last exported, or how long ago it was made where it never was — so the scrape lists every tenant's id, and tells which tenants are exported on a schedule and when |
| `go_*`, `process_*` | the Go version, memory, goroutines, file descriptors, the start time |
| the exporter's `nginx_*` | the frontend's connections and its request total |

## What they never carry

- **No label names a person, a ticket, a key, a token or a request id**, and a tenant only on the
  consistency family's three gauges — the check's two counts and the age of the last export —, by
  its id — never its slug, which would name a client: a unit test walks every family the backend
  records and fails on such a label, on `tenant` outside those three, on a tenant that is no id, and
  on another label beside it (`TestNoInstrumentCarriesAForbiddenLabel` in
  [`metrics_test.go`](../../backend/internal/metrics/metrics_test.go)). Every other count is the
  whole replica's, never a tenant's; those three are read from the database and are the same on
  every replica.
- **No client writes a label value.** A route is the API document's pattern —
  `/api/v1/tenants/{tenant}/projects/{project}/tickets/{number}`, never the path sent —, a path no
  route matches is `unmatched`, and a method HTTP does not define is `other`
  (`TestTheRouteLabelIsTheDocumentsPattern`, and the integration tier on the built binary). Every
  other label comes from a closed set in the code, or — an act's action, a job's name — from the
  code and the database's enum.
- **No secret, and of the configuration one value.** The schema read's error, which can name the
  database's host and user, goes to the log and never into the scrape. What the configuration sets
  shows in one series alone: the pool's maximum, `cowork_db_pool_max_connections` — the
  `pool_max_conns` of `COWORK_DATABASE_URL`, or the driver's default from the CPUs.

## What a scrape costs

The instruments are in memory, and so are the pool's statistics. The schema's version and dirty flag
are one query of the version table, at most once every ten seconds whoever scrapes, within two
seconds ([`internal/metrics`](../../backend/internal/metrics/metrics.go) `schemaCollector`); the
consistency family one read-only transaction of two queries — the counts from their table, every
tenant's last export from the audit record over a partial index of the export acts —, at most once a
minute, within two seconds (`consistencyCollector`). At most
four scrapes are served at once, a fifth answered `503`. The series are bounded: the routes by the
document's operations, the statuses by what the handlers answer, every other label by its set; a
scrape's size does not grow with the data, but for the consistency family's three series per
tenant.

## What this does not cover

<a id="h-63"></a>
### H-63 — The metrics port answers every pod of the cluster

Live by default, and accepted with ADR 0060 D1, D2: the listener has no authentication,
`metrics.enabled` is `true`, and the chart ships no NetworkPolicy
([ADR 0001](../adr/0001-two-containers-a-go-backend-and-an-nginx-frontend-installed-by-one-helm-chart.md)
D3). A workload anywhere in the cluster — another team's pod, a compromised one — reads the
installation's activity: how much of which feature is used and when, the rate of logins that fail
and of usernames locked while it guesses passwords through the backend Service, the release by its
schema version, the process's resources — and, per tenant id, how many of its files a restore or a
failed removal left out of step, and how long ago it was last exported, with every tenant's id and so
their number; a tenant's id is a UUIDv7, so it tells when the tenant was made as well. It reads no name of a tenant, a file or a ticket and gets nothing to
act with: no credential, no key, no person. The listener writes no request log, so a reader of the
port leaves no line behind. The exporter's port, when on, adds
the frontend's connection counts. What an installation can do: admit only the monitoring namespace
to the port, beside the policy for the Ingress controller
([docs/operations/metrics.md](../operations/metrics.md#the-network-policy-for-the-port)), or switch
the listener off with `metrics.enabled: false`, which renders `COWORK_METRICS_ADDR` empty and opens
no port. Not verified: no policy was run against a network plugin that enforces it.

<a id="h-80"></a>
### H-80 — What was scraped reaches whoever reads the monitoring

Live wherever the installation scrapes the port. A scrape is plain HTTP — the listener serves no TLS,
and the chart's `ServiceMonitor` and `PodMonitor` name no scheme of their own —, and what it carries
lives on in the monitoring for as long as its retention: every user who may query Prometheus, every
target it writes to remotely, the chart's Grafana dashboard, whose consistency panels show the counts
and the time since the last export by tenant id, and the receivers of Alertmanager, since `CoworkAttachmentsOutOfStep` and
`CoworkExportOverdue` carry the tenant id in their labels ([`prometheusrule.yaml`](../../deploy/helm/cowork/templates/prometheusrule.yaml)).
That is the activity of [H-63](#h-63), kept and passed on beyond the cluster's network. Mitigation:
treat the monitoring as reading the installation's activity, keep its users and its remote targets to
those who may, and put the scrape on a network path a policy of the cluster's keeps to the monitoring
namespace.
