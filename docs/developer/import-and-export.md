# Import and export

How tickets come into a project from files and leave it as files: the import job in two phases —
a dry run whose report a person corrects, then its execution in one transaction — and the export,
its mirror, an archive of grammar v1 with three manifests. The decisions are [ADR 0051] (the job,
the phases, the export, the round trip, who, the sizes — as amended 2026-10-09: a writer's act, an
agent's included, and nothing in it refuses), [ADR 0063] (what the importer reads and how it maps
it), [ADR 0064] D3 (a repeated import is a conflict, left out), [ADR 0007] D4 (a purged ticket's
number given back), [ADR 0065] D5 and D7 (what the export leaves out, the confidential flag on
import) and [ADR 0044] D1 and D3 (the grammar it reads back); what an administrator does with it is [docs/operations/import-and-export.md](../operations/import-and-export.md),
what it lets in and out [docs/security/import-and-export.md](../security/import-and-export.md);
the browser's import page and export buttons are [frontend.md](frontend.md#the-import-and-the-export).
Read against the tree on 2026-10-06, the browser's part on 2026-10-07, the import as the agent's
tool on 2026-10-09.

## Where it lives

| Part | Files |
|---|---|
| The routes | [`backend/api/imports.yaml`](../../backend/api/imports.yaml); the report, the corrections and the manifests are `Import*` and `Export*` in [`components/schemas.yaml`](../../backend/api/components/schemas.yaml) |
| The reading and the analysis | [`internal/importer`](../../backend/internal/importer/), a package of its own: pure, no database — what it needs of the project comes in as a `Target` |
| The handlers | [`api/imports.go`](../../backend/internal/api/imports.go) (the dry run, the read, the execution's checks), [`api/importwrite.go`](../../backend/internal/api/importwrite.go) (the execution's writes), [`api/exports.go`](../../backend/internal/api/exports.go) (the project and the team export) |
| The data | [migration 43](../../backend/internal/store/migrations/000043_import_jobs.up.sql) (`import_jobs`, `tickets.imported_from_file` and `imported_from_job`, the action `imported`), [migration 45](../../backend/internal/store/migrations/000045_import_jobs_of_their_writer.up.sql) (the policies that admit a job's maker); [`queries/read/imports.sql`](../../backend/internal/store/queries/read/imports.sql), [`queries/write/imports.sql`](../../backend/internal/store/queries/write/imports.sql), the export's reads in [`queries/read/export.sql`](../../backend/internal/store/queries/read/export.sql); the expiry job in [`store/imports.go`](../../backend/internal/store/imports.go) |
| The command line | `cowork-mcp export` in [`mcpcli/export.go`](../../backend/internal/mcpcli/export.go), `cowork-mcp import` in [`mcpcli/import.go`](../../backend/internal/mcpcli/import.go) |
| The browser | the import page, [`features/project/project-import.ts`](../../frontend/src/app/features/project/project-import.ts) with [`import-model.ts`](../../frontend/src/app/features/project/import-model.ts); the requests and the archive in [`core/imports.service.ts`](../../frontend/src/app/core/imports.service.ts) and [`core/export-archive.ts`](../../frontend/src/app/core/export-archive.ts) ([frontend.md](frontend.md#the-import-and-the-export)) |

## The dry run

`CreateImport` ([`imports.go`](../../backend/internal/api/imports.go)), `POST …/projects/{project}/imports`:

1. **Who.** A writer of the project, as creating a ticket needs ([ADR 0051] D6): `work` — the
   role `member`, a token's `write` scope, no capability, an agent too — against the team's role
   before the upload is read, and against the project's in the transaction (`importWriter`:
   `visibleProject`, `projectRole`, which a restricted project's list lowers).
2. **One at a time.** `slot` over the server's import slots (`imports`) holds the replica to one import, a dry run or an execution:
   the upload is held in memory, unpacked, and kept once more compressed. A second one waits for
   the slot within its request's deadline.
3. **The upload.** `limitBody` in [`validate.go`](../../backend/internal/api/validate.go) bounds
   the body of `createImport` by `COWORK_MAX_IMPORT_BYTES` plus 64 KiB of multipart overhead, as
   it bounds an attachment's by its own maximum. `importer.ReadUpload` reads every part named
   `file`: one starting with gzip's bytes is a tar.gz, read as it streams; one starting with a zip
   header is a zip, read whole; anything else is one file named by the part's file name — a base
   name, which Go's multipart reader makes of it. It keeps the bytes of the Markdown files and of
   an export's `manifest.json` and `links.json`, and lists every other file with the reason it is
   not read (`attachments.json`, anything not `.md`, an entry that is no regular file). Bounds, each
   an `UploadError`: the files together, the skipped ones by the size their archive declares, at
   most `COWORK_MAX_IMPORT_BYTES` (`413`); at most `importer.MaxFiles`, 10 000 (`413`) — a `zip` counted before
   `zip.NewReader` parses it, by the central-directory signatures `PK\x01\x02` its bytes hold, since the
   parser reads every entry of the directory, past the count its end declares too; a path at
   most 1024 bytes, no path twice — a correction names a file by its path — and only parts named
   `file` (`400` at `/file`). `uploadProblem` maps them, and a body cut off by its limit or its
   deadline to `413` and `504` as elsewhere. A path is cleaned to a relative, `/`-separated label;
   nothing is written anywhere by it.
4. **The stored form.** `importer.Pack` writes the files as a gzip-compressed tar, a skipped file
   with no bytes and its reason in the PAX record `COWORK.skip`; `Unpack` gives them back in their
   order.
5. **The analysis**, in a `Mutate`: `importProject` reads the project through the predicate and
   refuses an archived one (`409 project_archived`); `importTarget` asks what `Upload.Needs` lists
   and the analysis reads; `importer.Analyze` makes the report. The job is inserted with the report
   and the packed files, `expires_at` twenty-four hours after the handler's clock
   (`store.ImportValidity`), and the act `created` on the `import_job` counts the outcomes. The
   answer is `201` with the job, read back in the same transaction, and `Location`.

**What the analysis reads of the project** (`importTarget`, in the transaction that runs it):

| Query | Reads | Why |
|---|---|---|
| `ImportNumbersTaken` | the numbers among the upload's a ticket of the project holds, a deleted one included | a conflict ([ADR 0064] D3); exempt from the predicate and the deletion filter — a number's existence in the project, as the unique key holds it |
| `ImportPurgedKeys` | the keys among the upload's whose ticket was purged, from the purge's act | an import gives the number back, with a warning that what named the key names the new ticket ([ADR 0007] D4 as amended 2026-10-09) |
| `ImportReferencedTickets` | the project's live tickets the references name by number, through the predicate | a reference to a ticket the upload does not bring ([ADR 0051] D9) |
| `ImportPersons` | the members a `local:<username>` or an `oidc:<issuer>#<subject>` of the configured issuer names; then `CanSeeProject` for each | an assignee by identity, never by name ([ADR 0044] D1) |
| `GetMember`, `CanSeeProject` | a person a correction names | a corrected assignee: a member who can see the project, else `400` at `/corrections/<i>/assignee` |

Beside what it reads, `importTarget` hands the analysis `Agent`, the person of an agent's request,
and, for an execution, `Named`: the members the dry run's report assigned, by the file's path,
which `lockedDryRun` reads from the job's `report` (`LockImportJob`).

## The execution

`ExecuteImport`, `POST …/imports/{import}/execution`, the same who and slot; in one `Mutate`:

1. `importProject`; `lockedDryRun` reads the job `FOR UPDATE` (`LockImportJob`): none — another
   person's, which the policies hide —, or a dry run past `expires_at`, is `404`; an executed one
   `409 import_executed` — a second execution waits for the first's lock and then finds it executed
   ([ADR 0051] D3).
2. `Upload.Check` holds the corrections to the rules of `ImportCorrection`, each refusal `400` at
   its pointer, `/corrections/<i>/path`, `…/exclude`, `…/block`, `…/block/kind`, `…/block/from`.
3. `lockRank` takes the project's counter row: a filing waits, and no number the analysis checks
   can be taken meanwhile.
4. The analysis again, with the corrections, against the project as it stands now and with the
   dry run's assignees. A file that has an error or a conflict — a ticket filed after the dry run
   with one of its numbers included — is left out of the plan, its `reason` saying why; nothing
   refuses the execution ([ADR 0051] D2 as amended 2026-10-09).
5. `execute` ([`importwrite.go`](../../backend/internal/api/importwrite.go)) writes the plan:
   - the tickets in the plan's order — a parent, and the ticket a block waits on, before the ticket
     that names it — each `InsertImportedTicket` with its number, the source's state, dates,
     stages, horizon (set by the importer when it is not `later`), block, assignee, confidential
     flag, `done_from` `in-progress` for a done one, and `imported_from_file` and
     `imported_from_job`; an open one takes the next place at the bottom of the project's rank after
     `rankUnranked`, in the plan's order; `refreshScore`;
   - the acts of each ticket, all `Quiet` (below): `created` — type, title, severity, security,
     effort, `urgency`, state, `import_job`, `file` —, `confidential_set` with the reason of the
     rule that set the flag, and for a done or a dropped ticket `transitioned` with the note or the
     reason, which the export reads back as `shipped` and `dropped-reason`
     (`TransitionNote`);
   - the questions, `InsertImportedQuestion`, asked by the importer and, when answered or
     withdrawn, answered or withdrawn by the importer at the execution's time; each the act `asked`
     naming the job;
   - the parents' derived stages, `refreshProgress` once per parent;
   - the links: a `blocks` link takes the team's blocks lock once and is walked first
     (`BlocksPathExists`); one that would close a cycle through the project's tickets — which the
     analysis does not walk — is omitted and the file's report says so; `relates-to` is stored
     with the smaller id first; `linked` on both tickets, `Quiet` on a ticket the import creates
     and published on one of the project's;
   - `AdvanceTicketCounter` to the highest number when it is above the counter;
   - `Report.Executed`, `FinishImportJob` — the executed report replaces the dry run's, the files
     go —, and the act `imported` on the job, which carries `ProjectRank`.
6. The answer is `200` with the executed job.

**Publication.** `store.Event.Quiet` writes an act and never publishes it
([`tx.go`](../../backend/internal/store/tx.go) `writeEvents`): the acts on the tickets an import
creates are many, and the job's one act announces them as `project.changed` with the kind
`imported` — the browser reloads the project's lists once, and the dashboard and the open decisions
with them ([events.md](events.md), [frontend.md](frontend.md#how-a-change-reaches-the-screen)). No act of an
import tells anybody's inbox: none carries a notice.

**The read.** `GetImport`, the same writers (`importWriter`), reads the job through `GetImportJob`
with the handler's clock: another person's job, which the policies hide, and a dry run past its day
answer `404`, the latter before the job deletes it. `importJobView` decodes the stored report into
the generated types.

**The expiry.** `DB.ExpireImportJobs` ([`store/imports.go`](../../backend/internal/store/imports.go)),
the job `import-expiry`, lock key `9`, deletes the dry runs past `expires_at` in every team with
their files and records one `expired` act on `import_jobs` per run that removed any; `runJobs` in
[`main.go`](../../backend/cmd/cowork/main.go) runs it hourly with the others. An executed job stays:
its tickets name it.

**The policies** of `import_jobs` ([migration 43](../../backend/internal/store/migrations/000043_import_jobs.up.sql),
[migration 45](../../backend/internal/store/migrations/000045_import_jobs_of_their_writer.up.sql)):
the canonical `tenant_isolation`; restrictive policies that admit reading to the job's maker
(`created_by = app_user_id()`), a team's administrator (`app_is_tenant_admin()`), the job
`import-expiry` and the purge (`ticket-purge`), inserting to the maker in their own name and the
administrator, and changing to the maker, the administrator and the purge — a report holds what
its upload's files say, an embargoed finding's among them, which no other writer reads; the expiry job's own
permissive read and delete of the dry runs with no `app.tenant_id` set; and a restrictive delete that admits
only the expiry job and only a dry run. The runtime role may update `status`, `expires_at`,
`executed_by`, `executed_at`, `report` and `source`.

**The purge.** `Writer.PurgeTicket` ([`store/deletion.go`](../../backend/internal/store/deletion.go))
takes a ticket the import created out of its job's report (`forgetImportedFile`,
`ForgetPurgedImportFile`): the report keeps every file's text for good, and would keep what the purge
removes ([ADR 0024](../adr/0024-deletion-tickets-are-soft-deleted-and-purged-projects-archived-people-deactivated-tenants-deleted-explicitly.md)
D2). The summary still counts the file, and the purge's act counts `import_report`.

## The reading of a file

`importer.Read` parses every Markdown file (`Parse`) and reads the manifests; a name decides first:
`NNN-<slug>.md` and `local_NNN-<slug>.md` are a repository's ticket files, `<PROJECT>-<n>.md` an
export's, and anything else is skipped ([ADR 0063] D5). Then the content:

- not UTF-8 → an error; a byte order mark is dropped, `CRLF` read as `LF`;
- a first line `<!-- cowork: context of` → skipped, `skipContext` its reason ([ADR 0044] D3 as
  amended 2026-10-09);
- no frontmatter → `plain`, an archived record: the number of its name, its first `# ` heading or
  its name as the title ([ADR 0063] D4);
- a frontmatter with the key `key` → `export`, grammar v1; any other → `repository`.

**The frontmatter** is YAML read as nodes ([`parse.go`](../../backend/internal/importer/parse.go),
[`fields.go`](../../backend/internal/importer/fields.go)), each key with its line. A repository's
frontmatter is written by hand, and a value such as `0.3.0 — phase 5: …` is no YAML; a frontmatter
that is none is read line by line (`linePairs`: `key: value`, `  - item` under a key without a
value, comments and blank lines) with a warning, and is an error only when a line is none of those.
A key twice is an error; an unknown key a warning, its value not read. A vocabulary value outside
its set is an error naming the key and its line, never a guess ([ADR 0010] D5); `urgency` is read
as `horizon`, and the two disagreeing is an error ([ADR 0044] D3); the title, the state, the
severity, the security class and the effort are required, a class other than `none` needs its
threat and `none` takes none ([ADR 0010] D2); `id: T<n>` and an export's key must name the number
of the file's name; dates are `YYYY-MM-DD` or a time whose UTC day is taken; a stage 0 to 100 in
steps of five.

**The body and the questions** ([`questions.go`](../../backend/internal/importer/questions.go)):
the last `## Open questions` heading outside fenced code holds the questions, up to the next
heading of its level — grammar v1 writes it last, a repository before `## Not verified` and
`## Related`, which stay in the body. A `### Q<n>: <question>` starts a question; its options run
to the last `**Recommendation:**` line before its last `**Answer:**` line, and the answer is that
line and the rest of the block: `_open_`, `_withdrawn_`, or the answer given; no answer line, or an
empty one, is open with a warning. Text before the first question joins its options, with a
warning; a section with text and no question stays in the body, with a warning; an earlier
heading of the same name stays in the body, with a warning ([ADR 0011] Residual risks). The same
number twice, a question without its text, or one above 2000 characters is an error.

## The analysis

`importer.Analyze` ([`analyze.go`](../../backend/internal/importer/analyze.go),
[`columns.go`](../../backend/internal/importer/columns.go), [`refs.go`](../../backend/internal/importer/refs.go)),
in this order:

1. **Classify**: every file of the upload a report entry in its order; a skipped file and a manifest
   with the reason — a manifest's names the projects of its export by their team, which an archive
   names as `team`, or as `tenant` alone when the release that wrote it called a team so; that
   archive is read so for good, since an archive outlives a release (`exportManifest.team`,
   `TestTheManifestNamesItsTeamByEitherName`).
2. **Numbers**: a number two files bring is an error of both; one the project holds is a conflict
   naming the key; one a purged ticket held is imported, with a warning.
3. **Columns** of every file not excluded and readable: a record without frontmatter is a done task
   of severity `low`, security `none`, effort `S`, with a warning; the type is the correction's, the
   file's, or detected (below); the state is the correction's or the file's, `blocked` only as one
   of them says, with its block; a done ticket's note is `shipped` or the import note `imported from
   archive; the source carried no verification note`, a dropped one's reason `dropped-reason` or
   `imported from archive; the source carried no reason` ([ADR 0063] D2); the assignee by identity;
   the confidential flag by the rule of the source (below); the texts the plan does not rewrite held
   to the API's lengths (`lengths`): a threat, a block's reason and a dropped ticket's reason of
   2,000 characters, a done ticket's note of 10,000.
4. **Settle**: a block of kind `ticket` whose ticket resolves to nothing, and a chain of parents or
   of waited-on tickets that loops back, are errors — repeated until no file changes, since an
   error takes a file out of what the others resolve to.
5. **References and the plan**: a reference resolves to a file the execution creates, else — when
   the upload does not bring its number — to the project's ticket of that number, else to nothing.
   `T<n>` names a repository's number; a full key names the upload's file of that key, or a number
   of the project the upload's export files come from (`sourceProject`); a key of another project
   resolves to nothing ([ADR 0051] D4, D9). Then the parent, the ticket a block waits on (the
   file's own `blocked-by`, or the one `blocks` link of `links.json` that ends at the ticket), a
   repository's `blocked-by` and `filed-from`, and `links.json`. Each link once — `relates-to` once
   whichever end names it —, never one from a ticket to itself, never a `blocks` link that closes a
   cycle among the imported tickets; each reported at both its ends. What resolves to nothing is a
   warning, and a repository's reference becomes a line under the body's `## Related`, never a
   silent drop ([ADR 0063] D3). Then the order — parents and waited-on tickets first, else by
   number — the related lines, and `T<n>` outside code rewritten to the full key of a ticket the
   import creates, in the body and the questions of a repository's and a plain file. Last, the texts
   as the execution writes them are held to the lengths the API takes (`bounded`): a body of more
   than 200,000 characters, a question of more than 2,000, a question's options or answer of more
   than 100,000, or a recommendation of more than 10,000, is an error of the file — the lines and the
   keys the import adds count ([ADR 0051] D7). Such an error is known only once the plan stands, and
   a file left out changes what the others' texts become — a blocks link to it turns into a line
   under `## Related` —, so `Analyze` runs again from the files as they were parsed (`File.clone`),
   the files found too long errors from the start (`long`), until a run finds none.
6. **Finish**: each report entry from what the analysis made of its file; a conflict's and an
   error's `reason` says the import leaves it out.

A file with an error or a conflict is never in the plan: what names it resolves to nothing, as for
any file the import does not create, and the execution writes the rest.

A ticket the plan creates as done is done by hand unless all three of its stages are full and it
has no children ([ADR 0009] D5).

**The type by content** (`detectType`; [ADR 0008] D5), first match wins, and the report says which
rule matched: a title ending in `?` is a `question`; a `live` or `boundary` finding a `bug`; a title
with a decision's word (`undecided`, `decide`, `decides`, `decision`, `whether`) a `decision`; with a
defect's (`fail`, `fails`, `failed`, `failing`, `broken`, `breaks`, `crash`, `crashes`, `wrong`,
`incorrect`, `leak`, `leaks`, `regression`, `bug`, `defect`) a `bug`; with a missing capability's
(`there is no`, `there are no`, `has no`, `have no`, `cannot`, `can not`, `is missing`, `are
missing`, `does not exist`, `do not exist`, `not yet`) a `feature`; else a `task`. Words are matched
whole. A correction overrides it.

**The assignee** (`assignee`): a correction's member, or the member a `local:<username>` or an
`oidc:<issuer>#<subject>` of the configured issuer names, who can see the project. At an execution
the identity must resolve to the member the dry run's report named for the file (`Target.Named`);
anybody else — a person added as a member since — is assigned nobody, with a warning, so the
execution admits nobody to a ticket the report showed without them (`entry.unnamed`). For a request
through a token or an agent's (`Target.TokenPerson`, set by `importTarget` unless the caller is a
person's browser session), a confidential ticket is assigned to the token's person or to nobody,
whoever the file or a correction names, as a token's assignment is held ([ADR 0065] D9, [ADR 0043]
D3). `admission` applies both after the flag is known and warns once on the file, the token's
reason first.

**The confidential flag** (`confidential`; [ADR 0065] D7), first match wins: a `publication-accepted`
date leaves it unset; an export's `confidential: true` sets it; the `local_` prefix sets it; a
`live` or `boundary` class without a `shipped` line sets it; with one, it stays unset. The report
gives the reason whenever one of these decided. The importer applies the class's rule to an export's file as well:
an export written before grammar v1 carried the flag cannot publish an open finding, at the cost of
setting the flag again on an open finding whose flag an administrator lifted.

**The report** ([`report.go`](../../backend/internal/importer/report.go)) is the API document's
`ImportSummary` and `ImportFile` field by field in JSON; the job stores it as `jsonb` and the API
decodes it into the generated types. `Correction` writes back only what a correction named.

## The export

`ExportProject` and `ExportTeam` ([`exports.go`](../../backend/internal/api/exports.go)) decide who
exports what in a short `InTenant` transaction and answer an `exportStream`, which writes the
`tar.gz` as it reads it:

- **Who.** The project's: the project through the predicate and `read` on the project's role;
  the team's: `read` on the team's role, the projects through `ListProjects` with the archived
  ones. An agent too. A token restricted to a project gets the project's export only, by the
  boundary's rule.
- **One at a time, a page at a time.** `exportStream.write` waits for the replica's one export slot
  (`slot`, as the import's, within the request's deadline), then reads everything in one
  `InTenantSnapshot` — a read-only `REPEATABLE READ` transaction, so the counts the manifest writes
  first are the documents that follow. It reads the tickets `exportPage`, 50, at a time
  (`store.ByNumber`), renders and writes each, and holds no more than that page: the archive is
  never in memory (`TestTheExportStreamsALargeProjectWithinAMemoryBound` exports 600 documents of
  192,000 characters each, 115 MB, with the heap growing by about 14 MB). The manifests are read
  whole: a link and an attachment are a few hundred bytes each.
- **The documents**: every ticket the caller sees, done and dropped ones included, deleted ones
  not — `ListTickets` with `IncludeTerminal`, project by project in key order and by number —,
  each rendered by `exportDocument` exactly as `…/markdown` answers it, at
  `<team>/<PROJECT>-<n>.md`; `CountTickets` with the same filter is each project's count.
- **The manifests**, at the archive's root: `manifest.json` (`apigen.ExportManifest`: the format
  `cowork export v1`, the team's slug — as `team`, and as `tenant` beside it for the importers of the
  release before, which read no other name, for one release —, each project with its count of
  documents and of the confidential
  tickets left out, the time, the exporter as grammar v1 writes a person, the totals);
  `links.json` (`ExportLinks`: every link with an end in the project — or, for the team, every
  link — whose two ends the caller sees, once); `attachments.json` (`ExportAttachments`: ticket,
  name, type, size and the path of the bytes, under `/api/v1/teams/…`, `attachmentContentURL`).
- **The count of what it leaves out** is `ExportHiddenConfidential`, exempt from the predicate by
  name: the confidential tickets of the projects the caller sees that the caller cannot read. The
  predicate answers `NULL`, not `false`, for a ticket without an assignee, so the query asks
  `IS NOT TRUE`; `TestTheExportFollowsItsReader` failed on `NOT` alone.
- **The act**: `recordExport` writes `exported` on the project or the team with the format and the
  counts, once they are read and before the first byte of the archive is answered; `exported` is
  never published.
- **The answer**: `application/gzip`, `Content-Disposition: attachment;
  filename="<team>-<PROJECT>-<YYYYMMDD>.tar.gz"` (`<team>-<YYYYMMDD>.tar.gz` for the team),
  no `Content-Length`; the three manifests first — the browser reads `manifest.json` from the
  archive's start —, then the documents; every entry carries the export's time. A failure before
  the answer starts is a problem as anywhere, a wait for the slot past the deadline `504`; one after
  it is logged and cuts the connection off (`panic(http.ErrAbortHandler)`, which the recoverer
  passes on), so a client sees a broken transfer, never a short archive that ends cleanly. The
  request timeout bounds the whole stream. The validator reads `application/gzip` with kin-openapi's
  file decoder, registered in [`validate.go`](../../backend/internal/api/validate.go).

## `cowork-mcp export`

`exportProject` in [`mcpcli/export.go`](../../backend/internal/mcpcli/export.go) takes
`<team>/<PROJECT>` and a directory (usage, exit `2`, otherwise); refuses a directory that is not
empty, or a file, before it asks (exit `1`); fetches the export through the generated client with
the configuration of every subcommand; and unpacks it through a root opened at the directory
(`os.OpenRoot`), which no name or link reaches out of: regular files only, each named as an export
of that project names it — `exportEntry` takes a valid `/`-separated path that is one of the three
manifests or `<team>/<PROJECT>-<n>.md` of that team and project, the number as its key writes
it, and refuses every other —, created with `O_EXCL`; on POSIX systems the directories `0700` and
the files `0600`, on Windows, where Go sets only the read-only attribute, the directory's access
list ([docs/security/import-and-export.md](../security/import-and-export.md#h-77) H-77). It prints
the count of documents and, when there are any, of the confidential tickets left out. Its requests
carry the agent mark `cowork-mcp/unknown/export`, as every request of the binary carries one, so the
export's act names the binary ([mcp.md](mcp.md)).

## `cowork-mcp import`

`importProject` in [`mcpcli/import.go`](../../backend/internal/mcpcli/import.go) takes
`<team>/<PROJECT>`, a path and `--dry-run` anywhere among them ([ADR 0070] D2). `uploadOf` reads
the path before anything is asked: a directory is packed by `packDir`, reading through a root opened
at it (`os.OpenRoot`), as a `tar.gz` of the regular files the import reads (`importRead`: `.md`, `manifest.json`, `links.json`), named by their path
under the directory as it was given — `docs/tickets/001-….md` — or under its base name when the path
is absolute or leaves the working directory; a file goes as it is, one part named `file`. It makes
the dry run with the generated client's `CreateImportWithBodyWithResponse`, prints its report
(`writeImport`: the summary, then per file the outcome, key, title, type, state and confidential
flag, `why:` its reason, its parent and links, `error` and `warning` lines with field and line), and
unless `--dry-run` executes it without corrections and prints the executed report and a line that
the importer sets no parent a file does not name. Its requests carry `cowork-mcp/unknown/import`, so
the import is an agent's — [ADR 0051] D6 admits it, and a confidential ticket it imports is assigned
to the token's person or to nobody. A unit test holds the packing, the output and the refusals
([`mcpcli/import_test.go`](../../backend/internal/mcpcli/import_test.go)); `TestTheImportSubcommand`
runs it against the server, `TestTheBinaryRunsItsSubcommands` as the built binary.

## Tests

| Tier | Test | Proves |
|---|---|---|
| unit | [`parse_test.go`](../../backend/internal/importer/parse_test.go) | the four shapes of [ADR 0063]'s Consequences over copies of this repository's ticket files, the questions of a repository, every error class with its line, the names that are skipped, the questions outside fenced code |
| unit | [`roundtrip_test.go`](../../backend/internal/importer/roundtrip_test.go) | every golden file of `/markdown` in [`internal/markdown/testdata`](../../backend/internal/markdown/testdata/) parses without an error — `questions.md` with the one warning of its body's heading — and renders again to its bytes, and every one of `/context` is skipped; render, parse, render is the same document for values with quotes, colons, fences and every answer form |
| unit | [`analyze_test.go`](../../backend/internal/importer/analyze_test.go) | a dry run of the fixtures, the conflicts left out with their reasons and a purged number given back, the corrections and their rules, an export read back with its links, a block on a ticket, parents and a loop; every text at the lengths the API takes and one character beyond, a body grown beyond by the keys the import puts in, and one grown beyond because a file it names was left out (`TestTheImportHoldsTheTextsToTheLengthsOfTheAPI`); the assignee the dry run named and an agent's confidential ticket (`TestAnalyzeAssignsWhomTheDryRunNamedAndAnAgentMay`) |
| unit | [`upload_test.go`](../../backend/internal/importer/upload_test.go) | the three forms of an upload, the bounds, the stored form; a zip of 196,708 empty entries whose end declares 100 refused before it is parsed, its memory a fraction of what a parse takes (`TestReadUploadCountsAZipsEntriesBeforeItParsesThem`); a hand-written frontmatter read line by line |
| unit | [`mcpcli/export_test.go`](../../backend/internal/mcpcli/export_test.go) | the unpacking stays inside its directory and never overwrites; it writes the names an export holds and no other — backslashes, steps, volumes, other teams and projects, numbers not written as keys —, and a directory link planted in the target leads nowhere outside it |
| integration | [`api_imports_test.go`](../../backend/test/integration/api_imports_test.go) | the dry run and its execution end to end, who may — a viewer and a `read` token do not, another writer neither reads nor executes a job —, the acts, one event, the sequence, `409 import_executed`; a conflict and an error left out and the rest imported, also a conflict filed after the dry run, an exclusion naming the project's ticket (`TestImportLeavesOutWhatItCannotImport`); a member's agent token without any capability importing and setting a parent afterwards, a confidential ticket assigned to nobody (`TestAWriterAndTheirAgentImport`); a plain token's import assigning a confidential ticket only to its person, a browser session's as the file says (`TestATokenImportAssignsAConfidentialTicketToItsPersonOnly`); a purged number given back and a `/context` document skipped (`TestImportGivesAPurgedNumberBack`); a member added between a browser session's dry run and its execution not assigned (`TestTheExecutionAssignsWhomTheDryRunNamed`); the bounds and the expiry; the policies of `import_jobs` as the runtime role meets them; the purge of an imported ticket taking its file out of the report, by the job; this repository's whole `docs/tickets/` read without an error, the open tickets after the execution as many as the source's open `state:` lines |
| integration | [`api_exports_test.go`](../../backend/test/integration/api_exports_test.go) | the round trip of [ADR 0051] D5; the export as each reader sees it; a project of 115 MB of bodies streamed with the heap growing by a fraction of it (`TestTheExportStreamsALargeProjectWithinAMemoryBound`) |
| unit | [`mcpcli/import_test.go`](../../backend/internal/mcpcli/import_test.go) | the import's arguments and refusals; a directory packed with the files the import reads under the path as given, a link and the rest left out; both reports printed; `--dry-run` executing nothing; a file sent as it is and the installation's refusal |
| integration | [`mcp_test.go`](../../backend/test/integration/mcp_test.go) `TestTheExportSubcommand`, `TestTheImportSubcommand`, `TestTheBinaryRunsItsSubcommands` | the subcommands by their command line and by the built binary: the export; the import's dry run, its execution as an agent's acts, the same files again left out as conflicts, a viewer refused |

**The fixtures** under [`internal/importer/testdata/tickets/`](../../backend/internal/importer/testdata/tickets/)
are copies of this repository's ticket files as of 2026-10-06 and three files made for the shapes
the repository does not hold: a record without frontmatter, a `README.md`, and an embargoed file —
named `embargoed-099-…` and read as `local_099-…`, because a `local_` name is ignored by many a git
configuration and the fixture must be in every clone. They are test data, not references to
tickets. `TestImportThisRepositorysTickets` reads the live `docs/tickets/` of the checkout instead,
so a ticket file the importer cannot read fails the tier.

## Changing it

- A new frontmatter key: its constant and its reader in [`fields.go`](../../backend/internal/importer/fields.go),
  what the analysis makes of it, a case in `TestParseNamesEveryErrorOfAFile` when it can be wrong,
  and the mapping in [ADR 0063] D3.
- A change of grammar v1 is a change of [`markdown.Render`](../../backend/internal/markdown/markdown.go),
  its golden files, the reader here and the round trip — `TestParseReadsTheGoldenFilesOfGrammarV1`
  fails until the reader follows ([markdown-grammar.md](markdown-grammar.md#changing-the-grammar)).
- A new field of the report: the schema first, then `FileReport` and its `json` tag; the
  integration tier validates every answer against the document.

[ADR 0007]: ../adr/0007-a-ticket-key-is-globally-unique-tenant-slash-project-dash-number.md
[ADR 0043]: ../adr/0043-agent-capabilities-are-chosen-per-token-the-default-is-everything-reversible-and-attributable.md
[ADR 0008]: ../adr/0008-five-ticket-types-and-an-optional-parent-in-the-same-project.md
[ADR 0009]: ../adr/0009-ticket-states-are-the-frontmatter-states-plus-blocked.md
[ADR 0010]: ../adr/0010-the-frontmatter-vocabularies-become-ticket-columns.md
[ADR 0011]: ../adr/0011-a-ticket-is-a-markdown-body-plus-first-class-open-questions.md
[ADR 0044]: ../adr/0044-two-endpoints-markdown-is-the-canonical-ticket-context-is-the-ticket-with-what-surrounds-it.md
[ADR 0051]: ../adr/0051-import-is-a-server-side-two-phase-atomic-job-export-is-its-mirror.md
[ADR 0063]: ../adr/0063-the-importer-takes-whatever-the-user-hands-it-open-and-archived-tickets-alike.md
[ADR 0064]: ../adr/0064-one-direction-import-and-export-no-synchronisation.md
[ADR 0065]: ../adr/0065-a-confidential-flag-replaces-the-file-name-embargo-set-automatically-lifted-only-by-a-person.md
[ADR 0070]: ../adr/0070-no-general-cli-the-mcp-binary-grows-workflow-subcommands.md
