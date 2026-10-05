# What an upload can and cannot do

What happens to a file someone attaches to a ticket or a comment — how its type is decided,
where its bytes live, how they are delivered back, what bounds an upload and what is recorded
— and what that leaves open, as built on 2026-10-02, the UI's preview on 2026-10-04, an image in a
rendered text on 2026-10-05. Who may read a ticket, and with it its
attachments, is [tenancy.md](tenancy.md); what a token or an agent may do, uploading included,
is [tokens.md](tokens.md); the network path between the containers is
[trust-boundaries.md](trust-boundaries.md).

## The type comes from the bytes

The type the client declares and the file name's extension are ignored. The server sniffs
the upload with `net/http.DetectContentType` — the WHATWG MIME sniffing algorithm over the
first 512 bytes — and stores it only when the result is on the allow-list
([`backend/internal/domain/attachment.go`](../../backend/internal/domain/attachment.go)
`DetectAttachmentType`;
[ADR 0016](../adr/0016-attachments-live-in-s3-compatible-storage-and-are-served-only-through-the-backend.md)
D3):

| Detected | Stored as | Delivered |
|---|---|---|
| PNG, JPEG, GIF, WebP | `image/png`, `image/jpeg`, `image/gif`, `image/webp` | inline |
| PDF | `application/pdf` | as a download |
| plain text — Markdown and patches among it | `text/plain; charset=utf-8` | as a download |
| plain text or XML whose root element is `svg` | `image/svg+xml` | as a download, never inline |

Anything else — HTML, XML with another root, archives, executables, binary data — is refused
with `415 unsupported_media_type` naming the detected type. The table is a unit test fixture
([`domain_test.go`](../../backend/internal/domain/domain_test.go)
`TestDetectAttachmentType`), and the database accepts no other type
([migration 14](../../backend/internal/store/migrations/000014_attachments.up.sql)). "Plain
text" is what the sniffer reports for bytes without binary control characters in that first
window: it does not check that the file is valid UTF-8 and does not look further. An SVG is
recognised by decoding the upload up to its first element; one that carries a script is stored
like any other SVG — as a download.

## The bytes leave only through the backend

- **Where they live.** The metadata — file name, size, SHA-256, detected type, uploader, agent
  mark, the token it was uploaded through (its id and name), ticket and comment — is a row of
  `attachments`; the bytes are an object in
  S3-compatible storage and never enter PostgreSQL (ADR 0016 D1).
- **The key.** An object's key is `<tenant-id>/<attachment-id>`, both made by the server;
  nothing from the request enters it ([`storage/storage.go`](../../backend/internal/storage/storage.go)
  `Key`). A download derives it from the attachment row the caller has read through the
  ticket's predicate, so an attachment of another tenant or of a hidden ticket is the `404` of
  one that does not exist.
- **No presigned URL.** Nothing in the backend signs one; every download streams through the
  backend after the ticket's read check (ADR 0016 D4). A presigned URL would be a bearer
  credential that outlives the check that issued it.
- **The bucket.** cowork creates no bucket, sets no bucket policy and checks none: the
  operator provides a private bucket of its own and an access key whose policy reaches that
  bucket only
  ([ADR 0058](../adr/0058-postgresql-and-object-storage-are-external-the-chart-takes-references-with-configurable-keys.md)
  D5; "The bucket's own controls" below).
- **Without storage.** Uploads answer `501 uploads_disabled`, lists and metadata still answer,
  and a download answers `404` saying the bytes are not reachable.
- **On the way.** The backend writes no file: it buffers an upload in memory (H-12) and
  streams a download. The frontend's nginx sees neither: the Ingress routes `/api/` to the
  backend ([ADR 0001](../adr/0001-two-containers-a-go-backend-and-an-nginx-frontend-installed-by-one-helm-chart.md)
  D3). The Ingress controller may write: a controller that buffers request bodies writes one
  larger than its memory buffer to a temporary file in its own pod for the duration of the request
  — ingress-nginx buffers request bodies (`proxy_request_buffering on`) and no response
  (`proxy_buffering off`) by default, its generated configuration of v1.15.1 shows on 2026-10-04,
  and nginx writes such a file by its documentation; the Ingress stand-in of local runs logged
  exactly that for a 10 MiB upload. That pod is the installation's. Not verified here: that a
  controller removes the file when the request ends, which is nginx's documented behaviour.

## Delivery makes the browser treat the bytes as data

Every download (`…/attachments/{attachment}/content`) carries
([`api/attachments.go`](../../backend/internal/api/attachments.go) `DownloadAttachment`;
ADR 0016 D5):

- `Content-Type`: the stored type, never the client's;
- `X-Content-Type-Options: nosniff`: the browser does not second-guess the type;
- `Content-Security-Policy: sandbox`: even a response the browser renders runs in an opaque
  origin, without scripts, forms or plugins;
- `Content-Disposition`: `inline` for the four raster types, `attachment` for everything else,
  so a PDF, a text file or an SVG is saved, never rendered inside cowork's origin. The file
  name is a parameter formatted by Go's `mime.FormatMediaType`: plain or quoted for an ASCII
  name, RFC 2231 encoded (`filename*=utf-8''…`) otherwise;
- `ETag`: the quoted hex SHA-256; a matching `If-None-Match` answers `304` without reading the
  bucket;
- `Cache-Control: no-store`, like every API response.

`TestAttachmentRoundTrip` asserts the type, `nosniff`, `sandbox`, the disposition and the
`ETag`. **An image in a ticket's text** (ADR 0016 D7): the server renders the body, a comment, a
question's options and its answer, and shows an image only when it names a raster attachment of the
same ticket — from that attachment's own path, so the browser asks nothing of another origin; an SVG,
another ticket's attachment and an address elsewhere become links, which load nothing until a person
follows them ([rendered-markdown.md](rendered-markdown.md#images)). Each time a reader's page shows
such an image, it is a recorded download, as a preview's is.

**The preview in the UI.** The ticket's page shows a raster attachment — the four types delivered
inline, never an SVG — as an `<img>` of the attachment's own URL
([`file-preview.ts`](../../frontend/src/app/features/ticket/file-preview.ts)). The image is
decoded in cowork's origin, which is what ADR 0016 D5 admits for raster types and nothing else:
an image runs no script, and a response the browser treats as a document is sandboxed. The shell's
policy admits images of its own origin and the `data:` and `blob:` URLs the page makes
(`img-src 'self' data: blob:`,
[trust-boundaries.md](trust-boundaries.md#the-shells-content-security-policy)); the request carries
the session cookie, as a download does, and is read under the same check. What the preview adds is
the decoder: a raster file crafted against a flaw in the browser's image decoder reaches it when the
page is opened, not only when a person chooses to open the file — the same exposure as an image in
any web page, and the reason the type is the server's, sniffed, never the client's.

## Limits before anything is stored

- **Size.** The per-file maximum, `COWORK_ATTACHMENT_MAX_BYTES` (10 MiB by default), is
  enforced up to four times: by the Ingress controller's body limit, which the installation sets
  above the backend's — the chart documents it and prints the figure, and a controller's own
  default may be below it, as ingress-nginx's 1 MiB is; by a declared `Content-Length` above the maximum plus 64 KiB of multipart
  overhead; by reading the body through a limit of the same size; and by reading the file
  part up to the maximum and one byte more. Each answers `413` before a row or an object
  exists ([`api/validate.go`](../../backend/internal/api/validate.go) `limitBody`,
  [`api/attachments.go`](../../backend/internal/api/attachments.go) `readUpload`). `0`
  switches the maximum off in the backend, and the chart's notes then ask the controller for no
  limit either: an upload is then read whole, whatever its size (H-12).
- **Count.** The per-ticket count, `COWORK_ATTACHMENT_MAX_PER_TICKET` (100 by default, `0` for
  none), is checked in the upload's transaction, under the ticket's attachment lock, before
  the object is put: `409 attachment_limit`. Simultaneous uploads to one ticket wait for each
  other there, so they cannot pass the count together
  ([`store/jobs.go`](../../backend/internal/store/jobs.go) `LockAttachments`;
  `TestSimultaneousUploadsKeepTheCount`).
- **Shape.** One file per upload; the multipart body takes `file` and an optional
  `comment_id`, and nothing else.
- **Order.** The act is authorised on the tenant role, the scope and, for an agent, the
  `upload` capability; the upload waits for a memory slot (H-12); the file is read, its type
  judged and its name sanitised; an agent's upload must carry an `Idempotency-Key`. Then, in
  one transaction, the ticket is read through its predicate, the role in its project is
  checked, a `comment_id` must name a comment of that ticket written by the caller's person,
  the count is checked, the row and its `uploaded` act are written, the object is put, and the
  transaction commits. When the row does not commit, or a concurrent request with the same key
  won, the object is deleted again ([`api/attachments.go`](../../backend/internal/api/attachments.go)
  `UploadAttachment`). A failed deletion leaves an object no row names; nothing sweeps the
  bucket for such objects — the consistency check of
  [ADR 0059](../adr/0059-backups-belong-to-the-operators-cowork-provides-the-export-and-makes-a-restores-inconsistency-visible.md)
  D4 is not built.

## The file name is sanitised, not trusted

`SanitizeFileName` ([`domain/attachment.go`](../../backend/internal/domain/attachment.go))
keeps the last path component after `/` or `\`, normalises to NFC, removes control characters
(Unicode `Cc`), the bidirectional controls (Unicode `Bidi_Control`, the right-to-left override
among them), double quotes and invalid UTF-8, trims white space and cuts the name to 255
bytes at a character boundary; an empty name, `.` or `..` becomes `attachment`
(`TestSanitizeFileName`). `FileNameFor` then holds the ending to the detected type: a name
that ends in none of the type's extensions — `.png`; `.jpg` or `.jpeg`; `.gif`; `.webp`;
`.pdf`; `.svg`; for text `.txt`, `.md`, `.markdown`, `.patch`, `.diff` or `.log` — gets the
first one appended, so `run.bat` detected as text is stored as `run.bat.txt`
(`TestFileNameFor`). The name is metadata only: it never enters the object key, so it cannot
address another object.

## What is recorded

- `uploaded`, on the ticket: the file name, size, type and SHA-256, the person, the token — its id
  and name — and the agent mark; the file shows the token and the mark as well
  ([tokens.md](tokens.md#what-is-recorded)).
- `downloaded`: every download that returns bytes, with the file name; a `304` is not one
  ([ADR 0026](../adr/0026-one-append-only-audit-table-written-by-the-request-layer.md) D5).
  The UI's preview of a raster image is such a download each time a page loads it.
  The act is written before the bytes are sent, and a download whose act cannot be written
  fails.
- A row whose object the bucket lacks — a restore that brought the database back without its
  bytes — answers `404` with a detail saying the bytes are missing from storage, never a bare
  `404` (ADR 0059 D4).
- The Markdown export of a ticket lists its attachments' names.

## What this does not cover

<a id="h-8"></a>
### H-8 — Nothing scans uploads for malware

Live today, by decision (ADR 0016 D8). What the type check guarantees is narrow: nothing
uploaded runs on the server, and nothing but a raster image is shown inside cowork's origin.
A name ends in an extension of the detected type and carries no bidirectional control, so it
does not offer an executable ending to the recipient's system; what the bytes are is not
checked any further. A malicious PDF reaches whoever downloads it, as it would by e-mail,
with an exploit for the reader that opens it. A text file can hold a valid batch or shell
script that someone saves and renames. An SVG that carries a script is saved as `.svg`, and
a browser that opens the saved file runs the script in the file's own origin, outside
cowork's. Invisible format characters other than the bidirectional controls, such as a
zero-width space, survive the sanitiser, so two names can look alike. What stands between
such a file and harm is the recipient's care and their machine's protection; ADR 0016 D8
names a scanning hook as the amendment.

<a id="h-9"></a>
### H-9 — File names are metadata other members read

Live today. The sanitiser removes what could break a header or a path, not what a name says.
Every reader of a ticket sees its attachments' names, sizes and SHA-256 sums in the list and
the metadata, the export prints the names, and the `uploaded` and `downloaded` acts carry the
name into the audit record, which keeps it for good (ADR 0026 D7). A name such as
`acme-breach-evidence.png` tells something to a reader who never downloads the file, and the
SHA-256 lets a reader confirm, without downloading, that a file is one they already hold.
ADR 0016 accepts this among its residual risks.

<a id="h-10"></a>
### H-10 — No per-tenant quota is enforced

Live today. ADR 0016 D6's per-tenant quota is neither enforced nor reported. What bounds a
tenant's storage is the per-file maximum times the per-ticket count times the number of
tickets, which any member with `write` scope can grow — at the defaults about a gibibyte per
ticket. Mitigation: a quota on the bucket in the storage itself; the tenant's audit
view shows the uploads by token, and revoking the token stops a runaway client.

<a id="h-11"></a>
### H-11 — A plain http:// storage endpoint is accepted

Live wherever an installation configures one; the development and test server is reached
that way. `COWORK_S3_ENDPOINT` may be `http://` or `https://`
([`config.go`](../../backend/internal/config/config.go)), and with `http://` the client speaks
plain HTTP ([`storage.go`](../../backend/internal/storage/storage.go) `New`): every upload's and
every download's bytes cross the network between the backend and the storage unencrypted,
with the access key id in each request's signature. The secret key does not travel; the
bytes do. The chart passes `storage.endpoint` as given and does not warn. Mitigation: an
`https://` endpoint — with `storage.tls.caConfigMap` for a private authority — or a mesh that
encrypts the hop.

<a id="h-12"></a>
### H-12 — A burst of uploads holds memory up to the budget

Live today. The backend has no writable disk, so an upload is buffered whole in memory, up to
the per-file maximum and one byte more. The uploads in flight share a budget of 64 MiB: the
number buffered at once is 64 MiB divided by the maximum, at least one — six at the 10 MiB
default ([`api/attachments.go`](../../backend/internal/api/attachments.go) `uploadBudget`,
`uploadSlots`). A slot is taken once the act is authorised and before the body is read —
nothing reads an upload's body earlier (`TestAnUploadIsRefusedBeforeItsBodyIsRead`) — and the
request timeout bounds the read, so a body that trickles in gives its slot back at the
deadline (`TestATricklingUploadEndsAtTheTimeout`); further uploads wait for a free slot until
their request's time runs out, answered `504`. A
burst by members of any tenant therefore holds up to about 60 MiB of file bytes at the
defaults — six times the maximum — against the chart's 256 MiB memory limit, plus what the
buffers' growth and the storage client add, which is not measured. With a maximum of 64 MiB
or more, one upload at a time holds up to the maximum, so a maximum near the container's
memory limit lets a single upload exhaust it. With the maximum switched off (`0`), one upload
at a time is read whole, and nothing but the request timeout bounds it — nothing at all when
`COWORK_REQUEST_TIMEOUT` is `0` as well: a single upload by any member with `write` scope can
exhaust the container's memory. The budget is a constant.
Mitigation: keep `COWORK_ATTACHMENT_MAX_BYTES` set and well below
`backend.resources.limits.memory`, and raise the limit along with the maximum.

<a id="h-13"></a>
### H-13 — An uploaded file cannot be taken back

Live today. No route removes or hides an attachment, and the runtime role may only insert and
read attachment rows. Withdrawing the comment a file was attached to hides the comment's text;
the file stays listed, downloadable and named in the export for every reader of the ticket. A
screenshot uploaded by mistake with a secret in it stays readable until someone with the
administrative database credential and access to the bucket removes both — outside the API,
with the `uploaded` act left in the record. Inside the API, a tenant administrator can narrow who
reads the file by setting the ticket confidential ([tenancy.md](tenancy.md) "The confidential
flag"), or take it back only with the whole ticket: deleting the ticket hides its files at once,
and purging it removes their rows and then their objects
([ADR 0024](../adr/0024-deletion-tickets-are-soft-deleted-and-purged-projects-archived-people-deactivated-tenants-deleted-explicitly.md)
D2, [tenancy.md](tenancy.md#a-deleted-ticket-answers-like-a-missing-one)). The objects go after the
purge committed, so that a rollback leaves no row naming missing bytes; an object whose removal
fails then — or that no configured storage could remove — stays in the bucket with no row naming
it, and the log names its key (`an attachment object of a purged ticket could not be removed`).
Nothing sweeps such objects.

### The bucket's own controls

Whether the bucket is private — no anonymous read, no public policy — and how far the access
key reaches, encryption at rest, versioning (a versioned bucket keeps the objects cowork
deletes after a failed upload), access logs and backups are the storage's and the operator's
(ADR 0058 D5, ADR 0059); cowork verifies none of them. The bucket holds a confidential ticket's attachments like any other:
[tenancy.md](tenancy.md) H-2 applies to them.
