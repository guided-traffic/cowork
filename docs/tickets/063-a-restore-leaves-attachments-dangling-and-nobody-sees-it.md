---
id: T63
title: a restore leaves attachments dangling and nobody sees it
state: in-progress    # filed -> analysed -> decided -> in-progress -> done | dropped
severity: medium      # a restore that loses files goes unnoticed until a reader opens one
security: none
threat:
urgency: release      # gated on the export of T57, which brings the last instrument and alert of this list
effort: S
blocked-by: T57
filed-from: phase 7, ADR 0059 D4-D6 and the consistency family of ADR 0060 D4-D6
opened: 2026-10-06
decided:
done:
---

## Current state

Built on 2026-10-06 (ADR 0059 D4–D6, ADR 0060 D4–D6 for the consistency family, details made
concrete in both records): the daily job `consistency-check`
([`store/consistency.go`](../../backend/internal/store/consistency.go)) under
[migration 42](../../backend/internal/store/migrations/000042_attachment_consistency.up.sql); the routes
`GET`, `POST …/orphan-removal` and `POST …/dangling-acceptance` under
`/api/v1/tenants/{tenant}/attachment-consistency`
([`api/consistency.go`](../../backend/internal/api/consistency.go)); the section *Files and the
bucket* on the tenant's settings page; `cowork check-consistency`; the gauges
`cowork_consistency_dangling_attachments` and `cowork_consistency_orphaned_objects`, the dashboard row,
the alert `CoworkAttachmentsOutOfStep` with `metrics.prometheusRule.restoreWindow`; the pages
[backups.md](../operations/backups.md) and [upgrade.md](../operations/upgrade.md), the runbook, the
developer and security pages (H-68 to H-71).

What is left:

- **The last export's age.** ADR 0060 D4 lists the seconds since the last export in the
  `consistency` family, and D6 the alert on an export older than a configurable number of days;
  ADR 0059's residual risks want the last export's time on the administration page. Nothing records
  when an export was fetched until the export of T57 is integrated, so neither is built.
- **The chart's example script.** `deploy/examples/minio-bucket.sh`, which the chart-references work
  adds, makes a key without `s3:ListBucket`; with it the check fails every hour
  ([installation.md](../operations/installation.md#object-storage)).
- **The migration's number.** 43 follows 40 to 42 of the night's other work; on this work alone
  six tests fail on the gap — the unit tests `TestMigrationFilesAreWellFormed`,
  `TestCountVersionsBetween` and `TestSchemaStatePending`, the integration tests
  `TestMigrateBringsFreshDatabaseToCurrentVersion`, `TestRankMigrationKeepsNumberOrder` and
  `TestStagesMigrationBackfill`, each counting the files as the versions —, and the unit and the
  integration tier pass whole with the file numbered 40 (checked 2026-10-07).

## Required changes

### Once the export of T57 is integrated

- `internal/metrics`: the third gauge of the family, the seconds since a tenant's last export, read
  from the database at a scrape like the two counts, by tenant id; `tenantLabelled` in
  `metrics_test.go` names it; a panel in `dashboard.go`; `make generate`.
- The chart: the alert on the age, with its number of days a value of `metrics.prometheusRule`, and
  its runbook section in [docs/operations/metrics.md](../operations/metrics.md); a `promtool` rule test
  as for `CoworkAttachmentsOutOfStep`.
- The tenant's settings page: the time of the last export beside the check.
- ADR 0060's Status and D4, D6; ADR 0059's residual risks; README, Metrics and Helm values;
  [backups.md](../operations/backups.md), which says that nothing watches the schedule yet.

## Open questions

### Q1: Does the removal of a check's orphaned objects take a browser session only, or an administrator's token as well?

Nothing undoes the removal. Options: (a) a browser session only — built: a token, an `admin` token
included, is `403 session_required`, as for the purge of a ticket, by the rule of ADR 0035 D5 that
an act whose effect outlives a leaked token's revocation takes a session; the cost is that no script
confirms a removal, so a restore's automation stops at the UI; (b) a session or an `admin`-scope
token, never an agent — what the brief of the work asked for; a script after a restore removes the
orphans, and a leaked administrator's token removes the bytes of files uploaded between two
snapshots, which may be their only copy. **Recommended: (a)**, built: it is the record's own rule
applied to an irreversible act, and the removal is meant to follow a person's look at the list.

**Answer:** _open_

### Q2: How does a loss the administrators accept stop counting, and the alert end?

ADR 0059 D5 says "accept the dangling metadata or re-upload" and names no act. Options: (a) an
administrator's recorded acceptance of the listed files — built: they count as accepted, not
dangling, stay on their tickets with their honest `404`, and an acceptance is forgotten when the
bytes come back; a table and a route more, and an acceptance can silence a real loss (H-70); (b)
nothing in cowork: the count stays until the bytes are back, and the operator silences the alert in
Alertmanager, whose silence expires; (c) removing the dangling metadata, so the files leave their
tickets — destructive, and against D4's honest `404`. **Recommended: (a)**, built: it makes the
alert resolvable without hiding the loss from the tenant, and it removes nothing.

**Answer:** _open_

### Q3: Is the metrics' tenant label the tenant's id or its slug?

ADR 0060 D5 allows the label and names no value. Options: (a) the id — built: the metrics port has
no authentication, and the id names no client; an alert's reader looks the slug up in the check's
log line or in `cowork check-consistency`; (b) the slug: readable alerts and dashboards, and the list
of the installation's clients readable by every pod that reaches the port, which the API keeps even
from a global administrator's token (ADR 0035 D5). **Recommended: (a)**, built, for that reason.

**Answer:** _open_

### Q4: Does the check need `s3:ListBucket`, or does it work without it?

Orphans cannot be found without a listing. Options: (a) the key needs `s3:ListBucket` — built and
documented; the key can then enumerate every tenant's object keys, so a key that leaks alone reads
every object (H-68); AWS S3 documents a `403` instead of a `404` for a missing object to a key without
it, which would make the honest `404` of a lost file a `500` there; (b) the check works without it:
the missing files by a `HEAD` per attachment, the orphans unchecked and said so on the page — more
code, a check that sees half, the key as narrow as before; (c) a second, list-only key for the
check — a configuration value against D6. **Recommended: (a)**, built: D4 needs the listing, and the
key's added reach matters only for a key that leaks without the database's credential, which names
every key as well.

**Answer:** _open_

### Q5: How does an operator run the check at once after a restore?

D5 says "run the check" and names no trigger; a restart does not run it when the restored database
records a run after the latest 03:00 UTC. Options: (a) `cowork check-consistency` through
`kubectl exec` into a backend container — built: the restore is the operator's work, and it adds no
route and no authorization; it needs the right to exec into the pod, and runs a second process
within the container's memory limit; (b) a global administrator's route — in the UI, and a new
installation-wide act to authorize; (c) a tenant administrator's "check now" for their tenant — useful
after bytes are put back, and an S3 listing anybody of the tenant's administrators can start at will.
**Recommended: (a)**, built.

**Answer:** _open_

## Not verified

- The check against an object store other than the MinIO of the tests, and AWS S3's documented `403`
  for a missing object without `s3:ListBucket` — what would settle it: a run against S3.
- A restore of a real installation by the steps of [backups.md](../operations/backups.md), and the
  CronJob of that page in a cluster.
- One transaction for the whole run on a large bucket — its time, and a database whose
  `idle_in_transaction_session_timeout` is shorter than a tenant's listing.
- `kubectl exec … check-consistency` in a cluster: that the process gets the container's
  environment, and its memory beside the server's within the chart's 256 MiB limit.
- A Prometheus scraping a release and firing `CoworkAttachmentsOutOfStep`; `promtool` parsed the
  rendered rules and fired it in a rule test.
- The settings page's section in a browser; its specs run in jsdom.

## Related

- T57 — the export, which brings the last export's age.
