# Object storage

Where an attachment's bytes live and how they get there and back: the object key, the client,
the upload flow and its limits, the type detection, the delivery headers, the consistency check
of the bytes against their metadata, and the S3 server the tests run against. The decisions are
[ADR 0016] (attachments), [ADR 0058] (external storage, the chart's references) and [ADR 0059]
(the consistency check); the variables are in
[README.md, Configuration](../../README.md#configuration). Read against the tree on 2026-10-06.

## Keys and metadata

The metadata is a row of `attachments` (migration `000014_attachments`): ticket, optional
comment of the same ticket, file name, size, SHA-256, the stored content type, uploader, agent
mark and the token it was uploaded through, its id and name (migration 27, [ADR 0036] D6). It
carries no version and never changes. The object key is
`<tenant-id>/<attachment-id>` — `storage.Key`, derived and never stored. The bucket is private;
the bytes leave only through the backend, so nothing signs a URL (D4).

## The client

[`internal/storage`](../../backend/internal/storage/storage.go) wraps minio-go v7 for one bucket:

| Function | Does |
|---|---|
| `New(config.Storage)` | builds the client from `COWORK_S3_*`: TLS when the endpoint is `https://`; path-style bucket addressing (`COWORK_S3_USE_PATH_STYLE`, default true, as MinIO expects) or DNS-style; the region, or none to let the client ask; a PEM authority from `COWORK_S3_CA` added to the system pool, and then TLS 1.2 at least. It does not reach the server |
| `Put(ctx, key, r, size, contentType)` | stores the bytes |
| `Get(ctx, key)` | opens the object for streaming with its size; `ErrMissing` when the bucket does not hold it |
| `Delete(ctx, key)` | removes the object; a missing one is no error |
| `List(ctx, prefix)` | every object whose key begins with the prefix — current versions only — with its size and last change (`Object`); takes `s3:ListBucket`, the consistency check's alone |
| `Exists(ctx, key)` | whether the bucket holds the object, a `HEAD` |
| `ParseKey(tenantID, key)` | the attachment id a key names under the tenant's prefix, when the key is exactly what `Key` writes; any other key names none |
| `EnsureBucket(ctx)` | creates the bucket; only the tests call it — the operator provides the bucket ([ADR 0058] D5) |

Endpoint, bucket and both keys come together or not at all (`config.Load`). Without them
`cowork serve` logs a warning and `api.Options.Storage` is `nil`: uploads answer
`501 uploads_disabled`, the lists and the metadata still answer, and a download is `404`.

## Upload

`UploadAttachment` in [`attachments.go`](../../backend/internal/api/attachments.go), a
`multipart/form-data` `POST` with one `file` part and an optional `comment_id`:

1. No storage: `501`. Then the tenant-level authorization: a member with write scope, an agent
   needs `upload`.
2. A slot of the upload semaphore. Uploads are buffered in memory — the chart gives the backend
   256 MiB and no writable disk — so `uploadBudget` (64 MiB) divided by
   `COWORK_ATTACHMENT_MAX_BYTES` uploads are read at once per replica, at least one; with the
   maximum at 0, one.
3. The pipeline has already held the body to the maximum plus 64 KiB of multipart overhead. The
   handler reads the file up to the maximum plus one byte; more is `413 payload_too_large`, a
   second `file` or any other part is `400`. With the maximum at 0 the file is read whole.
4. The type is detected from the bytes (below); an unknown one is `415 unsupported_media_type`,
   naming what was detected. The name is sanitised and given an extension of the detected type.
5. The idempotency fingerprint covers the file's SHA-256, the name and the comment, keyed like
   every fingerprint ([api.md](api.md)).
6. In `Mutate`: the ticket through the predicate; where `COWORK_ATTACHMENT_TENANT_QUOTA` is set,
   the tenant's quota lock (`Writer.LockAttachmentQuota`, `lockQuota`), first, so the tenant's
   uploads check the quota one after the other; the ticket's attachment lock
   (`Writer.LockAttachments`), so simultaneous uploads to it count one after the other; the
   project role; a `comment_id` must name a comment of this ticket written by the caller's
   person; the ticket's attachment count must be below `COWORK_ATTACHMENT_MAX_PER_TICKET`
   (0: no limit), else `409 attachment_limit`; the tenant's attachments summed
   (`TenantAttachmentUsage`, every ticket's — the query is exempt from the predicate — and a
   deleted ticket's until the purge removes its rows: they occupy the bucket until then) plus the
   file must not exceed the quota, else `409 attachment_quota`, whose detail names the quota and
   the file's size and never the sum (`withinQuota`). Then the
   row and its `uploaded` act, then `Put`, then the stored `201` response.
7. When the object was written and the row did not commit — or a concurrent request with the same
   key committed first and this one replays — the object is deleted again: no row names those
   bytes.

`GetAttachmentUsage` (`GET /api/v1/tenants/{tenant}/attachment-usage`) answers the same sum, the
count and the quota to the tenant's administrators (`adminRead`), with a weak `ETag` and `304` for an
answer the client holds: the sum counts files of tickets a member may not see, and a deleted
ticket's until the purge. The tenant's settings page shows it
([frontend.md](frontend.md#the-tenants-administration)). The quota is off by default, and why is
[runtime.md](../operations/runtime.md#the-tenants-attachment-quota).

## Type detection

[`domain.DetectAttachmentType`](../../backend/internal/domain/attachment.go) judges the bytes
with `http.DetectContentType`, never the client's type (D3). Stored: `image/png`, `image/jpeg`,
`image/gif`, `image/webp`, `application/pdf`, and `text/plain; charset=utf-8` — Markdown and
patches are plain UTF-8 text. Text or XML whose root element is `svg` is stored as
`image/svg+xml` and delivered as a download, never inline. Anything else is refused. The table's
`CHECK` allows exactly these seven. `SanitizeFileName` keeps the last path component, drops
control characters, the bidirectional controls (a right-to-left override would make `txt.exe`
read as `exe.txt`) and double quotes, normalises to NFC and cuts at 255 bytes on a character
boundary; an empty result is `attachment`. `FileNameFor` then appends the detected type's first
extension to a name that ends in none of the type's (`run.bat` stored as text becomes
`run.bat.txt`), so a download never offers an ending the recipient's system would run.

## Download

`DownloadAttachment` reads the row through the ticket's predicate, then:

| Case | Answer |
|---|---|
| `If-None-Match` matches | `304`, not recorded |
| no storage configured, or the object is missing | `404`; a missing object says so — a restore can bring the database back without its bytes ([ADR 0059] D4) |
| otherwise | `200`, the bytes streamed, recorded as `downloaded` ([ADR 0026] D5) |

The UI shows a raster attachment as a preview: an `<img>` of its `content_url`, which the
browser loads with the session cookie, each load a `200` that is recorded
([frontend.md](frontend.md#the-detail-page)). It uploads to a comment from the comment, whose
author it must be (step 6 above).

The headers make the browser treat the bytes as data (D5): `Content-Type` is the stored type;
`Content-Disposition` is `inline` for the four raster types and `attachment` for everything else,
with the file name encoded by `mime.FormatMediaType` (RFC 2231 for what is not ASCII);
`X-Content-Type-Options: nosniff`; `Content-Security-Policy: sandbox`; `ETag` is the quoted hex
SHA-256; `Cache-Control: no-store` comes from the pipeline. `downloaded` acts are on neither the
activity list nor the event stream.

## The purge

The purge of a deleted ticket ([ADR 0024] D2, [data-access.md](data-access.md#deletion-and-the-purge))
deletes its attachment rows in its transaction and returns their ids; once that committed,
`api.RemovePurgedObjects` deletes each object, `<tenant-id>/<attachment-id>` — an administrator's
purge in its request, the job after its run. Removing first would leave rows that name missing
bytes after a rollback. An object whose removal fails, or one left because no object storage is
configured, stays in the bucket with no row naming it, and the log says which; the next
consistency check lists it as an orphan ([below](#the-consistency-check)). A deleted ticket's files
stay readable to nobody — every route of the ticket is `404` — and stay in the bucket until the
purge.

## The consistency check

The job `consistency-check` ([ADR 0059] D4, D6; [`store/consistency.go`](../../backend/internal/store/consistency.go))
compares each tenant's attachment rows with the objects under its prefix, after a restore that
brought the database and the bucket back from two points in time. Its schedule and its place among
the jobs are [data-access.md](data-access.md#jobs); its tables and policies
[data-access.md](data-access.md#the-consistency-checks-tables).

```
CheckConsistency(objects, now) ── RunJob(consistency-check, lock 7) ─┬─ ListTenantsToCheck
                                                                     └─ per tenant, inTenant:
   1 objects.List(<tenant-id>/)              the listing first
   2 ListTenantAttachmentIDs                 then the rows: an upload puts its object before its row commits
   3 judge                                   orphans: listed, no row, older than OrphanGrace; unlisted rows
   4 objects.Exists, per unlisted row        a put after the listing passed its key is no loss
   5 ListAcceptedAttachments, ForgetWholeAcceptances
   6 ListCheckedAttachments (at most 1000)   the missing files' names and tickets
   7 SaveConsistencyCheck                    the tenant's one row, under a new id
then one installation-level act `checked`, counts per tenant id
```

- **The grace.** An object no row names is judged only when its key's UUIDv7 was made more than
  `OrphanGrace`, an hour, before the run — a restore that rewrote the object keeps its key and with it
  that time —, or, under a key the backend does not write, when it last changed that long ago
  (`judge`, `young`). An object under any other key than `storage.Key`'s is an orphan: no row can
  name it.
- **The order.** The rows are read after the listing, so a row the read finds has its object in the
  listing unless the object was put after the listing passed its key, which `Exists` answers. A purge
  that committed between the listing and the read leaves its object as a short-lived orphan, which
  the purge's own removal ends.
- **The lists.** At most `ConsistencyListBound`, a thousand, of each; the counts are exact. The
  missing files are listed with the ones nobody accepted first. An acceptance whose attachment is
  whole again is forgotten (`ForgetWholeAcceptances`), so a later loss counts once more.
- **What it removes: nothing.** A tenant administrator confirms the removal of a result's orphans
  (`RemoveOrphanedObjects` in [`api/consistency.go`](../../backend/internal/api/consistency.go)): in
  the confirming transaction each listed orphan is asked again whether a row names it now
  (`ListAttachmentsAmong`) — one that does is kept —, a key outside the tenant's prefix is never
  touched, the act `purged` is recorded with the counts, and the objects go after the commit, as an
  administrator's purge removes its ticket's. The acceptance of the missing files
  (`AcceptDanglingAttachments`) inserts `consistency_acceptances` rows and moves the counts; it
  removes nothing either.
- **Immediately.** `cowork check-consistency` ([`main.go`](../../backend/cmd/cowork/main.go)
  `runCheckConsistency`) runs `CheckConsistency` once and prints every tenant's counts — what a
  restore runs ([docs/operations/backups.md](../operations/backups.md)).

The download of a dangling attachment answers its `404` before any of this
([above](#download)); the check is what finds it without a reader.

## The test server

The integration tier needs an S3-compatible server: `make minio-up` starts the Chainguard MinIO
image the [`Makefile`](../../Makefile) pins by digest, with `server /data` as its command, on
`localhost:9000` with the development keys `cowork` / `cowork-secret` (`# default`);
`make minio-down` removes it. `make test-integration` passes `COWORK_TEST_S3_ENDPOINT`,
`COWORK_TEST_S3_ACCESS_KEY_ID` and `COWORK_TEST_S3_SECRET_ACCESS_KEY`; `TestMain` fails without
them and creates a bucket of its own per run, `cowork-it-<nanoseconds>`, with `EnsureBucket`, and
empties and removes it when the run ends (`removeBucket` in the test package, minio-go directly:
the server never removes a bucket). The API tests run with a 1 MiB file maximum and five
attachments per ticket. The consistency check's tests put and delete objects in that bucket directly
and run the check with a clock two hours ahead, so that the objects they put are past the grace
([`api_consistency_test.go`](../../backend/test/integration/api_consistency_test.go)). CI starts the
same image with `make minio-up` ([ci-and-release.md](ci-and-release.md)).

`make run` sets no `COWORK_S3_*`, so a local backend refuses uploads unless they are exported,
and the bucket they name must exist — the server never creates it.

[ADR 0016]: ../adr/0016-attachments-live-in-s3-compatible-storage-and-are-served-only-through-the-backend.md
[ADR 0024]: ../adr/0024-deletion-tickets-are-soft-deleted-and-purged-projects-archived-people-deactivated-tenants-deleted-explicitly.md
[ADR 0026]: ../adr/0026-one-append-only-audit-table-written-by-the-request-layer.md
[ADR 0036]: ../adr/0036-a-token-acts-as-its-person-an-agent-flag-is-the-floor-the-agent-header-only-narrows.md
[ADR 0058]: ../adr/0058-postgresql-and-object-storage-are-external-the-chart-takes-references-with-configurable-keys.md
[ADR 0059]: ../adr/0059-backups-belong-to-the-operators-cowork-provides-the-export-and-makes-a-restores-inconsistency-visible.md
