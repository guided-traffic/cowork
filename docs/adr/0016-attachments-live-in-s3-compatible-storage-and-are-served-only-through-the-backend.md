# ADR 0016: Attachments Live in S3-Compatible Storage and Are Served Only Through the Backend, With a Sniffed Type, an Allow-List and No Inline Rendering Except Raster Images

## Status

Accepted. Date: 2026-09-29. Decided by the owner as the answer to the catalog question
"attachments?": attachments on tickets and comments in the first release, in S3-compatible
storage, over links only, over bytes in PostgreSQL, and over deferring the feature. The
recommendation put to the owner was to defer; the owner chose to build it now.

The protection rules of D3–D7 are this record's proposal for implementing that choice
safely; they were not part of the question and stay open to the owner's objection until the
first implementation makes them concrete. They are the minimum under which foreign bytes may
be stored by one person and shown to another.

Amended 2026-10-02 (D1: what the sanitised name keeps; D3: the allow-list as built and the SVG
rule; D5: the name's extension follows the detected type; D6: the per-ticket count and its
default, the maximum switchable off; the client). The first implementation made D3–D6 concrete:
`net/http.DetectContentType` reports Markdown and patches as UTF-8 plain text and an SVG as
plain text or XML, so an SVG is recognised by its root element; and the client was chosen by
a suite against a MinIO test server.

Amended 2026-10-04 by the owner's decision that every act made through a token is shown as such
([ADR 0036](0036-a-token-acts-as-its-person-an-agent-flag-is-the-floor-the-agent-header-only-narrows.md) D6): D1, a file's metadata names the token it was uploaded through. Built the same day
([migration 27](../../backend/internal/store/migrations/000027_acts_through_a_token.up.sql)).

Amended 2026-10-05 (D6: the tenant's quota refuses; the residual risk of a quota only reported
amended) by the answer to "does the tenant's attachment quota refuse an upload, or is it only
reported?". The options were (a) enforce — a quota per tenant in a variable, `0` for none, the
tenant's stored bytes summed and checked under a per-tenant lock before the bytes are stored, a
refusal with a code of its own, the usage in the administration; and (b) report only — the usage in
the administration, no refusal, D6's refusal narrowed to the per-file and per-ticket limits. (a) was
the recommendation, and it was built on the owner's instruction of 2026-10-05 to build the
recommended option, the owner reviewing the result.

Amended 2026-10-06 (D7 and the References: they say that
[ADR 0011](0011-a-ticket-is-a-markdown-body-plus-first-class-open-questions.md) D6 states the image
rule in place instead of claiming to amend it, by the owner's rule that every amendment is made in place
in the record it changes; no rule changes).

Amended 2026-10-10 by the owner's rename of a tenant to a team
([ADR 0005](0005-a-tenant-is-a-client-organisation-and-the-isolation-unit.md) D1): D6, the quota's
variable is `COWORK_ATTACHMENT_TEAM_QUOTA`, in the chart `backend.config.attachmentTeamQuota`. The
names before, `COWORK_ATTACHMENT_TENANT_QUOTA` and `attachmentTenantQuota`, are still read for one
release: the variable alone with a warning in the log that names its replacement; the two set to
different values are a configuration error, and the two values a failed render, naming both. Made
concrete by the implementer the same day, open to the owner's objection: the variable beside the
new one at the same value is read without a warning, because the chart renders both, so that an
image rolled back to the release before, which reads the name before alone, keeps the quota. Built the same day
([`config.go`](../../backend/internal/config/config.go) `renamedVariables`, the chart's
`cowork.attachmentTeamQuota`); the removal of the names before is a later release's.

**Partly built** (phase 2, 2026-10-02): D1–D6 and D8 — [`internal/storage`](../../backend/internal/storage/)
over `minio-go`, the `attachments` table (migration 14), upload, list, metadata and download
under the ticket's path. ~~D6's per-tenant quota is neither enforced nor reported~~ *(built
2026-10-05, below)*; D7 arrives
with the sanitiser of [ADR 0011](0011-a-ticket-is-a-markdown-body-plus-first-class-open-questions.md) D6.
*(Built 2026-10-05.)* D7 with that sanitiser: an image in a rendered text shows only when its address
names a raster attachment of the same ticket — PNG, JPEG, GIF, WebP — and is then served from that
attachment's own path; an SVG, another ticket's attachment and any other address are rendered as a
link, which a non-raster attachment answers as a download (D5)
([docs/security/rendered-markdown.md](../security/rendered-markdown.md#images)).
*(2026-10-04.)* In the browser: an upload to a comment of the person's own, and the preview of a
raster attachment — an image of its own URL, inline by D5, never an SVG — each load of which is a
recorded download.

**Built** (phase 3, 2026-10-05): D6's per-tenant quota as amended that day —
~~`COWORK_ATTACHMENT_TENANT_QUOTA`~~ `COWORK_ATTACHMENT_TEAM_QUOTA` *(2026-10-10)*, the check under the tenant's lock
([`api/attachments.go`](../../backend/internal/api/attachments.go) `lockQuota`, `withinQuota`),
`409 attachment_quota`, the usage at `GET /api/v1/tenants/{tenant}/attachment-usage` and on the
tenant's settings page in the browser.

*(2026-10-06.)* The project and the tenant export carry the attachment manifest of the
Consequences ([ADR 0051](0051-import-is-a-server-side-two-phase-atomic-job-export-is-its-mirror.md) D4): each attachment's ticket, name, type, size
and the path of its bytes, never the bytes (D5). The importer uploads nothing: a document's
`attachments:` list is reported and not brought.

## Context

A bug ticket wants a screenshot, an agent wants to leave a diagram, a client's member wants to
hand over a PDF. The Markdown tickets cowork imports have none of these; the owner's workflow
keeps artefacts in repositories. The owner nevertheless wants attachments in the first
release. An upload is the one place where a team product accepts arbitrary bytes from one
person and delivers them into another person's browser — inside the same origin as the
session cookie ([ADR 0001](0001-two-containers-a-go-backend-and-an-nginx-frontend-installed-by-one-helm-chart.md)
D3, D4). A file that is called an image and is HTML, an SVG with a script, a PDF with an
embedded form action: each is a stored cross-site-scripting or phishing vector unless the
server decides what a file is and how it is delivered.

## Decision

**D1 — Attachments belong to a ticket or a comment, inside a tenant, and their bytes live in
S3-compatible object storage.** The object key is `<tenant-id>/<attachment-id>`; the bucket
is private; metadata — id, tenant, ticket or comment, original file name (sanitised), size,
SHA-256, detected content type, uploader, agent mark, *(added 2026-10-04, [ADR 0036](0036-a-token-acts-as-its-person-an-agent-flag-is-the-floor-the-agent-header-only-narrows.md) D6)* the token
it was uploaded through — its id and name, none for a browser session —, timestamp — lives in
PostgreSQL. Bytes
never enter PostgreSQL. *(Amended 2026-10-02: the sanitised name is the last path component in
NFC, without control characters, bidirectional controls, double quotes or invalid UTF-8, at
most 255 bytes; a right-to-left override would otherwise let `txt.exe` read as `exe.txt`.)* Storage is configured through `COWORK_S3_*` variables; an
installation without them refuses uploads with a clear error and the UI hides the control.

**D2 — Access follows the ticket.** Whoever may read the ticket may download its
attachments; whoever may comment may upload. A soft-deleted ticket keeps its attachments
until the purge; a tenant's deletion deletes its prefix.

**D3 — The server decides the content type.** The client's declared type is ignored. The
type is detected from the bytes (magic-number sniffing) and must be on an allow-list: raster
images (PNG, JPEG, GIF, WebP), PDF, plain text and Markdown, patches, and archives only if a
later amendment says so. SVG is stored as a file and never treated as an image. Anything
else is refused at upload with the detected type in the error. *(Made concrete 2026-10-02: the allow-list is PNG, JPEG, GIF, WebP,
PDF and UTF-8 plain text — which is what Markdown and patches are detected as — and SVG, stored
as `image/svg+xml` when the bytes are text or XML whose root element is `svg`; the refusal is
`415` naming the detected type; the list is a test fixture,
[`domain_test.go`](../../backend/internal/domain/domain_test.go).)*

**D4 — Bytes are served only through the backend**, at an attachment endpoint under the
ticket's path, after the same authorization check as the ticket, streamed from storage. No
presigned URL reaches a browser in the first release: a presigned URL is a bearer credential
that outlives the session check that issued it.

**D5 — Delivery headers make the browser treat the bytes as data.** Every attachment response
carries `X-Content-Type-Options: nosniff`, `Content-Security-Policy: sandbox`, and the
detected type. Raster images are delivered inline; **everything else is delivered with
`Content-Disposition: attachment`** and never rendered by the browser inside cowork's origin.
PDF is a download, not an embedded viewer. *(Amended 2026-10-02: the stored name ends in an
extension of the detected type — a name that ends in none gets the type's first appended,
`run.bat` stored as text becomes `run.bat.txt` — so a download never offers the recipient's
system an ending it would run.)*

**D6 — Limits.** A per-file maximum (`COWORK_ATTACHMENT_MAX_BYTES`, default 10 MiB), a
per-ticket count, and a per-tenant quota reported in the tenant's administration; uploads
beyond a limit are refused before bytes are stored. *(Made concrete 2026-10-02: the per-ticket count is
`COWORK_ATTACHMENT_MAX_PER_TICKET`, default 100, `0` for none, refused with `409
attachment_limit`; an upload is buffered in memory up to the maximum and one byte more, and the
uploads in flight share a budget of 64 MiB, because the backend has no writable disk. The
count is checked under a per-ticket lock, so simultaneous uploads cannot pass it together. A
maximum of `0` switches the per-file limit off ([ADR 0039](0039-no-request-budgets-size-and-time-limits-instead-configurable-and-switchable.md)
D2); one upload at a time is then read whole, whatever its size.)* *(Amended 2026-10-05: the
per-tenant quota refuses as the other limits do. ~~`COWORK_ATTACHMENT_TENANT_QUOTA`~~
`COWORK_ATTACHMENT_TEAM_QUOTA` *(2026-10-10)* is the bytes one
tenant's attachments hold together, a size such as `10GiB`; `0` sets none and is the default —
no figure suits every installation, one tenant's has the bucket as its bound, and an upgrade must
not start refusing uploads that worked before; an installation of several tenants sets it. Where it
is set, an upload takes the tenant's quota lock before the ticket's, sums the sizes of every
attachment of the tenant — every ticket's, a confidential ticket's and a restricted project's
included, and a deleted ticket's until the purge removes it
([ADR 0024](0024-deletion-tickets-are-soft-deleted-and-purged-projects-archived-people-deactivated-tenants-deleted-explicitly.md)
D2), because its files occupy the bucket until then — and refuses a file that would take the sum
above the quota with `409 attachment_quota`
before a row or an object exists; the refusal names the quota and the file's size, never the sum.
The lock is held until the upload commits, so the tenant's uploads pass the check one after the
other, and one tenant's never wait for another's. The quota is "reported in the tenant's
administration" as the usage — the sum, the number of files and the quota — which the tenant's
administrators read, and nobody else, because the sum counts files of tickets a member may not
see.)*

**D7 — Markdown may embed a raster-image attachment of the same ticket, and nothing else.**
The sanitiser of [ADR 0011](0011-a-ticket-is-a-markdown-body-plus-first-class-open-questions.md)
D6 allows `<img>` sources only on the attachment endpoint, which ADR 0011 D6 states in place.
A link to any other attachment renders as a download link.

**D8 — No virus scanning in the first release, and this is said aloud.** The defences of
D3–D5 mean that nothing uploaded executes on the server and nothing but a raster image is
rendered in a browser under cowork's origin; a malicious PDF or archive is a download the
recipient opens on their own machine, as it would be from an e-mail. A scanning hook is the
amendment when an installation needs it.

## Consequences

- A second persistence system: an S3-compatible endpoint (MinIO, a cloud bucket, a
  Kubernetes-hosted store) with its credential in a Secret, its own backup path, and chart
  values. How it is provisioned is a new catalog question.
- New configuration: `COWORK_S3_ENDPOINT`, `COWORK_S3_BUCKET`, `COWORK_S3_REGION`,
  `COWORK_S3_ACCESS_KEY_ID`, `COWORK_S3_SECRET_ACCESS_KEY`, `COWORK_S3_USE_PATH_STYLE`,
  `COWORK_ATTACHMENT_MAX_BYTES` — the README's table grows in the change that builds it.
  *(Added 2026-10-02:)* `COWORK_S3_CA` for a private authority and
  `COWORK_ATTACHMENT_MAX_PER_TICKET`; endpoint, bucket and both keys come together or not at
  all, and without them uploads answer `501 uploads_disabled` while lists and metadata work.
- *(Added 2026-10-02:)* the client is `minio-go` v7 over `aws-sdk-go-v2`: a put with a known
  size, a streamed get, delete, path-style addressing and a custom authority are each one
  option, at a fraction of the dependency tree. The test server is ~~the MinIO build Chainguard
  publishes (`cgr.dev/chainguard/minio`, pinned by digest)~~ *(amended 2026-10-09 by
  [ADR 0058](0058-postgresql-and-object-storage-are-external-the-chart-takes-references-with-configurable-keys.md) D2:)* PGSTY Silo, the maintained MinIO
  fork (`docker.io/pgsty/silo`, pinned by release tag and digest), started with `server /data`; the
  client stays `minio-go`.
- A security page of its own under `docs/security/` ("what an upload can and cannot do"),
  written in the change that builds the feature, with D8 as its first open gap.
- The backend streams every download; large files and many readers cost backend bandwidth.
  Presigned URLs are the amendment if that ever matters, and the amendment has to answer how
  a presigned URL is revoked with the session.
- The export of ADR 0011 D4 gains an attachment manifest; the importer of the Markdown
  tickets uploads nothing, because they have nothing.
- SVG diagrams from an agent are downloads, not pictures, until an SVG sanitiser is decided.

## Alternatives Considered

- **No attachments in the first release; a link is the attachment** — the recommendation.
  No subsystem, no upload surface; a client without a repository would have had no place for
  an image. Lost by the owner's decision.
- **Bytes in PostgreSQL** (`bytea`, small files, images only). No second system, backups with
  the database; the database grows with pictures, dumps get heavy, and D3–D5 are needed all
  the same. Lost.
- **Presigned S3 URLs handed to the browser.** No backend bandwidth; a URL that keeps
  working after the session ends, and a second origin in the CSP. Lost to D4 for the first
  release.
- **Trusting the client's content type.** The classic stored-XSS mistake. Lost to D3.

## Residual risks

- D8: a malicious file reaches a person's machine as a download. Mitigated by delivery as
  attachment, not by scanning.
- Metadata leaks through file names; D1 sanitises the name but a name can still say
  something. Accepted.
- D3's sniffing library is a dependency in the request path; its allow-list is a test
  fixture in the same way as the Markdown sanitiser's.
- ~~The tenant quota of D6 is reported, not enforced against a hard storage limit; a runaway
  upload loop is bounded by the per-file and per-ticket limits only.~~ *(Amended 2026-10-05: the
  quota refuses where it is set; it is off by default, and until an installation sets it a runaway
  upload loop — an agent with `upload` that files new tickets — is bounded by the per-file and
  per-ticket limits only, and one tenant can fill the storage every tenant shares. Where it is set,
  a member who uploads files of chosen sizes learns from the refusals how many bytes the tenant has
  left once it is within one file of the quota, a figure that counts files they cannot see
  ([docs/security/attachments.md](../security/attachments.md#h-10) H-10).)*

## References

- [ADR 0011](0011-a-ticket-is-a-markdown-body-plus-first-class-open-questions.md) D6 — the sanitiser, whose image rule names the attachment endpoint of D7
- [ADR 0005](0005-a-tenant-is-a-client-organisation-and-the-isolation-unit.md) D1 — the tenant in the object key
- [ADR 0004](0004-cowork-is-a-team-product.md) — the other person whose browser receives the bytes
- [ADR 0001](0001-two-containers-a-go-backend-and-an-nginx-frontend-installed-by-one-helm-chart.md) D3 — one origin, which is why delivery headers matter
