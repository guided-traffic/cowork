# ADR 0059: Backups Belong to the Operators of the Database and the Object Store; cowork Provides the Export and Makes a Restore's Inconsistency Visible

## Status

Accepted. Date: 2026-10-01. Decided by the owner as the answer to the catalog question
"backups?": no backup code in cowork, the export as the second line and a consistency check
after restore, over cowork-made backups, over automatic exports into the same object store,
and over documentation alone. The rules of D4–D6 were put to the owner with the question and
not objected to.

**Partly built** (phase 2, 2026-10-02): D3 for the one export that exists (a ticket's
`/markdown`) and D4's honest `404` for an attachment whose bytes are missing; ~~the project and
tenant export,~~ the consistency check, the restore steps and D6 arrive with the export.
*(2026-10-06.)* D2 and D3 are built for the project and the tenant export of
[ADR 0051](0051-import-is-a-server-side-two-phase-atomic-job-export-is-its-mirror.md) D4: `GET …/projects/{project}/export` and
`GET /api/v1/tenants/{tenant}/export` each record `exported` on what they exported, and
[docs/operations/import-and-export.md](../operations/import-and-export.md) shows the CronJob as a
snippet, with a `read`-scoped token restricted to the tenant; the export needs no configuration of
its own (D6). A restore of the tickets through the importer keeps what the archive carries, and
not the comments, the time or the attachments' bytes (ADR 0051 Residual risks).

**Built** (2026-10-06): D4 and D6 — the consistency check —, and D1, D2 and D5 as the operations
page [docs/operations/backups.md](../operations/backups.md). The project and the tenant export
of D2 and D3 come with the export of
[ADR 0051](0051-import-is-a-server-side-two-phase-atomic-job-export-is-its-mirror.md) D4, which the
same release brings and the page describes as built; the metric of the last export's age comes
with it. What the record left open is made concrete in place, in D4, D5 and D6, by the
implementer, open to the owner's objection:

- **D4** — the job `consistency-check` ([`store/consistency.go`](../../backend/internal/store/consistency.go)),
  [migration 42](../../backend/internal/store/migrations/000042_attachment_consistency.up.sql),
  `GET`, `POST …/orphan-removal` and `POST …/dangling-acceptance` under
  `/api/v1/tenants/{tenant}/attachment-consistency`
  ([`api/consistency.go`](../../backend/internal/api/consistency.go)), the section *Files and the
  bucket* on the tenant's settings page, the gauges `cowork_consistency_dangling_attachments` and
  `cowork_consistency_orphaned_objects` and the alert `CoworkAttachmentsOutOfStep`
  ([ADR 0060](0060-prometheus-metrics-on-a-second-listener-with-servicemonitor-and-prometheusrule.md)
  D4, D6).
- **D5** — the acceptance of a loss and `cowork check-consistency`, the step that runs the check
  at once.
- **D6** — the schedule, daily in the hour after 03:00 UTC and at a start that finds the last run
  older than that.
- Verified by the unit tests of the schedule, the judgement and the family, the integration tier —
  a restore's dangling row and orphans in one tenant and nothing in another, the summary, the
  counts on a second replica, the acceptance, the removal in a session that keeps an object
  that gained metadata, the refusals, the command line on the built binary — and the frontend's
  specs. Not run: the check against a store other than the MinIO of the tests, and a restore of a
  real installation.

**Amended** (2026-10-07): the comparison holds a bounded memory. D4's check compares the listing and
the rows as two ordered streams — the listing in the byte order of its keys, a thousand objects at a
time, and after each batch the rows its keys can name, read once the listing has passed them, so the
listing still comes before the rows — and keeps of a tenant the counts and the bytes, the first
thousand orphans and the files the listing did not show, whatever the number of its objects; a store
that lists out of byte order fails the run. What the check finds, keeps and shows is unchanged.
Verified by the unit tests of the comparison — held at several batch sizes to what the check found
when it read the whole listing and every row, and run over a synthetic tenant of a million objects
with the growth of the live heap below 8 MiB, where the whole listing and every row had taken
145 MiB —, by the tests of the check of both tiers, which pass unchanged, and by the integration
tier reading both orders back from PostgreSQL and MinIO. Not run: a tenant of that size against a
real store. The job lock D4 names was wrong when it was made
concrete: the code has held `(cowk, 8)` since the check was built.

## Context

cowork's state lives in two external systems — PostgreSQL and an S3-compatible store
([ADR 0058](0058-postgresql-and-object-storage-are-external-the-chart-takes-references-with-configurable-keys.md))
— each with backup tooling of its own that is better than anything a ticket tool would
build: CloudNativePG's continuous backups and point-in-time recovery, bucket versioning and
replication. What those tools cannot give is a human-readable second line and a view of the
one thing a two-system restore gets wrong: a database snapshot and a bucket snapshot are not
taken in the same transaction, so after a restore an attachment may have metadata without an
object, or an object without metadata. The export of [ADR 0051](0051-import-is-a-server-side-two-phase-atomic-job-export-is-its-mirror.md)
D4 is the second line; visibility of the gap is cowork's to provide.

## Decision

**D1 — cowork performs no backups.** Database backups are the database operator's
(continuous archiving and point-in-time recovery where available, `pg_dump` otherwise);
object-store backups are the store's (versioning, replication). cowork ships no backup
endpoint, no scheduler for backups, no `pg_dump` in its image.

**D2 — The export is the second line, triggered by the installation.** A project or tenant
export (ADR 0051 D4) is fetched on a schedule the operator runs — a CronJob calling
`GET …/export` with a `read`-scoped, tenant-restricted personal access token
([ADR 0035](0035-personal-access-tokens.md)) — and stored wherever the operator keeps
backups. The operations page shows the CronJob as a snippet inside the text; it is an
illustration, not a maintained manifest (the examples of ADR 0058 D1 are for the external
systems, not for backup jobs).

**D3 — Every export is a recorded act** ([ADR 0026](0026-one-append-only-audit-table-written-by-the-request-layer.md)
D5).

**D4 — A daily consistency check makes a restore's gap visible.** A background job
([ADR 0027](0027-data-access-is-sqlc-over-pgx-behind-a-tenant-transaction-and-a-mutation-wrapper.md)
D5) lists attachment metadata whose object is missing (dangling metadata) and objects under
a tenant's prefix without metadata (orphans), writes a summary into the installation-level
audit record, exposes both counts as metrics (the metrics record), and shows them in the
tenant's administration page. A download of a dangling attachment answers `404` with a
problem whose `detail` says the object is missing, never a bare `404`. Orphans are removed by
the purge job only after an administrator confirms the list. *(Made concrete 2026-10-06 by the
implementer, open to the owner's objection:)* the job is `consistency-check`, under the job lock
~~`(cowk, 7)`~~ `(cowk, 8)`, in one transaction: for every tenant it lists the objects under
`<tenant-id>/` — which takes `s3:ListBucket` on the bucket beside reading, writing and deleting
objects (confirmed by the owner 2026-10-09, over a check without the listing that sees only the
missing files, and over a second key for the listing) — then reads the tenant's attachments, asks the bucket for each attachment the listing
missed, and judges an object no metadata names an orphan unless the time in its key's UUIDv7, or
the last change of a key the backend does not write, lies within the last hour: an upload puts its
object before its row commits. *(Amended 2026-10-07:)* The listing and the attachments are
compared as two streams in the byte order of the keys, a thousand listed objects at a time, the
attachments a batch's keys can name read once the listing has passed them; the check holds a
bounded memory whatever the number of objects, and a store that lists out of that order fails the
run. The tenant's result replaces its last one under a new id — the counts
exact, each list at most a thousand entries, the dangling files with their names and tickets — and
is readable by the tenant's administrators and the job alone, under row-level security. The
installation-level act is `checked`, with the counts in all and per tenant by id, never a file
name or a key. The gauges are read from the stored results at a scrape, so every replica answers
the same. The removal is not the hourly purge job's but the confirming request's: the
administrator names the check whose list they were shown — a newer check, or a removal confirmed
already, is `409 consistency_check_stale` —, each orphan of that list is asked again in the
confirming transaction whether metadata names it now, the act `purged` is recorded with the
counts, and the objects are removed after its commit, as an administrator's purge of a ticket
removes its objects; a removal that fails is logged and listed again by the next check. It takes a
browser session and no agent, by the rule of
[ADR 0035](0035-personal-access-tokens.md) D5 for an act nothing undoes *(confirmed by the owner
2026-10-07, over a session or an `admin`-scope token: the orphans after a restore are often the only
copy of what the backup missed)*. The objects under the
prefix of a tenant the database does not know are not listed.

**D5 — The restore procedure is a step list in `docs/operations/`,** honest about the
non-transactional gap: restore the database to its point in time first, then the bucket to
the nearest point at or after it; run the consistency check; read its summary; accept the
dangling metadata or re-upload. The page says plainly that an attachment uploaded between the
two snapshots is the one that will be missing. *(Made concrete 2026-10-06 by the implementer,
open to the owner's objection; the trigger confirmed by the owner 2026-10-09, over a route for
global administrators and over a tenant administrator's "check now":)* the check runs at once with
`cowork check-consistency` in a backend container, which prints every tenant's counts; a restart does not run it when the restored
database records a run after the last 03:00 UTC. Accepting the dangling metadata is a tenant
administrator's recorded act, `accepted`, with `admin` scope and never an agent's: the listed
files count as accepted instead of dangling and hold no alert, nothing is removed, and their
download keeps answering that the bytes are missing; a file whose bytes come back is whole again,
and the check forgets its acceptance, so that a later loss counts once more. *(Confirmed by the
owner 2026-10-07, over leaving the count to Alertmanager's silences and over removing the dangling
metadata.)*

**D6 — Export and consistency check need no new configuration.** The export is an API route;
the check uses the storage configuration that exists; its schedule is a constant (daily, at a
fixed UTC hour) until an installation asks for a value. *(Made concrete 2026-10-06 by the
implementer, open to the owner's objection:)* the jobs ask every hour whether the check is due —
when no tenant has a result yet, or when the last run, as the database records it, lies before the
latest 03:00 UTC —, so it runs once a day in the hour after 03:00 UTC on whichever replica asks
first, and at a start that finds the last run older than that, which a run a day old always is.
Without object storage it never runs. The alert's restore window is a value of the chart
([ADR 0060](0060-prometheus-metrics-on-a-second-listener-with-servicemonitor-and-prometheusrule.md)
D6), not of the backend.

## Consequences

- No second backup truth and no backup code to keep correct; the operators' tools do what
  they are built for.
- The export doubles as migration between installations and as the human-readable copy of a
  tenant's work; the round-trip test of ADR 0051 D5 keeps it readable.
- A restore that is silently wrong is impossible: D4 names the missing objects and the
  orphans within a day, and the download path names the cause at once.
- The operations page carries a procedure that an operator can rehearse, which is the only
  backup that counts.

## Alternatives Considered

- **cowork-made backups** (`pg_dump` plus bucket mirror from the backend, a backup bucket, a
  schedule). A button; a backup product inside a ticket tool, a shell-less image to rebuild,
  retention, encryption and a restore path the operators already have better. Lost.
- **Automatic exports by cowork into its own object store.** No CronJob to configure;
  backups written into the system whose loss they insure against, or a second storage
  configuration. Lost; possible later as an option when an installation asks.
- **Documentation only.** No second line, no visibility; "the restore looked fine, the
  attachments are gone" is found by a person. Lost.

## Residual risks

- D2 depends on an operator actually scheduling the export; the operations page lists it in
  the installation checklist, and the installation-level audit shows the last export's time
  on the administration page, so a missing schedule is visible too.
- D4's orphan listing on a very large bucket is a prefix scan per tenant per day; at the
  expected sizes it is seconds, and the job is bounded by the per-tenant prefix of
  [ADR 0016](0016-attachments-live-in-s3-compatible-storage-and-are-served-only-through-the-backend.md)
  D1. *(Added 2026-10-06:)* The run holds one database transaction open for every tenant's
  listing; not measured on a large bucket. *(Added 2026-10-07:)* Its memory no longer grows with a
  tenant's objects; its time does — a listing request and a read of the attachments per thousand
  objects —, and the transaction stays open for all of it.
- *(Added 2026-10-06:)* The listing needs `s3:ListBucket`, so the access key can enumerate every
  tenant's object keys, which it could not before; a key that leaks alone then reads every object,
  not only those whose keys the database names
  ([docs/security/attachments.md](../security/attachments.md#h-68) H-68).

## References

- [ADR 0058](0058-postgresql-and-object-storage-are-external-the-chart-takes-references-with-configurable-keys.md) — the two external systems
- [ADR 0051](0051-import-is-a-server-side-two-phase-atomic-job-export-is-its-mirror.md) D4, D5 — the export and its round-trip test
- [ADR 0016](0016-attachments-live-in-s3-compatible-storage-and-are-served-only-through-the-backend.md) D1, D2 — the tenant prefix and the attachment lifecycle
- [ADR 0027](0027-data-access-is-sqlc-over-pgx-behind-a-tenant-transaction-and-a-mutation-wrapper.md) D5, [ADR 0024](0024-deletion-tickets-are-soft-deleted-and-purged-projects-archived-people-deactivated-tenants-deleted-explicitly.md) D2 — the job scheduler and the purge
- [ADR 0035](0035-personal-access-tokens.md), [ADR 0026](0026-one-append-only-audit-table-written-by-the-request-layer.md) D5 — the token the CronJob uses and the recorded export
