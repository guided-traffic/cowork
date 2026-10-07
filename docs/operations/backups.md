# Backups and a restore

What keeps a copy of an installation — not cowork —, how the export is fetched as the second line,
what the daily consistency check finds after a restore, and the restore step by step. The decision
is [ADR 0059](../adr/0059-backups-belong-to-the-operators-cowork-provides-the-export-and-makes-a-restores-inconsistency-visible.md);
the routes and the values are [README.md](../../README.md#api-backend); what the lists of the check
tell, and to whom, is [docs/security/attachments.md](../security/attachments.md#the-consistency-check).

## cowork makes no backup

cowork ships no backup endpoint, no scheduler for backups and no `pg_dump` in its image (D1). Its
state lives in two systems the installation provides
([ADR 0058](../adr/0058-postgresql-and-object-storage-are-external-the-chart-takes-references-with-configurable-keys.md)),
and each is backed up by its own tools:

| What | Lives in | Is backed up by |
|---|---|---|
| tenants, people, projects, tickets, comments, time, the audit record, the attachments' names and sizes | the PostgreSQL database | its operator: continuous archiving with point-in-time recovery where the database has it — CloudNativePG's backups, for one —, `pg_dump` otherwise. The two roles and their passwords are not part of a database's dump (`pg_dumpall --roles-only`) |
| the attachments' bytes | the bucket, `<tenant-id>/<attachment-id>` | the object store: versioning, replication, the provider's snapshots |
| the server key, the database URLs, the storage key | the Kubernetes Secrets you created | whoever keeps the cluster's Secrets. A server key that is lost is replaced as a rotation is ([installation.md](installation.md#the-secrets)) |

The two copies are never taken in the same transaction, which is what [the consistency
check](#the-consistency-check) and [the restore](#a-restore-step-by-step) are about.

## The export, the second line

The export of a tenant or a project is a copy a person can read without cowork (D2):
`GET /api/v1/tenants/{tenant}/export` answers a `tar.gz` with every project of the tenant the caller
sees, each ticket as its Markdown document, a manifest, the links and the attachments' names, types
and sizes — never their bytes, which only the bucket's backup keeps
([ADR 0051](../adr/0051-import-is-a-server-side-two-phase-atomic-job-export-is-its-mirror.md) D4);
`GET /api/v1/tenants/{tenant}/projects/{project}/export` the same for one project. Every export is a
recorded act (D3). This page describes the export as the release that brings this page builds it.

**The installation fetches it, on its own schedule.** cowork does not export by itself and does not
watch the schedule — the age of the last export as a metric, with its alert, comes later. Give the
fetch a personal access token of its own (D2):

- `read` scope, and **restricted to the tenant** — a token restricted to a project cannot fetch the
  tenant's export; one restricted to nothing reaches every tenant of its person.
- **Of a person who sees what the copy must hold.** The export holds what its caller sees: a tenant
  administrator sees every project and every confidential ticket, anybody else's copy leaves out what
  they cannot see and counts the confidential tickets it left out in its manifest.
- Made in that person's browser session — `POST /api/v1/me/tokens` takes no token — and with a
  lifetime at most `COWORK_TOKEN_MAX_LIFETIME`: a fetch fails with `401 token_expired` the day it
  ends, so put its renewal in your calendar.

A CronJob that fetches one tenant every night is one way; it is an illustration to adapt, not a
manifest the project maintains:

```yaml
apiVersion: batch/v1
kind: CronJob
metadata:
  name: cowork-export-acme                       # example
  namespace: cowork                              # example
spec:
  schedule: "30 2 * * *"                         # example: daily, 02:30 in the controller's time zone
  concurrencyPolicy: Forbid
  jobTemplate:
    spec:
      backoffLimit: 2
      template:
        spec:
          restartPolicy: Never
          containers:
            - name: export
              image: curlimages/curl:8.16.0      # example
              env:
                - name: TOKEN
                  valueFrom:
                    secretKeyRef:
                      name: cowork-export-token  # example: the token, key token
                      key: token
              command: ["/bin/sh", "-c"]
              args:
                - >-
                  curl -fsS -H "Authorization: Bearer $TOKEN"
                  -o /backup/acme-$(date -u +%Y%m%d).tar.gz
                  https://cowork.example.com/api/v1/tenants/acme/export
              volumeMounts:
                - name: backup
                  mountPath: /backup
          volumes:
            - name: backup
              persistentVolumeClaim:
                claimName: cowork-exports        # example: where your backups are kept
```

Keep the archives where your other backups are, not in the bucket cowork writes to, whose loss they
insure against. Not run against a cluster here.

## The consistency check

A database and a bucket restored from two points in time disagree: a file whose row came back
without its bytes — **dangling** —, and bytes no row names — an **orphan** (D4). The check finds
both, per tenant, and changes nothing.

**When it runs.** One replica runs it once a day, in the hour after 03:00 UTC; and a pod that starts
runs it when the last run, as the database records it, lies before the latest 03:00 UTC — so a new
installation's first start runs it, and so does a start after a day without one. No value changes
the schedule (D6). Without object storage it never runs.

**What it does.** For each tenant it lists the objects under `<tenant-id>/`, reads the tenant's
attachments, and asks the bucket again for each attachment the listing did not show. An object no
attachment names is an orphan unless its attachment id was made within the last hour — an upload
puts its bytes before its row commits. The result replaces the tenant's last one; the run is one
database transaction, under a lock that keeps the replicas from running it twice. The storage key
needs `s3:ListBucket` for the listing ([installation.md](installation.md#object-storage)). The
listing and the attachments are compared a thousand objects at a time, in the order of their keys,
so what the check holds does not grow with the number of a tenant's objects; it relies on the store
listing the keys in byte order, as S3 does, and a store that lists them otherwise fails the run —
the job's log says `job failed` with `consistency-check` and `the comparison needs the keys in byte
order`, and `cowork check-consistency` logs `consistency check failed` with that error and exits `1`.

**Running it at once** — after a restore, or after putting lost bytes back:

```bash
kubectl -n cowork exec deploy/cowork-backend -c backend -- /app/cowork check-consistency
```

It reads the container's own configuration, refuses a schema it cannot serve, and prints the run
and every tenant, by slug and id — counts, never a file name:

```
consistency check at 2026-10-06T09:14:03Z: 3 tenants, 1 dangling, 0 accepted as lost, 2 orphaned objects (5242880 bytes)
tenant acme (0199a7c2-1d2e-7f00-8000-0000000000aa): 1 dangling, 0 accepted as lost, 2 orphaned objects (5242880 bytes)
tenant globex (0199a7c2-1d2e-7f00-8000-0000000000bb): 0 dangling, 0 accepted as lost, 0 orphaned objects (0 bytes)
```

It exits `1` when another replica runs the check at that moment — run it again a minute later. A
restart is no way to run it: a restored database whose last run lies after the latest 03:00 UTC is
not due.

**Where the findings show:**

| Where | Shows | To whom |
|---|---|---|
| the tenant's settings page, *Files and the bucket*; `GET /api/v1/tenants/{tenant}/attachment-consistency` | the counts, the missing files with their names and tickets, the orphans' keys, sizes and times, at most a thousand of each | the tenant's administrators |
| `cowork check-consistency` | every tenant's counts | whoever may exec into a backend pod |
| the log | `consistency check done` with the counts in all; `the attachments of a tenant are out of step with the bucket` at warn with the tenant's slug and counts | the operator |
| the metrics | `cowork_consistency_dangling_attachments` and `cowork_consistency_orphaned_objects` per tenant id; the alert [`CoworkAttachmentsOutOfStep`](metrics.md#coworkattachmentsoutofstep) when they stay above zero for the restore window | Prometheus |
| the audit record | one installation-level act `checked` of `system:consistency-check` per run, with the counts per tenant id | a database administrator; no route answers it |

**Settling it** is the tenant administrators' work, on the settings page:

- **A missing file** — dangling — answers its download with `404` and a detail that its bytes are
  missing. Put the bytes back under the key the list names, from an older backup of the bucket
  (`mc cp ./bytes <store>/<bucket>/<tenant-id>/<attachment-id>`), and the next check finds it whole;
  or have the file uploaded again and **accept the loss** of the old entry: it counts as accepted
  from then on, no longer as dangling, holds no alert, and stays listed on its ticket. A file of a
  ticket in the bin goes with its ticket at the purge.
- **An orphan** is bytes no ticket shows — usually a file uploaded between the two snapshots, whose
  row the restored database does not have. Whoever runs the bucket can copy it out by the key the
  list names, to upload it to its ticket again. Then an administrator **removes the orphans** in a
  browser session: the page asks twice, each object is asked again whether a file names it by then —
  one that does stays —, and the objects are gone for good.

## A restore, step by step

Rehearse it on a copy before you need it: a procedure nobody has run is not a backup (D5).

1. **Stop the backend**, so that nothing writes between the two restores:
   `kubectl -n cowork scale deploy/cowork-backend --replicas=0`. The frontend may keep running; its
   page says the backend cannot be reached.
2. **Restore the database first**, to the point in time you choose — with the database operator's
   tool. Note the time it is at.
3. **Then restore the bucket to the nearest point at or after that time**, with the store's tool.
   Not before it: a file the database names and the bucket lost is a file nobody gets back, while
   an object the database does not name is still there to be copied out or removed.
4. **Start the backend** — `kubectl -n cowork scale deploy/cowork-backend --replicas=<n>`. A restored
   database at an older schema version is migrated forward by the pods as at an upgrade; one at a
   newer version than the image is served with a warning ([upgrade.md](upgrade.md)).
5. **Run the check at once** with `cowork check-consistency` ([above](#the-consistency-check)).
6. **Read its summary**: the counts per tenant. Tell each tenant's administrators that their settings
   page lists what is missing and what is left over.
7. **Settle it**: put lost bytes back or accept the loss; copy out the orphans worth keeping, then
   remove them ([settling it](#the-consistency-check)). The alert waits the restore window — a day by
   default — before it fires on what is still unsettled.

**An attachment uploaded between the two snapshots is the one that will be missing.** Its row and
everything else written after the database's point in time — tickets, comments, the upload itself —
are not in the restored database, so no ticket lists the file; its bytes are in the bucket as an
orphan, which the check names by key and size, never by name. A file purged between the two
snapshots comes back as a row in the bin whose bytes are gone; the purge removes it again when its
ticket's thirty days are over.

## What the check and this page leave to you

- The database's and the bucket's own backup, retention and encryption, which are their operators'
  and checked by nothing in cowork.
- The age of the last export: nothing in cowork knows when an export was last fetched.
- Objects under the prefix of a tenant the restored database does not know: the check lists the
  prefixes of the tenants it knows only
  ([attachments.md H-69](../security/attachments.md#h-69)).
