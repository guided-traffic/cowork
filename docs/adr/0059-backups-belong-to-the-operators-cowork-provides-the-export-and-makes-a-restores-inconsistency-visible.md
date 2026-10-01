# ADR 0059: Backups Belong to the Operators of the Database and the Object Store; cowork Provides the Export and Makes a Restore's Inconsistency Visible

## Status

Accepted. Date: 2026-10-01. Decided by the owner as the answer to the catalog question
"backups?": no backup code in cowork, the export as the second line and a consistency check
after restore, over cowork-made backups, over automatic exports into the same object store,
and over documentation alone. The rules of D4–D6 were put to the owner with the question and
not objected to.

**Not built.** No export, no consistency check.

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
the purge job only after an administrator confirms the list.

**D5 — The restore procedure is a step list in `docs/operations/`,** honest about the
non-transactional gap: restore the database to its point in time first, then the bucket to
the nearest point at or after it; run the consistency check; read its summary; accept the
dangling metadata or re-upload. The page says plainly that an attachment uploaded between the
two snapshots is the one that will be missing.

**D6 — Export and consistency check need no new configuration.** The export is an API route;
the check uses the storage configuration that exists; its schedule is a constant (daily, at a
fixed UTC hour) until an installation asks for a value.

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
  D1.

## References

- [ADR 0058](0058-postgresql-and-object-storage-are-external-the-chart-takes-references-with-configurable-keys.md) — the two external systems
- [ADR 0051](0051-import-is-a-server-side-two-phase-atomic-job-export-is-its-mirror.md) D4, D5 — the export and its round-trip test
- [ADR 0016](0016-attachments-live-in-s3-compatible-storage-and-are-served-only-through-the-backend.md) D1, D2 — the tenant prefix and the attachment lifecycle
- [ADR 0027](0027-data-access-is-sqlc-over-pgx-behind-a-tenant-transaction-and-a-mutation-wrapper.md) D5, [ADR 0024](0024-deletion-tickets-are-soft-deleted-and-purged-projects-archived-people-deactivated-tenants-deleted-explicitly.md) D2 — the job scheduler and the purge
- [ADR 0035](0035-personal-access-tokens.md), [ADR 0026](0026-one-append-only-audit-table-written-by-the-request-layer.md) D5 — the token the CronJob uses and the recorded export
