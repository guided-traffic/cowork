# Upgrading

What an operator does, in order, to move an installation to a newer release, and how to tell where
the schema stands. The commands, the two Helm flags for the values and what each release changes are
[installation.md, Upgrade](installation.md#upgrade); how a migration runs and fails is
[runtime.md, the migration run](runtime.md#the-migration-run). The rules are
[ADR 0028](../adr/0028-migrations-only-go-forward-no-down-files-expand-before-contract.md) and
[ADR 0057](../adr/0057-migrations-on-start-by-default-a-helm-hook-job-as-the-switchable-alternative.md).

## Before

1. **Read the release's notes** in [installation.md, Upgrade](installation.md#upgrade): a release that
   asks for something of the installation first — a policy, a permission, a controller's limit —
   says so there.
2. **Take a backup** as [backups.md](backups.md) says, of the database at least. A migration only
   goes forward: there is no `migrate down` and no down file (ADR 0028 D1, D2), so the way back to the
   schema before an upgrade is the database's restore — and with it the bucket's, and the
   consistency check after both.
3. **Look at the schema's state.** A dirty one stops every pod of the upgrade
   ([below](#a-dirty-schema)); repair it first. The state is on the metrics port of any backend pod:

   ```bash
   kubectl -n cowork port-forward deploy/cowork-backend 18081:8081 &   # example namespace and release
   curl -s localhost:18081/metrics | grep '^cowork_migrations'
   # cowork_migrations_schema_dirty 0
   # cowork_migrations_schema_version 43
   ```

   or, as the owner role, `SELECT version, dirty FROM schema_migrations;`.

## The order of an upgrade

**The new release's migrations run before its servers start**, in the init container of each new
backend pod — or in the chart's migration Job, where the installation runs the migrations as a Job
([installation.md](installation.md#how-the-schema-is-migrated)). Pods that migrate together take turns
on an advisory lock; the first applies, the others find the schema current.

**The old pods keep serving the newer schema** while the rollout lasts, and that is safe for one
release: a migration of a release never drops, renames or narrows what the release before it reads —
expand before contract (ADR 0028 D3) —, and a contract comes in a later release, once no supported
binary reads the old shape. Across a jump of several releases the oldest pods may meet a schema they
cannot serve until they are replaced; where a release's notes name a contract, go release by
release.

**Reading where it stands while it runs:**

| Where | Says |
|---|---|
| `kubectl -n cowork logs <pod> -c migrate`, or the migration Job's log where it runs as one | `database schema is current` with `version` and `applied` — the files this run applied, `0` for a pod that found another's work done —; `database schema is ahead of this binary; nothing applied` for an older image; `migration failed` with the error |
| `cowork migrate`, run by hand with both roles' URLs | the same lines, then exit `0`, or `1` on a failure |
| the serving container's log | `database check failed` with `pending migrations: N` — the migrations did not run before it — or `schema version N is dirty` |
| `cowork_migrations_schema_version`, `cowork_migrations_schema_dirty` | the version table as a scrape reads it, at most every ten seconds; the dirty flag is `1` while a migration runs, and stays `1` when one failed — [`CoworkSchemaDirty`](metrics.md#coworkschemadirty) fires after ten minutes |

**Done** when `kubectl -n cowork rollout status deploy/cowork-backend deploy/cowork-frontend` returns,
`GET /api/v1/version` answers the new version and the metrics' schema version is the release's
newest migration.

## Rolling back

**Rolling back is rolling the image back** (ADR 0028 D4): the previous release's image over the
newer schema, which D3 keeps servable for one release — the previous image's migration run finds the
schema ahead and applies nothing, and its server serves it with a warning. How, with Helm, and what a
release asks of its rollback are [installation.md, Upgrade](installation.md#upgrade). A rollback across
more than one release is not supported: a migration keeps only the release directly before it
working.

The other way back is the backup taken before: restore the database — and the bucket after it, then
run the consistency check ([backups.md](backups.md#a-restore-step-by-step)). It loses what was
written since the backup.

## A dirty schema

A migration that failed halfway leaves the version table's version marked dirty. Nothing repairs it
by itself — not the pods, not the migration Job (ADR 0057 D7): every pod that starts refuses with
`schema version N is dirty`, while the pods that run keep serving. A person finds the cause in the
failed run's log, fixes it, and sets the version back so the next run applies the file again; the
steps are [runtime.md, the migration run](runtime.md#the-migration-run), the alert's runbook is
[metrics.md, CoworkSchemaDirty](metrics.md#coworkschemadirty).
