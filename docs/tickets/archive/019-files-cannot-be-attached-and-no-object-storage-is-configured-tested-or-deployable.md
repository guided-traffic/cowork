---
id: T19
title: files cannot be attached to tickets or comments, and no S3 storage is configured, tested or deployable
state: done
severity: high
security: hardening
threat: decides the stored type from the bytes against an allow-list, serves every file only through the backend after the ticket's read check with nosniff, a sandbox CSP and attachment disposition for everything but raster images, and refuses oversized and over-count uploads before anything is stored — additionally covering a file served as active content under cowork's origin and an upload that exhausts the backend's memory
urgency: later        # rule 4: decided fix (the ADRs)
effort: L
blocked-by: T16
filed-from: the phase-2 conversion (T2)
opened: 2026-10-02
decided: 2026-10-02
done: 2026-10-02
shipped: attachments in S3 over minio-go with detection, limits, delivery headers and audited downloads, make minio-up, the chart's storage values (existingConfigMap moved to phase 7)
---

## Current state

Decided by [ADR 0016](../adr/0016-attachments-live-in-s3-compatible-storage-and-are-served-only-through-the-backend.md)
D1–D6, D8, [ADR 0026](../adr/0026-one-append-only-audit-table-written-by-the-request-layer.md)
D5, [ADR 0039](../adr/0039-no-request-budgets-size-and-time-limits-instead-configurable-and-switchable.md)
D2, D3, [ADR 0045](../adr/0045-idempotency-put-where-it-is-free-a-required-key-on-agent-posts-stored-with-the-act.md)
D3, D6, [ADR 0058](../adr/0058-postgresql-and-object-storage-are-external-the-chart-takes-references-with-configurable-keys.md)
D1–D3, D5, [ADR 0059](../adr/0059-backups-belong-to-the-operators-cowork-provides-the-export-and-makes-a-restores-inconsistency-visible.md)
D4 and [ADR 0038](../adr/0038-no-development-login-switch-the-development-environment-is-the-real-login-path.md)
D2, D5.

- No storage client, configuration, table or test server.
- The `minio/minio` images can no longer be pulled. `cgr.dev/chainguard/minio`, a MinIO build
  Chainguard publishes, pulls anonymously (verified), so the records that name MinIO —
  ADR 0038 D2, [ADR 0056](../adr/0056-end-to-end-playwright-against-the-built-containers-with-two-identities.md)
  D1, ADR 0058 D1, D2 — stay true. Its entrypoint is the `minio` binary with no default
  arguments: it needs `server /data` as its command, locally and as a CI service.
- ADR 0016's Status leaves D3–D7 open "until the first implementation makes them concrete".
  `net/http.DetectContentType` (Go 1.26.5) reports Markdown and patches as
  `text/plain; charset=utf-8`, an SVG without a prolog as `text/plain` and with `<?xml` as
  `text/xml`, and HTML as `text/html` (verified).
- The backend runs with a read-only root filesystem and a `256Mi` memory limit
  ([`values.yaml`](../../deploy/helm/cowork/values.yaml), `backend.securityContext` and
  `backend.resources`): no multipart parsing into temporary files.
- A retried multipart upload carries a new boundary, so T8's raw-body fingerprint would answer
  `422` to an honest retry.
- ADR 0016 D6 names a per-tenant quota "reported in the tenant's administration"; its residual
  risk says it is not enforced, and no phase-2 route reports it.

## Required changes

1. **Configuration:** `COWORK_S3_ENDPOINT`, `COWORK_S3_BUCKET`, `COWORK_S3_REGION`,
   `COWORK_S3_ACCESS_KEY_ID`, `COWORK_S3_SECRET_ACCESS_KEY` (never echoed),
   `COWORK_S3_USE_PATH_STYLE` (ADR 0016's Consequences), `COWORK_S3_CA` for a private authority,
   and `COWORK_ATTACHMENT_MAX_PER_TICKET` (D6's per-ticket count, `0` disables); endpoint, bucket
   and both keys all or none; none → uploads answer `501`, lists and metadata still work (ADR 0016
   D1).
2. **The client:** `minio-go` or `aws-sdk-go-v2`, chosen in this ticket by a suite against the
   test server (a put with a known size, a streamed get, delete, list by prefix, path-style
   addressing, a custom CA); the choice and its reason go into ADR 0016.
3. **The test server:** `make minio-up` and `make minio-down` with `cgr.dev/chainguard/minio`
   pinned by digest and started with `server /data`; the same server in the integration job — a
   service container, or a step that starts it on the job's Docker daemon if the runners do not
   honour a service's command; `COWORK_TEST_S3_*` variables, the tier failing when they are unset
   ([ADR 0003](../adr/0003-test-and-ci-policy.md) D3); a Renovate rule that moves the digest.
4. **Migration:** `attachments` (ticket, an optional comment of the same ticket by a composite
   key, file name, size, SHA-256, the detected type on the allow-list, uploader and agent mark,
   the generated `search` column over the file name (ADR 0025 D5), time); no version — an
   attachment never changes; the tenant policy. The object key `<tenant-id>/<attachment-id>` is
   derived, not stored.
5. **Upload** `POST …/tickets/{number}/attachments` (`multipart/form-data`, one `file`, an
   optional `comment_id`): member, `write`; an agent needs `upload`; a comment's attachment only
   by the comment's author or that person's agents. Read with a multipart reader; a
   `Content-Length` above the maximum refused at once; the file buffered up to maximum + 1 byte
   (`413` before anything is stored); concurrent uploads bounded against the memory limit; the
   type from the bytes — PNG, JPEG, GIF, WebP, PDF, `text/plain; charset=utf-8`, and text or XML
   whose root element is `svg`, stored as `image/svg+xml` — anything else `415` naming the
   detected type; the per-ticket count (`409`); the file name sanitised (no path components,
   control characters or `"`, NFC, at most 255 bytes); the metadata row in `Mutate`, then the
   object, then the commit — a failed put rolls back, a failed commit deletes the object
   best-effort; the idempotency fingerprint over the file's SHA-256, the name and `comment_id`;
   the act `uploaded`.
6. **Download** `GET …/attachments/{id}/content`: the ticket's read check, then streamed;
   `Content-Type` as detected, `X-Content-Type-Options: nosniff`,
   `Content-Security-Policy: sandbox`, `Content-Disposition: inline` for the raster types and
   `attachment` with an RFC 6266 file name for everything else, `ETag` the SHA-256;
   `If-None-Match` → `304`, not audited; every `200` writes `downloaded` (ADR 0026 D5); a missing
   object → `404` whose `detail` says so (ADR 0059 D4). `GET` list and metadata.
7. **Chart** (ADR 0058 D3): `storage.existingSecret` with `keys.accessKeyId` and
   `keys.secretAccessKey`; `storage.endpoint`, `.bucket`, `.region`, `.pathStyle`, or
   `storage.existingConfigMap` with `keys.*`; `storage.tls.caConfigMap` with `keys.ca`; a `ci/`
   values file.
8. **Tests:** the allow-list fixture (unit; ADR 0016's residual risk); file names;
   `Content-Disposition`; the all-or-none configuration; the round trip with the object key
   exactly `<tenant-id>/<id>` and size and hash stored; PNG bytes declared `text/plain` stored as
   PNG, HTML declared `image/png` refused naming `text/html`; the delivery headers per type; above
   the maximum `413` with neither object nor row; the count limit; uploads refused without
   storage; a viewer downloads and cannot upload; an agent without `upload` refused; every
   download audited; a retry with a new boundary replays; a dangling object answers the detailed
   `404`; the restriction and confidential rows; a 10 MiB upload through both images (nginx sized
   by T6); the cross-tenant rows.
9. **Docs and records:** new docs/security/attachments.md — what an upload can and cannot do;
   its gaps with the next free `H-<n>`, the first ADR 0016 D8's: no virus scanning; then file
   names as metadata (D1 sanitises, a name still says something), no per-tenant quota enforced
   (the per-file maximum and the per-ticket count are the bounds), a plain `http://` endpoint
   inside the cluster, the memory a burst of uploads holds; installation.md (the bucket and its
   access key, ADR 0058 D5); new docs/developer/storage.md and its row in the developer README;
   testing.md (the S3 server and its variables); the README's configuration, values and API; ADR
   0016 (the allow-list, the SVG rule, the count, the client), ADR 0058 (the storage references,
   the test image and why) and ADR 0059 D4 (the detailed `404`) Status and index rows.

## Not verified

- Whether the ARC runners honour a service container's command; whether older Chainguard
  digests stay pullable without a subscription.
- kin-openapi with a multipart body; T5's validator would skip the body (`ExcludeRequestBody`).

## Related

- T1 — the runners the integration job runs on.
- T6 — nginx's body size above the attachment maximum.
- T16 — comments, which attachments may belong to.
- T21 — the export lists attachment names.
