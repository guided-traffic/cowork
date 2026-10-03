# ADR 0060: Prometheus Metrics on a Second Backend Listener Without Authentication, Not in the Service, With `ServiceMonitor` and `PrometheusRule` in the Chart — OpenTelemetry When Tracing Comes

## Status

Accepted, amended 2026-10-03 (D2: the chart's backend NetworkPolicy). Date: 2026-10-01. Decided
by the owner as the answer to the catalog question "metrics?": Prometheus on a separate port with `client_golang`, together with the
kube-prometheus custom resources (`ServiceMonitor`/`PodMonitor`, `PrometheusRule`) rendered
by the chart, over metrics on the main port behind authentication, over OpenTelemetry push,
and over OpenTelemetry instruments with a Prometheus exporter. The rules of D5–D8 were put to
the owner with the question and not objected to.

**Not built.** No metrics listener, no instruments, no monitoring resources in the chart.

## Context

Several records promised numbers: the event stream's open streams, drops and replays
([ADR 0054](0054-server-sent-events-per-tenant-carry-keys-not-content-polling-is-the-fallback.md)),
the consistency check's dangling metadata and orphans and the last export time
([ADR 0059](0059-backups-belong-to-the-operators-cowork-provides-the-export-and-makes-a-restores-inconsistency-visible.md)
D4), the pool and a tracer slot ([ADR 0027](0027-data-access-is-sqlc-over-pgx-behind-a-tenant-transaction-and-a-mutation-wrapper.md)
D8), the per-token view of the audit ([ADR 0026](0026-one-append-only-audit-table-written-by-the-request-layer.md)).
The API port is proxied by nginx under `/api/`, so a metrics path on it would be one proxy
mistake away from the internet; a second listener that the Service does not expose is the
pattern the sibling operator uses and every Prometheus scrapes. The owner runs
kube-prometheus and wants the resources that wire scraping and alerting rendered by the chart.

## Decision

**D1 — Metrics are Prometheus text on a second backend listener,** `COWORK_METRICS_ADDR`
(default `:8081`), path `/metrics`, no authentication, implemented with `client_golang`. The
listener shares the lifecycle of the API listener and is never proxied by nginx. The health
probes stay on `:8080`.

**D2 — The chart exposes the port on the pod, not on the Service.** `metrics.enabled`
(default `true`) adds the container port `metrics`; the backend Service does not list it. The
operations page names the NetworkPolicy an installation should add: port 8081 reachable from
the monitoring namespace only. *(Amended 2026-10-03: the chart now renders a NetworkPolicy of its
own for the backend pods, which admits the frontend's pods on the API port and nothing else
([ADR 0001](0001-two-containers-a-go-backend-and-an-nginx-frontend-installed-by-one-helm-chart.md)
D3). Policies add up, so the rule for port 8081 is a policy beside it — and with the chart's in
place it is needed, not only advisable: a scrape of the metrics port is refused where policies
are enforced until the monitoring namespace is admitted.)*

**D3 — The chart renders the kube-prometheus resources, each behind a switch, default off:**

| Resource | Value | Content |
|---|---|---|
| `PodMonitor` | `metrics.podMonitor.enabled` | scrapes the `metrics` port of the backend pods; labels and interval configurable; `PodMonitor` rather than `ServiceMonitor` because the port is not on the Service |
| `PrometheusRule` | `metrics.prometheusRule.enabled` | the alerts of D6, with configurable labels for routing |
| Grafana dashboard `ConfigMap` | `metrics.grafanaDashboard.enabled` | one dashboard JSON with the sidecar label, covering the metrics of D4 |

A `ServiceMonitor` variant exists for installations whose Prometheus discovers only Services
(`metrics.serviceMonitor.enabled`), which then adds a separate headless Service for the
metrics port alone, so the API Service still never carries it.

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

**D5 — Cardinality discipline.** No label carries a person, a ticket key, a token or a
request id; route labels are the pattern (`/tenants/{slug}/projects/{KEY}/tickets/{number}`),
never the instance; the `tenant` label appears on the `consistency` family only.

**D6 — The alerts of the first release:** schema dirty; consistency counts above zero for
longer than the restore window; last export older than a configurable number of days;
sustained event-stream drops; pool exhaustion sustained; a background job failing twice in
a row. Each alert's annotation links to the operations page's section for it.

**D7 — nginx metrics are opt-in.** `stub_status` on `127.0.0.1` inside the frontend
container; `frontend.metrics.exporter.enabled` adds the nginx exporter sidecar and the
corresponding `PodMonitor` entry; default off.

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
