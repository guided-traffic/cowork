# What an import lets in and an export lets out

Who may bring tickets into a project from files and take them out as files, what an import reads,
keeps and creates, what an export carries out of the installation and what it leaves behind, how
both are bounded, and what the command line does with an export on a person's machine — and what
that leaves open, as built on 2026-10-07
([ADR 0051](../adr/0051-import-is-a-server-side-two-phase-atomic-job-export-is-its-mirror.md),
[ADR 0063](../adr/0063-the-importer-takes-whatever-the-user-hands-it-open-and-archived-tickets-alike.md),
[ADR 0065](../adr/0065-a-confidential-flag-replaces-the-file-name-embargo-set-automatically-lifted-only-by-a-person.md)
D5, D7). Who may read a ticket, confidential ones included, is [tenancy.md](tenancy.md); what a
token or an agent may do is [tokens.md](tokens.md); how the import works, for somebody changing it,
is [docs/developer/import-and-export.md](../developer/import-and-export.md).

## Who may import, and who may export

| Act | Route | Needs | An agent |
|---|---|---|---|
| a dry run | `POST …/projects/{project}/imports` | the role `admin` in the tenant and, through a token, the `admin` scope | refused, `403 agent_forbidden` — a flagged token and a request with `X-Cowork-Agent` alike (the hard-off rule "administration") |
| its execution | `POST …/imports/{import}/execution` | the same | refused the same way |
| reading a job | `GET …/imports/{import}` | the role `admin`, the `read` scope | refused the same way: a report holds what the upload's files say, of those the import left out too |
| the project export | `GET …/projects/{project}/export` | `read` on the project, which the caller must see | admitted, reading what its person reads |
| the tenant export | `GET /api/v1/tenants/{tenant}/export` | `read` in the tenant | admitted the same way |

([`imports.go`](../../backend/internal/api/imports.go), [`exports.go`](../../backend/internal/api/exports.go).)
A token restricted to a project exports its project; the tenant export is outside that project
and answers it `404`, as every tenant route outside the project does (`tenantWideForProjectTokens`
in [`tenant.go`](../../backend/internal/api/tenant.go)). A session's dry run and execution are
writes and pass the CSRF check ([csrf.md](csrf.md)); an export is a read, which the check leaves
alone ([csrf.md, H-22](csrf.md#h-22)). The browser offers the import page to a tenant's
administrators only and the tenant's export in its settings to them alone; that is what the pages
offer, not a check — the routes above are the check.

Below the handlers, the rows of `import_jobs` are the tenant's administrators' alone: the
restrictive policies of [migration 43](../../backend/internal/store/migrations/000043_import_jobs.up.sql)
hold reading, inserting and changing a job to `app_is_tenant_admin()`, so a query that forgot its
caller's role shows a member nothing; beside the administrators, the job `import-expiry` reads and
deletes dry runs and nothing else, and the purge of a ticket (`ticket-purge`) reads and changes the
report of its tenant's jobs ([below](#what-a-dry-run-keeps)).

Verified in the integration tier: a member is `403 forbidden`, an administrator's `write` token
`403 insufficient_scope`, an administrator's token with the agent header `403 agent_forbidden` at the
dry run and at the read of a job, and a member of another tenant `404`
(`TestImportADryRunAndItsExecution`); a member, a viewer and an
agent export, a member of another tenant and a member outside a restricted project get `404`
(`TestTheExportFollowsItsReader`); a member reads, changes and inserts no job and an administrator
deletes none at the database (`TestTheImportJobPoliciesAdmitTheTenantsAdministratorsOnly`). Not
tested: a flagged agent token at the import, which takes the same path as the header.

## What an import reads

The upload is held in memory and nothing of it is written to a file system: an archive's paths are
cleaned into relative, `/`-separated labels the report shows and a correction names, never places
([`upload.go`](../../backend/internal/importer/upload.go)). An entry that is no regular file — a
link, a device — is listed as skipped and never followed. Only Markdown files and an export's
`manifest.json` and `links.json` are read — every `.md` file, whatever it holds, and the dry run
keeps each ([below](#what-a-dry-run-keeps)); everything else is listed with its reason.

A frontmatter is read as YAML nodes, and a value must be one scalar: an alias, a list or a mapping
where one value belongs is an error of the file, so an alias is never expanded (read in
[`fields.go`](../../backend/internal/importer/fields.go) `scalar`, not tested). A value outside its
vocabulary is an error, never a guess ([ADR 0010](../adr/0010-the-frontmatter-vocabularies-become-ticket-columns.md)
D5). A `/context` document is an error: its read-only sections are no import format
([ADR 0044](../adr/0044-two-endpoints-markdown-is-the-canonical-ticket-context-is-the-ticket-with-what-surrounds-it.md)
D3). A text is held to the length the API holds every write of it to, as the execution would write
it: a body of more than 200,000 characters, and a question's options or its answer of more than
100,000, is an error of its file ([ADR 0051](../adr/0051-import-is-a-server-side-two-phase-atomic-job-export-is-its-mirror.md)
D7, `TestTheImportHoldsTheTextsToTheLengthsOfTheAPI`); the database holds the same lengths
([migration 44](../../backend/internal/store/migrations/000044_text_length_checks.up.sql)), and what
a reader's rendering of them costs is [rendered-markdown.md](rendered-markdown.md#what-a-rendering-reads).

## The bounds

| Bound | Value | Refusal |
|---|---|---|
| the request's body | `COWORK_MAX_IMPORT_BYTES` (50 MiB by default) plus 64 KiB of multipart framing, before the body is read where its length is declared | `413` |
| the files of the upload together | `COWORK_MAX_IMPORT_BYTES` once unpacked — a file the import does not read by the size its archive declares, since `tar` reads through those bytes and a `zip`'s are never decompressed; a `tar` entry that is no regular file, a link or a device, by the size it declares; a directory, a `tar`'s global header and a `zip` entry that is no regular file count nothing | `413` |
| the files of an upload | 10,000 (`importer.MaxFiles`) | `413` |
| a path | 1,024 bytes, and no path twice | `400` at `/file` |
| imports at once | one per replica, a dry run or an execution; another waits for it within its own request timeout (`importSlot`) | `504` past the timeout |
| time | `COWORK_REQUEST_TIMEOUT`, reading the body included; the transaction rolls back past it | `504` |

A file the import reads is decompressed only up to the bound that is left
(`io.LimitReader`), so an archive whose files unpack to more is refused, not unpacked; a `zip`
is read whole, and so bounded by the body's limit, and the files it skips are never decompressed.
What a `tar` holds besides its files — the headers of its entries, its directories — is decompressed
and counted by no bound but the time ([H-105](#h-105)).
`0` switches the byte bound off, the body's included — one upload may then hold unbounded memory.
`TestReadUploadRefusesWhatItCannotTake` and `TestImportBoundsAndExpiry` hold the bounds.

## What a dry run keeps

The dry run stores its report and every file it read, compressed, in the job's `source` column, so
that the execution reads exactly what the person reviewed and no second upload can slip in between.
No route answers `source`: the report is what `GET …/imports/{import}` shows. Twenty-four hours
after the dry run a read and an execution answer `404`, and the job `import-expiry` deletes the row
with its files within the hour; the execution drops the files at once. The report stays with an
executed job for good ([H-73](#h-73)), except that the purge of a ticket the import created takes
that ticket's file out of it, so the text the purge removes from the ticket and the audit record
does not stay readable there ([ADR 0024](../adr/0024-deletion-tickets-are-soft-deleted-and-purged-projects-archived-people-deactivated-tenants-deleted-explicitly.md)
D2; `TestThePurgeTakesAnImportedTicketOutOfItsReport`).

## What an import creates

- **Nothing over what is there.** A number the project holds — a deleted ticket's included — or a
  purged ticket held is a conflict, and the execution refuses while one remains
  ([ADR 0064](../adr/0064-one-direction-import-and-export-no-synchronisation.md) D3); so is a file
  with an error. An execution analyses the files again under the project's rank lock, so a ticket
  filed after the dry run cannot be overwritten either.
- **Attribution.** The person who executes is the reporter of every ticket, and every ticket,
  question and link has an act of theirs that names the job; the dates and notes a file carries
  are the file's claims, recorded as the import's.
- **An assignee only by identity.** `local:<username>`, or `oidc:<issuer>#<subject>` of this
  installation's issuer, of a member who can see the project — never by a name, and an identity of
  another issuer never ([ADR 0044](../adr/0044-two-endpoints-markdown-is-the-canonical-ticket-context-is-the-ticket-with-what-surrounds-it.md)
  D1). Assigning a confidential ticket admits its assignee to it (ADR 0065 D9), so an upload
  admits whom its files name; the report shows each assignee as the member it resolved to, and a
  correction removes one before the execution.
- **The confidential flag by the rule of the source** (ADR 0065 D7, [`columns.go`](../../backend/internal/importer/columns.go)
  `confidential`): a `publication-accepted:` date decides first and leaves it unset; otherwise an
  export's `confidential: true`, the `local_` prefix, or a `live` or `boundary` class without a
  `shipped:` line sets it. The report
  gives the rule that decided for every file it applies to, and the act `confidential_set` carries
  it. The upload's own words decide: a file that claims an accepted publication is imported
  unflagged, and the report says so.
- **Links within the project.** A reference resolves to a ticket the import creates or to one of
  the project's tickets the caller sees: `T<n>`, a key of a file in the upload, or a key of the
  project the upload's export comes from, read as the number in the target project. A repository
  file's `T<n>` in its text is rewritten to the full key of the ticket it became. Any other key
  resolves to nothing, is reported, and goes into the body under `## Related`. A `blocks` link that
  would close a cycle is omitted.
- **Text as text.** A body, a question and an answer are stored as written and rendered through
  the sanitiser like any other ([rendered-markdown.md](rendered-markdown.md)). The paths and titles
  in the report are the upload's, shown as text — the import page shows every text of a report by
  interpolation, never as markup; unlike an attachment's name
  ([attachments.md](attachments.md#the-file-name-is-sanitised-not-trusted)), nothing strips a
  bidirectional control from them.

## What an export lets out

The archive holds, for the projects its reader sees:

- every ticket they may read, done and dropped ones included, deleted ones not, each as its
  `/markdown` document: the frontmatter with the threat, the notes and reasons, the assignee as
  `Name <identity>` — a username, or an issuer and a subject together
  ([identity-provider.md](identity-provider.md)) —, `confidential: true` on a confidential one, the
  names of its attachments, the body and the questions with their answers;
- `manifest.json`: the tenant, the projects with their names, the counts, the time, and the
  exporter as `Name <identity>`;
- `links.json`: every link whose two ends the reader sees, once;
- `attachments.json`: each attachment's ticket, name, type, size and the path of its bytes, which
  only a reader of the ticket fetches — never the bytes.

It leaves out the confidential tickets its reader may not read, counting them per project
([H-74](#h-74)); a restricted project its reader cannot see, without a count; and, for everybody,
the comments, the time entries, the activity and the audit record. An administrator's export
holds every confidential ticket of the tenant. Each export is the act `exported` on the project or
the tenant, with the format and the counts, the token and the agent mark as every act carries them,
and is never published to a stream ([ADR 0059](../adr/0059-backups-belong-to-the-operators-cowork-provides-the-export-and-makes-a-restores-inconsistency-visible.md)
D3). The answer is `application/gzip` with `Content-Disposition: attachment` and
`Cache-Control: no-store`.

## `cowork-mcp export` on a person's machine

The subcommand ([`mcpcli/export.go`](../../backend/internal/mcpcli/export.go)) refuses a target
that is a file or a directory that is not empty before it asks the server. It writes only the names
an export of the project holds — the three manifests and `<tenant>/<PROJECT>-<n>.md` of that tenant
and project, each a valid `/`-separated path (`exportEntry`) —, so a name with another separator, a
step, a volume or an absolute path ends the unpacking with an error, as does an entry that is no
regular file. It writes through a root opened at the target (`os.OpenRoot`): no name and no link
inside the target reaches out of it — a directory link planted in the target that leads elsewhere
is refused (tested with a symbolic link; not run on Windows, whose junctions the test does not
make). It creates every file with `O_EXCL`, so nothing that exists is overwritten
(`TestUnpackStaysInsideAndNeverOverwrites`, `TestTheExportWritesOnlyItsOwnNamesInsideItsTarget`).
On POSIX systems the directories are `0700` and the files `0600`, since an archive may hold
confidential tickets; Windows applies no such mode — Go sets only the read-only attribute there —,
and the files take the access list of the directory they are written to ([H-77](#h-77)). Its
requests carry the agent mark `cowork-mcp/unknown/export`, so the export's act names the binary.

## What this does not cover

<a id="h-72"></a>
### H-72 — A dry run keeps the files it read for a day, in the database and its backups

Live for every dry run. Until its execution, or for a day and up to the hour after it until the
job `import-expiry` runs — up to twenty-five hours —, the job's `source` holds the full text of every
`.md` file the upload carried — a file the person then excludes, an embargoed finding's among them.
No route answers it and the policies hold it to the tenant's administrators, but whoever reads the
database past row-level security, or holds a backup taken meanwhile, reads it, as they read a
confidential ticket ([tenancy.md, H-2](tenancy.md#h-2)). A backup keeps it for its own retention,
and an archive of the database's write-ahead log keeps the insert for its own. Mitigation: execute the dry run, or let it expire, before
a backup that must not hold it; upload only the files to be imported.

<a id="h-73"></a>
### H-73 — An import job's report keeps what the upload said, the files it left out included

Live for every import. An executed job's report is kept for good, and no route deletes it: it names
every file of the upload by its path, and for each file the import read its title, its type and
state, its columns — the threat among them —, its note or reason, its questions' texts, its block's
reason, its assignee as the file wrote it, and its warnings. The purge of a ticket the import
created takes that ticket's file out of the report of the job that created it, and of no other; a
file that was skipped or excluded stays as the report had it — its path, and what was read of it, an
embargoed finding's title among them, which no ticket of cowork holds —, and so does a created ticket's text
as the file had it after a person changed the ticket, as the audit record keeps a replaced text
(ADR 0026 D7). The tenant's administrators read it, never an agent, and they read every confidential
ticket anyway. Mitigation: upload only the files to be imported; a report holding what must go is
changed by hand in the database.

<a id="h-74"></a>
### H-74 — An export tells its reader how many confidential tickets they cannot read

Live today, by decision (ADR 0065 D5). The manifest counts, per project the reader sees, the
confidential tickets left out. The other surfaces behave as if such a ticket did not exist — a
dashboard tile counts only what its reader sees —, but for the signals of [tenancy.md](tenancy.md#h-3)
H-3, and an export does not: a member who exports a
project now and then learns how many confidential tickets it holds and when one more appears,
though nothing of what they are. A restricted project the reader cannot see is not counted.

<a id="h-75"></a>
### H-75 — An export takes what its reader may read beyond cowork's reach

Live for every export, by its nature. Once answered, an archive is outside every control cowork
has: the act records who exported what and when, and nothing after. An administrator's archive
holds every confidential ticket of its projects with its threat; every archive holds the persons'
identities — usernames, and the issuer and subject of a person of the identity provider together,
as a ticket's `/markdown` and `/context` write them too — and the exporter's. A scheduled export's token reads a
whole tenant for as long as it lives
([docs/operations/import-and-export.md](../operations/import-and-export.md)). Mitigation: give a
backup's token the `read` scope and the tenant's restriction, keep its archives as the backups they
are, encrypted and readable only by those who may read the tenant, and read the `exported` acts in
the tenant's audit view.

<a id="h-76"></a>
### H-76 — One import's memory is not measured at its bound

Live on every installation that leaves `COWORK_MAX_IMPORT_BYTES` at a size its memory limit does
not allow for. A dry run holds its upload several times over — the zip's bytes or the files read,
their compressed copy for the job, the parsed text, the report — and an execution the files, the
text and the plan. One import at a time per replica bounds it to one such set, but how large the
set is at 50 MiB was not measured, and the chart's default memory limit is 256 MiB. A tenant's
administrator can reach that bound, and a replica that runs out of memory restarts for every
tenant it serves; and while one import runs, every other tenant's import on that replica waits,
each up to its request timeout, so one administrator who keeps uploading keeps the others waiting.
Mitigation: keep the backend's memory limit well above the variable, lower the
variable to what the installation's imports need, split an import by directory.

<a id="h-77"></a>
### H-77 — An export is as private as its target directory

Live where another local account may write or read the target directory or its parent. The root
holds every file the unpacking writes inside the directory the target names when the answer
arrives — after the check that it is empty, or absent, which ran before the request: a process
that can write the target's parent can put a link to another directory in its place in between, and
the files are written there, new files only, since `O_EXCL` refuses one that exists. A file planted
in the target under a name the export holds ends the unpacking with an error. On Windows the files
and directories take the target directory's access list, not owner-only modes: an export written
where other accounts may read reaches them, confidential tickets included. Mitigation: export into
a directory only the person can write and read — on Windows, one under their profile.

<a id="h-105"></a>
### H-105 — What a tar holds besides its files is decompressed by no bound but the time

Live for every import of a `tar.gz`. The byte bound counts what an archive's entries hold, by what
is read or what they declare; the headers of the entries, and the entries that hold nothing — a
directory, a global header —, count toward neither the bytes nor the number of files
([`upload.go`](../../backend/internal/importer/upload.go) `tarEntry`). A small compressed upload can
so make the replica decompress and walk a great many headers, bounded by the request timeout alone,
while it holds the replica's one import slot ([H-76](#h-76)) — and by nothing with
`COWORK_REQUEST_TIMEOUT` at `0`. It takes a tenant's administrator. Mitigation: keep the request
timeout set.

<a id="h-106"></a>
### H-106 — An import can move a project's numbering to the end of its range

Live for every import. An imported ticket keeps the number its file names, and the project's counter
moves to the highest number an import wrote ([`queries/write/imports.sql`](../../backend/internal/store/queries/write/imports.sql)
`AdvanceTicketCounter`), so a file numbered `2147483647`, the largest the column holds, leaves the
project no number for its next ticket: every filing in it fails from then on, and no route lowers the
counter. It takes a tenant's administrator, and the report shows each file's number, and the highest
of them, before the execution. Mitigation: read the numbers in the dry run's report; a counter moved
by mistake is set back by hand in the database.

<a id="h-107"></a>
### H-107 — The command line holds an export whole in memory and bounds none of it

Live where `cowork-mcp export` meets an archive larger than the person's machine should hold. The
subcommand reads the answer whole, and each entry of the archive whole before it writes it
([`mcpcli/export.go`](../../backend/internal/mcpcli/export.go) `unpack`, `writeNew`): no bound holds
the answer's size, what it unpacks to, or the number of its entries. An export of a large project, or
an answer of an installation that is not what it claims, can so take the client's memory. Mitigation:
export from an installation the person trusts; the server's own bounds are the export's, which a
compromised installation does not keep.
