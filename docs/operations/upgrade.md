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
   # cowork_migrations_schema_version 42
   ```

   or, as the owner role, `SELECT version, dirty FROM schema_migrations;`.

## The order of an upgrade

**The new release's migrations run before its servers start**, in the init container of each new
backend pod (`migrations.mode: onStart`, the default) — or, with `migrations.mode: job`, in the
chart's migration Job, a `pre-upgrade` hook that migrates and then runs the bootstrap before any new
pod starts, and fails the release when it fails ([installation.md](installation.md#job-mode)). Pods
that migrate together take turns on an advisory lock; the first applies, the others find the schema
current.

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

## The release that relates tickets across teams

A ticket's parent, its children and its links may be tickets of another project or team from this
release on ([ADR 0005](../adr/0005-a-tenant-is-a-client-organisation-and-the-isolation-unit.md) D3,
as amended 2026-10-10). Its migration, 47, rewrites no row: it widens two keys and adds an audit
action, functions, a trigger and policies for the owner role, named as the role that runs it — so it
runs as every migration does, as the owner of the tables. A database whose crossing policies or
functions are not that role's is refused by `cowork serve` at its start
([runtime.md](runtime.md#the-backend)).

**While two releases write at once** — the rolling update over migration 47 — and after a rollback to
the release before, the release before walks the parents and the `blocks` links within one team,
under locks that do not exclude this release's, derives a parent's progress from its own team, and
ends the relations into other teams at a purge without an act in their record
([H-112](../security/tenancy.md#h-112)). Keep the window short and relate nothing across teams in it;
a cycle that a writer of each release closes between them stays until a person removes one of its
edges.

## The release that calls a tenant a team

A team was called a tenant until this release
([ADR 0005](../adr/0005-a-tenant-is-a-client-organisation-and-the-isolation-unit.md) D1, as amended
2026-10-10). This release says team on every surface an operator, a person or an agent reads, and
keeps each name before beside the new one for this release, deprecated and working as it did, so
the upgrade itself needs nothing from you. The database keeps its names, and the release brings no
migration. A later release removes the names before: move what you run before it.

**What to move**, at your pace within this release:

| The name before | Its replacement | Where you have it |
|---|---|---|
| `COWORK_BOOTSTRAP_TENANT_SLUG`, `COWORK_BOOTSTRAP_TENANT_NAME`, `COWORK_ATTACHMENT_TENANT_QUOTA` | `COWORK_BOOTSTRAP_TEAM_SLUG`, `COWORK_BOOTSTRAP_TEAM_NAME`, `COWORK_ATTACHMENT_TEAM_QUOTA` | the backend's environment outside the chart, `backend.extraEnv` |
| `bootstrap.tenant.slug`, `bootstrap.tenant.name`, `backend.config.attachmentTenantQuota` | `bootstrap.team.slug`, `bootstrap.team.name`, `backend.config.attachmentTeamQuota` | your values |
| the label `tenant` of `cowork_consistency_dangling_attachments`, `cowork_consistency_orphaned_objects` and `cowork_consistency_last_export_age_seconds` | `team`, the same id — in an alert of your own only where no routing label is named `team`, else the id copied to a label of another name with `label_replace` | dashboards, recording rules and alerts of your own; the chart's own alerts and dashboard read `tenant` in this release, the label every image of a rollback window carries ([metrics.md](metrics.md#the-alerts)) |
| `/api/v1/tenants` and every path under `/api/v1/tenants/{tenant}` | `/api/v1/teams`, `/api/v1/teams/{team}/…` | scripts and CronJobs — the export's of [backups.md](backups.md#the-export-the-second-line) among them |
| the query parameter `tenant`; the properties `tenant`, `tenants`, `restricted_tenant`; `group_by=tenant` | `team`; `team`, `teams`, `restricted_team`; `group_by=team` | clients of the API |
| `tenant:` in a repository's `.cowork.yaml` | `team:` | repositories — only once every machine runs a `cowork-mcp` of this release, below |
| `create_project`'s argument `tenant`, `search`'s scope `tenant` | `team` | instructions that name them to an agent |
| `tenant` of `cowork-mcp token check --json`, `Tenant` of `cowork-mcp lookup --json` | `team`, `Team` | scripts that read them |

The values and the API's names are listed in full in [README.md](../../README.md#api-backend), the
variables under [deprecated variable names](../../README.md#deprecated-variable-names).

**In this order:**

1. **The installation first, then the people's `cowork-mcp`.** A `cowork-mcp` of this release calls
   `listTeamTickets`, which 0.14 does not serve, and refuses every tool against it, naming the
   operation. A `cowork-mcp` of 0.14 keeps working against this release: the deprecated twins keep
   the operations it calls under their names before.
2. **A `.cowork.yaml` says `team:` only once every machine that works in the repository runs a
   `cowork-mcp` of this release.** A `cowork-mcp` of 0.14 knows no `team`: it ignores a file that
   names it — with `tenant` beside it or without — and notes that in the session block, binding by
   the remote if it can. This release reads `tenant:` alone, and both keys when they name the same
   slug; the file `create_project` offers in this release says `tenant:` for that reason.
3. **Scripts and CronJobs on `/api/v1/teams` once a rollback is off the table**: an image of 0.14
   answers that family `404`.
4. **Your values and variables, dashboards and alerts**, before the release that removes the names
   before.

**What warns.** A variable set under its name before, alone, makes `cowork serve`, `cowork migrate`
and `cowork check-consistency` log at their start, at warn, `a variable is set under its deprecated
name; set the variable that replaces it, since a later release no longer reads the name before`,
with `variable` and `replaced_by`. A variable set under both names with the same value — the quota
compared as a size, `1GiB` as `1073741824` — does not warn — and that is what the chart renders for this release, whichever of the two values you set, so
that an image rolled back to 0.14, which reads only the names before, keeps them. A value of the
chart set under its name before is named instead by the chart's notes after the install or upgrade,
with its replacement. Both names set to different values refuse the start, or the render, naming
both.

**What changes at once, with no name before kept:** the log's words — `the attachments of a team
are out of step with the bucket` with the attribute `team`, `consistency check done` with `teams`,
`an orphaned object could not be removed` with `team`, `team boundary failed`, `the bootstrap team is
created` —, the lines `cowork check-consistency` prints (`… N teams …`, `team <slug> (<id>): …`), the
problems' titles and details (`Team slug taken`, `no such team`; their codes stay), and every label
of the UI. A log query or a parser on the words before needs moving with the upgrade. The UI's
addresses stay: `/t/<slug>/…` keeps its path, and no bookmark breaks.

**Rolling back to 0.14** is an image rollback as for every release ([above](#rolling-back)): the
chart renders the bootstrap and the quota under both names, so 0.14 keeps them; its metrics carry
`tenant` alone, which the chart's alerts and dashboard read; a `cowork-mcp` of this release refuses
it, and a script on `/api/v1/teams` gets `404`. An image uploaded and embedded in a ticket's body or
a comment under this release is addressed under `/api/v1/teams/…`: under 0.14 it shows as a link,
and the address answers `404`, until the image is current again — nothing is lost. An export
archive of this release names its team under `tenant` as well, so 0.14 imports it; this release
imports an archive of 0.14, and of every release before, for good.
