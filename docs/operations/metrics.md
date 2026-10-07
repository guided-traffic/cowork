# Metrics

Where Prometheus reads cowork's numbers, how the chart wires the scraping, the alerts and the
dashboard, the network policy an installation adds for the port, and — one section each — what an
alert means and what to do about it. The name and the labels of every instrument are
[README.md, Metrics](../../README.md#metrics); the decision is
[ADR 0060](../adr/0060-prometheus-metrics-on-a-second-listener-with-servicemonitor-and-prometheusrule.md);
what the port tells whoever reaches it is [docs/security/metrics.md](../security/metrics.md).

## The listener

The backend serves Prometheus text at `/metrics` on a listener of its own, `COWORK_METRICS_ADDR`
(`:8081` `# default`), without authentication. The health probes, the API and the login flows stay
on `COWORK_LISTEN_ADDR` (`:8080`), which has no `/metrics`; the metrics listener has nothing but
`/metrics` and answers every other path `404`. The variable set to an empty value switches the
listener off, and nothing is recorded then.

- **At start** the backend binds both ports before it serves either, and the log says `listening`
  and `metrics listening` with the address — or `the metrics listener is off`. A port that is taken
  ends the process with `server stopped with error`, naming `COWORK_METRICS_ADDR` and the address.
- **At `SIGTERM`** both listeners stop together, within `COWORK_SHUTDOWN_TIMEOUT`; one that fails
  while serving stops the other, and the process exits 1.
- **A scrape** reads the instruments from memory and the database pool's statistics from the pool;
  the schema's version and dirty flag come from one query of the version table at most every ten
  seconds, within two — a read that fails leaves both out of the scrape until the next read and logs
  `the schema version could not be read for the metrics` at warn. At most four scrapes are served
  at once; a fifth is answered `503`.
- **Nothing of it is on a Service**: the chart puts the container port `metrics` on the backend pods
  and lists it in no Service the Ingress could reach. To look at it by hand:

```bash
kubectl -n cowork port-forward deploy/cowork-backend 8081:8081   # example namespace and release
curl -s http://localhost:8081/metrics | grep '^cowork_migrations'
```

In the chart, `metrics.enabled` (`true` `# default`) renders `COWORK_METRICS_ADDR=":<metrics.port>"`
and the container port; `false` renders the variable empty. `metrics.port` (`8081` `# default`) must
differ from `backend.containerPort`, or rendering fails
([Helm chart values](../../README.md#helm-chart-values)).

## Scraping it with the Prometheus Operator

The chart renders the kube-prometheus resources, each behind a switch that is off by default. They
need the Prometheus Operator's CRDs (`monitoring.coreos.com/v1`) in the cluster; the install notes
name the ones switched on. A cluster without the CRDs knows no such kind, and the install fails on
it — leave the switches off there. Not run here: the chart in a cluster with the operator; `helm lint`
and `helm template` render every resource with `ci/metrics-values.yaml`, nothing more.

| Value | Renders | Scrapes |
|---|---|---|
| `metrics.podMonitor.enabled` | a `PodMonitor` `<fullname>` that selects the release's pods (`app.kubernetes.io/name`, `app.kubernetes.io/instance`) | the port `metrics` of the backend pods, and with `frontend.metrics.exporter.enabled` the port `nginx-metrics` of the frontend pods — a pod without the port is not a target |
| `metrics.serviceMonitor.enabled` | a headless Service `<fullname>-backend-metrics` with the metrics port alone (component label `metrics`) and a `ServiceMonitor` `<fullname>` that selects it | the backend pods alone; the nginx exporter is the `PodMonitor`'s |

Take one of the two: a Prometheus that reads both scrapes the backend twice, and the notes say so.
Each has `labels` — what your Prometheus selects monitors by; kube-prometheus-stack selects by its
own release label unless its `podMonitorSelectorNilUsesHelmValues` and
`serviceMonitorSelectorNilUsesHelmValues` are `false` — and `interval` and `scrapeTimeout`
(`30s` and `10s` `# default`). The scraped series carry the target's `namespace`, `pod`,
`container` and `job`; that is why a job of cowork's is labelled `name`, never `job`.

## The alerts

`metrics.prometheusRule.enabled` renders a `PrometheusRule` `<fullname>` with the alerts below.
`metrics.prometheusRule.labels` are the resource's own — what your Prometheus's `ruleSelector`
matches; kube-prometheus-stack's is its release label unless its `ruleSelectorNilUsesHelmValues` is
`false` —, `metrics.prometheusRule.alertLabels` go on every alert for Alertmanager's routing, and a
`severity` there replaces each alert's own. Every alert reads the backend pods of the release
(`namespace` and a `pod` name that begins with `<fullname>-backend-`), and its `runbook_url` is this
page on GitHub at the tag of the chart's `appVersion`. The thresholds are the template's; to change
one, switch the rule off and write your own from
[`prometheusrule.yaml`](../../deploy/helm/cowork/templates/prometheusrule.yaml).

| Alert | Severity | Fires when |
|---|---|---|
| [`CoworkSchemaDirty`](#coworkschemadirty) | critical | the schema's version has been dirty for ten minutes |
| [`CoworkEventStreamDrops`](#coworkeventstreamdrops) | warning | a pod dropped an event stream that fell behind in every ten minutes for fifteen |
| [`CoworkDatabasePoolExhausted`](#coworkdatabasepoolexhausted) | warning | every connection of a pod's pool has been in use, with acquires waiting, for ten minutes |
| [`CoworkJobFailing`](#coworkjobfailing) | warning | a background job failed at its last two runs on a pod |
| [`CoworkAttachmentsOutOfStep`](#coworkattachmentsoutofstep) | warning | a tenant's latest consistency check found files whose bytes are missing or objects no file names, and they stayed for `metrics.prometheusRule.restoreWindow` (`24h` `# default`) |

`metrics.prometheusRule.restoreWindow` is the one threshold that is a value: how long a restore takes
to be settled is the installation's
([ADR 0060](../adr/0060-prometheus-metrics-on-a-second-listener-with-servicemonitor-and-prometheusrule.md)
D6). The alert on the age of the last export comes with the export; nothing watches the export's
schedule yet ([backups.md](backups.md#the-export-the-second-line)).

## The dashboard

`metrics.grafanaDashboard.enabled` renders a ConfigMap `<fullname>-dashboard` with one dashboard,
`cowork.json`, for a Grafana sidecar that loads labelled ConfigMaps: `metrics.grafanaDashboard.labels`
is `grafana_dashboard: "1"` `# default`, the label and value kube-prometheus-stack's sidecar looks
for, and `metrics.grafanaDashboard.annotations` carries what your sidecar reads beside it, a folder
for one. The dashboard picks a Prometheus data source and a namespace, and has a row each for HTTP,
the database, the background jobs, the event stream, the audit and the login, the attachments'
consistency by tenant id, and the process — the last reads the series of the container `backend`,
the label the operator's targets carry. Not run
here: a Grafana loading it.

## nginx's numbers

The frontend's configuration serves nginx's `stub_status` on `127.0.0.1:8082` in every image:
connections active, reading, writing and waiting, connections accepted and handled, requests. The
loopback address is the pod's own, so nothing outside the pod reaches it
([ADR 0060](../adr/0060-prometheus-metrics-on-a-second-listener-with-servicemonitor-and-prometheusrule.md) D7;
verified against the built image on 2026-10-06: `127.0.0.1:8082` answers inside the container, its
own address and another container are refused).

`frontend.metrics.exporter.enabled` adds the sidecar `nginx-exporter`, NGINX's
`nginx/nginx-prometheus-exporter` (`1.5.3` `# default`), which reads `stub_status` and answers a
scrape on the container port `nginx-metrics` (`9113` `# default`) with `nginx_up`,
`nginx_connections_*`, `nginx_http_requests_total` and its own Go runtime's series. It runs as the
pod's user with a read-only root filesystem and has no probe; like any container it counts toward
the pod's readiness, so an exporter that cannot start — a wrong image, a pull that fails — keeps the
frontend pod out of its Service. Its resources are `frontend.metrics.exporter.resources`. The
`PodMonitor` scrapes it; the `ServiceMonitor` variant does not.

## The network policy for the port

The chart ships no NetworkPolicy
([ADR 0001](../adr/0001-two-containers-a-go-backend-and-an-nginx-frontend-installed-by-one-helm-chart.md)
D3), so every pod of the cluster reaches the metrics port of the backend pods — and the exporter's
of the frontend pods. What the port tells is
[docs/security/metrics.md](../security/metrics.md); admit the monitoring namespace to it and nothing
else:

```yaml
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: cowork-backend-metrics                  # example
  namespace: cowork                             # example: the release's namespace
spec:
  podSelector:
    matchLabels:
      app.kubernetes.io/name: cowork
      app.kubernetes.io/instance: cowork        # example: the release's name
      app.kubernetes.io/component: backend
  policyTypes: [Ingress]
  ingress:
    - from:
        - namespaceSelector:
            matchLabels:
              kubernetes.io/metadata.name: monitoring   # example: where Prometheus runs
      ports:
        - protocol: TCP
          port: 8081                            # metrics.port
```

A pod that a policy selects for ingress admits only what some policy admits: this policy alone
cuts the backend off from the Ingress controller. Write it beside the policy that admits the
controller to port 8080 ([installation.md](installation.md#network-policies-are-the-clusters)); the
two add up. A `podSelector` beside the `namespaceSelector` narrows it to Prometheus's own pods. With
the exporter on, the same rule for the frontend pods on port 9113. Not run against a network plugin
that enforces policies.

## CoworkSchemaDirty

**What it means.** The version table records its version as dirty: a migration began and did not
finish — it failed halfway, or it is still running after ten minutes. The pods that run keep serving,
because they checked the schema when they started; a pod that starts now refuses to — its `migrate`
init container ends with `migration failed`, and the server with `database check failed`, both naming
the version as dirty ([runtime.md](runtime.md#the-migration-run)). A rollout, a rescheduled pod or a
crash stops at it.

**What to do.** Find the failed run: the log of the `migrate` init container of the newest backend
pod (`kubectl logs <pod> -c migrate`) names the migration and the error. A migration runs in one
transaction, so a failed one left nothing behind: fix the cause, set the version back to the one
before (`UPDATE schema_migrations SET version = <N-1>, dirty = false`, as the owner role) and let the
next pod apply it again ([runtime.md](runtime.md#the-migration-run)). A migration that is only slow
needs no repair; the alert ends when it is done.

## CoworkEventStreamDrops

**What it means.** A pod keeps dropping event streams that fell behind: a stream holds 256 events,
and one whose client did not take them as fast as they came was told `resync` and ended
([ADR 0054](../adr/0054-server-sent-events-per-tenant-carry-keys-not-content-polling-is-the-fallback.md)
D4). Each such client reloads every list it shows and opens a new stream — load on the backend, and
live updates missed in between. A few drops are a client on a slow line; drops in every ten minutes
for a quarter of an hour are a pattern.

**What to do.** On the dashboard, set the drops beside the events published: a burst of acts — an
import, a script or an agent writing in a loop — larger than a stream's buffer drops every stream of
its tenant, and is over when the burst is. Steady drops without bursts point at the way to the
clients: the Ingress controller must pass `text/event-stream` unbuffered
([installation.md](installation.md#expose-it)); check that the controller's read timeout sits above
twenty seconds and that nothing between it and the browsers holds responses back. The buffer is no
setting. `reason="limit"` — a person's oldest stream closed beyond
`COWORK_SSE_MAX_STREAMS_PER_PERSON` — and `reason="resync"` — the listener came back after losing its
connection, [runtime.md](runtime.md#the-event-stream) — are not counted by this alert.

## CoworkDatabasePoolExhausted

**What it means.** Every connection of a pod's pool has been in use for ten minutes while requests
waited for one. Requests slow down, and those that wait past `COWORK_REQUEST_TIMEOUT` answer
`504 timeout`; the event streams' recomputations and the jobs wait in the same queue.

**What to do.** Look for what holds the connections: the backend's `slow query` lines name the
queries slower than 500 ms, the dashboard's query errors by kind show timeouts and cancellations, and
the database's `pg_stat_activity` shows what the runtime role is doing and what it waits for. A pool
that is simply too small for the load grows with `pool_max_conns=<n>` in `COWORK_DATABASE_URL` — the
default is the greater of 4 and the number of CPUs the process sees — or with more replicas, each
with a pool of its own; every replica's maximum together must stay below the database's
`max_connections`, with room for the migrations and the event listener, one connection per
replica.

## CoworkJobFailing

**What it means.** A background job failed at its last two runs on a pod — the jobs run at start
and every hour. The label `name` is the job: `idempotency-expiry`, `session-expiry`, `login-expiry`,
`notification-expiry`, `github-delivery-expiry`, `import-expiry`, `ticket-purge`, `consistency-check` — asked every hour whether it is due,
and due again every hour while it fails —, or `bootstrap`, the start's synchronisation, which ends the
process when it fails, so its alert shows only as a pod that does not start. While a job fails,
what it removes stays: stored responses of idempotent requests, sessions past their limits — which are
refused at their next request all the same —, failed login attempts and ended locks, notifications
read long ago, and — `ticket-purge` — the tickets deleted more than thirty days ago, which stay in
their tenant's bin past the thirty days they promise
([ADR 0024](../adr/0024-deletion-tickets-are-soft-deleted-and-purged-projects-archived-people-deactivated-tenants-deleted-explicitly.md)
D2).

**What to do.** The pod's log says `job failed` with the same job name and the error. A database
that does not answer shows on `/readyz` as well; a refusal of a policy or a grant after an upgrade
is a defect to report with the line. `consistency-check` failing with `Access Denied` at `list the
objects` is a storage key without `s3:ListBucket` on the bucket
([installation.md](installation.md#object-storage)). The alert ends with the job's next success, an
hour later.

## CoworkAttachmentsOutOfStep

**What it means.** The latest consistency check of the tenant with the id in the label `tenant`
found files whose metadata is there and whose bytes the bucket lacks, or objects under the tenant's
prefix that no file names, and they have stayed for longer than the restore window
([ADR 0059](../adr/0059-backups-belong-to-the-operators-cowork-provides-the-export-and-makes-a-restores-inconsistency-visible.md)
D4). The value is the two counts together; a loss an administrator accepted counts in neither. The
usual cause is a restore of the database and the bucket from two points in time; an orphan alone is
also what a removal that failed after a purge leaves. The tenant's people see a file that answers its
download with `404` saying the bytes are missing, or nothing at all — an orphan is a file no ticket
lists any more.

**What to do.** Find the tenant: the label is its id; the slug is in the log line `the attachments of
a tenant are out of step with the bucket` of the check's run, and in the output of
`cowork check-consistency`. Its administrators see the lists on its settings page and settle them
there — putting lost bytes back or accepting their loss, copying orphans out and removing them —, as
[backups.md](backups.md#the-consistency-check) says. Bytes put back show at the next check, which
`cowork check-consistency` runs at once; an acceptance or a removal shows within a minute. The alert
ends when the counts are zero.
