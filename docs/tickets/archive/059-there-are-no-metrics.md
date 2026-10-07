---
id: T59
title: there are no metrics, so an installation cannot see its backend and the chart wires no scraping, alerts or dashboard
state: done
severity: medium
security: none
threat:
urgency: later        # rule 4: a decided fix (ADR 0060)
effort: L
filed-from: phase 7, ADR 0060
opened: 2026-10-06
decided: 2026-10-06
done: 2026-10-06
shipped: the metrics listener COWORK_METRICS_ADDR with every instrument of ADR 0060 D4 but the consistency family, the chart's metrics values with the PodMonitor, the ServiceMonitor and its headless Service, the PrometheusRule with four alerts and the Grafana dashboard, nginx stub_status on the loopback address and the exporter sidecar
---

## Current state

Built as [ADR 0060](../../adr/0060-prometheus-metrics-on-a-second-listener-with-servicemonitor-and-prometheusrule.md)
decides, with what the record left open made concrete in place by the implementer, open to the
owner's objection: the listener `COWORK_METRICS_ADDR` (`:8081`, an empty value closes it) with one
lifecycle beside the API's ([`main.go`](../../../backend/cmd/cowork/main.go) `serve`,
[`httpserver.ServeAll`](../../../backend/internal/httpserver/server.go)); the registry and the
instruments of D4 in [`internal/metrics`](../../../backend/internal/metrics/metrics.go), the only
importer of `client_golang`; the chart's `metrics.*` and `frontend.metrics.exporter.*`; the four
alerts of D6 with their runbooks in [docs/operations/metrics.md](../../operations/metrics.md); what
the unauthenticated port tells in [docs/security/metrics.md](../../security/metrics.md), H-63; how to
add an instrument in [docs/developer/metrics.md](../../developer/metrics.md).

What remains of ADR 0060 is the consistency family of D4 and its two alerts of D6, which come with
the consistency check of
[ADR 0059](../../adr/0059-backups-belong-to-the-operators-cowork-provides-the-export-and-makes-a-restores-inconsistency-visible.md)
D4 that sets them; [docs/developer/metrics.md](../../developer/metrics.md#the-consistency-family)
says where they go.

Verified on 2026-10-06:

- `make test-unit lint cyclo gosec vuln`, `make generate-check`, `make helm-lint helm-template` (every
  `ci/` file, `ci/metrics-values.yaml` with every monitoring resource on) and `make test-integration`
  pass. The unit tier holds the naming rule, no forbidden label on any family, the route label as the
  document's pattern, the pool and the schema read at a scrape, the hub's drops and replays, the
  committed acts by actor, the query error kinds, the dashboard's metrics, the import boundary and
  the shared lifecycle; the integration tier builds `cmd/cowork`, runs `serve` with both listeners
  against a database of its own, scrapes after a few API requests, sees a dirty flag set beside it,
  ends both with `SIGTERM` and runs again with the listener off
  (`TestServeAnswersAScrapeOnItsMetricsListener`).
- The frontend image built with the new configuration and run read-only on a free port: the UI,
  a deep link, a hashed asset and the `404` problem for `/api/` with their headers as before;
  `stub_status` answering on `127.0.0.1:8082` inside the container and refused on the container's
  own address and from another container; `nginx/nginx-prometheus-exporter:1.5.3` in the container's
  network namespace, as user 101 with a read-only root filesystem and the chart's arguments,
  answering `nginx_up 1`.
- An upgrade with `--reuse-values` from a release before the metrics renders as with the chart's
  defaults: the previous release's values have no `metrics` and no `frontend.metrics` block, and the
  helpers `cowork.metrics` and `cowork.frontendMetrics` read the defaults then;
  `ci/reuse-values-values.yaml` renders the chart without both blocks, and its manifests equal those
  of the defaults. The documented upgrade is `--reset-then-reuse-values`.

## Required changes

None.

## Not verified

- A Prometheus scraping through the `PodMonitor` or the `ServiceMonitor`, evaluating the
  `PrometheusRule` or firing an alert, and a Grafana loading the dashboard: the chart was rendered,
  never applied to a cluster with the Prometheus Operator.
- The network policy of [docs/operations/metrics.md](../../operations/metrics.md#the-network-policy-for-the-port)
  against a network plugin that enforces it.
- That Renovate's `helm-values` manager updates the exporter's tag.
