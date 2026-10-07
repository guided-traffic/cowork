# What the metrics tell, and whom

What the backend's metrics listener and the frontend's nginx numbers reveal to whoever reaches them,
what they never carry, and what a scrape costs, as built on 2026-10-06. Who reaches the other ports
of the two pods, and what they trust, is [trust-boundaries.md](trust-boundaries.md); how to scrape
and how to close the port is [docs/operations/metrics.md](../operations/metrics.md); the decision is
[ADR 0060](../adr/0060-prometheus-metrics-on-a-second-listener-with-servicemonitor-and-prometheusrule.md).

## Who reaches them

| Port | Reached by | Verified in |
|---|---|---|
| `COWORK_METRICS_ADDR`, `:8081` by default, of the backend pods: `/metrics` and nothing else, **no authentication** | every pod of the cluster that can reach the pod's address — the chart ships no NetworkPolicy — and the node. It is on no Service the Ingress routes: the backend Service lists the API's port alone, and the chart's Ingress sends `/api/` and `/auth/` to that Service and nothing else. With `metrics.serviceMonitor.enabled` a headless Service `<fullname>-backend-metrics` names the pods' addresses on the port; it adds a DNS name, not a path from outside | [`main.go`](../../backend/cmd/cowork/main.go) `serve`, [`httpserver.NewMetrics`](../../backend/internal/httpserver/server.go), [`backend-service.yaml`](../../deploy/helm/cowork/templates/backend-service.yaml), [`servicemonitor.yaml`](../../deploy/helm/cowork/templates/servicemonitor.yaml); `TestServeAnswersAScrapeOnItsMetricsListener` |
| nginx's `stub_status` on `127.0.0.1:8082` of the frontend pods | the pod's own containers: nginx and, when it is on, the exporter sidecar | [`default.conf`](../../frontend/nginx/default.conf); the built image on 2026-10-06 — the loopback address answers inside the container, the container's own address and another container are refused |
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
| `go_*`, `process_*` | the Go version, memory, goroutines, file descriptors, the start time |
| the exporter's `nginx_*` | the frontend's connections and its request total |

## What they never carry

- **No label names a person, a ticket, a key, a token or a request id**, and none a tenant: a unit
  test walks every family the backend records and fails on such a label, and on `tenant` outside the
  consistency family, which is not built yet (`TestNoInstrumentCarriesAForbiddenLabel` in
  [`metrics_test.go`](../../backend/internal/metrics/metrics_test.go)). A count is the whole
  replica's, never a tenant's.
- **No client writes a label value.** A route is the API document's pattern —
  `/api/v1/tenants/{tenant}/projects/{project}/tickets/{number}`, never the path sent —, a path no
  route matches is `unmatched`, and a method HTTP does not define is `other`
  (`TestTheRouteLabelIsTheDocumentsPattern`, and the integration tier on the built binary). Every
  other label comes from a closed set in the code, or — an act's action, a job's name — from the
  code and the database's enum.
- **No secret and no configuration value.** The schema read's error, which can name the database's
  host and user, goes to the log and never into the scrape; nothing of `COWORK_*` is an instrument.

## What a scrape costs

The instruments are in memory, and so are the pool's statistics. The schema's version and dirty flag
are one query of the version table, at most once every ten seconds whoever scrapes, within two
seconds ([`internal/metrics`](../../backend/internal/metrics/metrics.go) `schemaCollector`). At most
four scrapes are served at once, a fifth answered `503`. The series are bounded: the routes by the
document's operations, the statuses by what the handlers answer, every other label by its set; a
scrape's size does not grow with the data.

## What this does not cover

<a id="h-63"></a>
### H-63 — The metrics port answers every pod of the cluster

Live by default, and accepted with ADR 0060 D1, D2: the listener has no authentication,
`metrics.enabled` is `true`, and the chart ships no NetworkPolicy
([ADR 0001](../adr/0001-two-containers-a-go-backend-and-an-nginx-frontend-installed-by-one-helm-chart.md)
D3). A workload anywhere in the cluster — another team's pod, a compromised one — reads the
installation's activity: how much of which feature is used and when, the rate of logins that fail
and of usernames locked while it guesses passwords through the backend Service, the release by its
schema version, the process's resources. It reads nothing of a tenant and gets nothing to act with:
no credential, no key, no person, no name of a tenant or a ticket. The exporter's port, when on, adds
the frontend's connection counts. What an installation can do: admit only the monitoring namespace
to the port, beside the policy for the Ingress controller
([docs/operations/metrics.md](../operations/metrics.md#the-network-policy-for-the-port)), or switch
the listener off with `metrics.enabled: false`, which renders `COWORK_METRICS_ADDR` empty and opens
no port. Not verified: no policy was run against a network plugin that enforces it.
