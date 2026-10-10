# ADR 0051: Import Is a Server-Side, Two-Phase, Atomic Job With a Correctable Report; Export Is Its Mirror; No Generic Batch API

## Status

Accepted. Date: 2026-10-01. Decided by the owner as the answer to the catalog question "bulk
and import?": a server-side import job with dry run and an export endpoint that mirrors it,
over the importer as a client of the ordinary routes, over a generic batch API, and over the
import job without the export. The rules of D6–D9 were put to the owner with the question
and not objected to.

Amended 2026-10-06 (the References: the project plan whose phase 6 held the cut-over is consumed
into the phase tickets and deleted,
[ADR 0074](0074-the-question-catalog-is-consumed-phases-become-tickets-when-they-start-in-their-own-session.md)
D4; the cut-over is this record's Consequences; no rule changes).

Amended 2026-10-06 by the owner (D4: the links manifest), answering whether the links enter the
export: into the project export only, as a links manifest beside the tickets, each link once,
over `/markdown`'s frontmatter, which would write a link at both its ends and change what one
ticket's document is
([ADR 0044](0044-two-endpoints-markdown-is-the-canonical-ticket-context-is-the-ticket-with-what-surrounds-it.md)
D1), and over an export without them, which would lose every link of a backup restored through
the importer ([ADR 0059](0059-backups-belong-to-the-operators-cowork-provides-the-export-and-makes-a-restores-inconsistency-visible.md)
D2). The manifest is built with the export. What the manifest holds and how the importer reads
it (D4's second paragraph) is the implementer's, made concrete the same day and open to the
owner's objection. The Residual risks name what follows from D2 and D9 as they stand: a restore
keeps the links within a project only.

Made concrete 2026-10-06 by the implementer where the record left the detail open, each marked
in place and open to the owner's objection: the shape of the upload (D1); the two phases as two
routes, what a correction names, what the dry run keeps for its execution, and that an error
refuses an execution as a conflict does (D2); where a ticket records its source (D3); the
archive's layout, and that the links manifest holds a link only where the reader sees both its
ends (D4); what the round trip compares (D5); who reads a job (D6); the bounds beside D7's variable
(D7).

Amended 2026-10-09 by the owner (D2, D6; D7 follows): the import is the agent's tool, and nothing
in it refuses — "Das LLM hat freie Hand. Es stellt Verbindungen zwischen den Tickets her, es darf
Tickets egal welcher Nummer importieren. Das LLM sorgt dafür dass Family-Tickets zusammenhängen oder
eben nicht. […] Ich will mit LLMs auf einem Ticket System arbeiten." The dry run, the execution and
the read of a job are a writer's acts of the project, as creating a ticket is, an agent's included
(D6, with [ADR 0043](0043-agent-capabilities-are-chosen-per-token-the-default-is-everything-reversible-and-attributable.md)
D2 amended the same day); the execution imports every file it can and leaves out each file with an
error or a conflict, the report naming each and why, and a `/context` document is skipped with its
reason (D2, with [ADR 0063](0063-the-importer-takes-whatever-the-user-hands-it-open-and-archived-tickets-alike.md)
D5 and [ADR 0044](0044-two-endpoints-markdown-is-the-canonical-ticket-context-is-the-ticket-with-what-surrounds-it.md)
D3 amended the same day); a number a purged ticket held is imported
([ADR 0007](0007-a-ticket-key-is-globally-unique-tenant-slash-project-dash-number.md) D4,
[ADR 0064](0064-one-direction-import-and-export-no-synchronisation.md) D3, amended the same day); the
importer guesses no parent, and the person's agent sets the parents after the import through the
ordinary routes (D2). Built the same day: `cowork-mcp import` ([ADR 0070](0070-no-general-cli-the-mcp-binary-grows-workflow-subcommands.md)
D2), the policies of `import_jobs` that admit a job's maker beside the tenant's administrators
([migration 45](../../backend/internal/store/migrations/000045_import_jobs_of_their_writer.up.sql)),
and the import page offered to every writer of the project. What D6 holds of a job's maker, what
D2 holds of an assignee at the execution and what D7 holds of the texts and of the export are made
concrete by the implementer the same day, each marked in place, open to the owner's objection.

~~**Not built.** No import, no export, no job entity.~~ **Built** (phase 6, 2026-10-06, in the API;
~~the UI's import page and export button outstanding~~ *(built 2026-10-07, below)*): D1–D9 — the routes of
[`imports.yaml`](../../backend/api/imports.yaml): `POST …/projects/{project}/imports` (the dry
run), `GET …/imports/{import}` (the job and its report), `POST …/imports/{import}/execution` (the
execution with its corrections), `GET …/projects/{project}/export` and
`GET /api/v1/tenants/{tenant}/export`; the table `import_jobs` and the ticket's
`imported_from_file` and `imported_from_job`
([migration 43](../../backend/internal/store/migrations/000043_import_jobs.up.sql)); the reading
and the analysis in [`internal/importer`](../../backend/internal/importer/); the handlers
[`imports.go`](../../backend/internal/api/imports.go),
[`importwrite.go`](../../backend/internal/api/importwrite.go) and
[`exports.go`](../../backend/internal/api/exports.go); the job `import-expiry`
([`store/imports.go`](../../backend/internal/store/imports.go)). D5 is
`TestTheExportRoundTripsThroughTheImport`, and a dry run and an execution of this repository's
whole `docs/tickets/` is `TestImportThisRepositorysTickets`. How it works:
[docs/developer/import-and-export.md](../developer/import-and-export.md); what an administrator
does: [docs/operations/import-and-export.md](../operations/import-and-export.md); what it lets in
and out: [docs/security/import-and-export.md](../security/import-and-export.md).

*(2026-10-07.)* The Consequences' import page and export button are built in the UI: ~~a tenant
administrator's~~ *(2026-10-09: every writer's of the project, D6)* import page of a project at `/t/{slug}/p/{KEY}/imports`, its job at
`…/imports/{import}` — the report with its summary, what blocks the execution, every file with its
outcome and the correction fields of D2 (left out, type, state with its block, assignee), the
execution after a question, ~~its refusals on the files they name~~ *(2026-10-09: the files it will
leave out marked as such, D2)* —; the export of a project from
its header for whoever reads it, and of the tenant from its settings for its administrators, each
saying how many confidential tickets the archive leaves out
([`project-import.ts`](../../frontend/src/app/features/project/project-import.ts),
[`imports.service.ts`](../../frontend/src/app/core/imports.service.ts)). How it is built:
[docs/developer/frontend.md](../developer/frontend.md#the-import-and-the-export). That the UI offers the
tenant's export to its administrators alone, while the API serves it to every reader of the tenant
(D6), is the implementer's choice of the same day, open to the owner's objection.

## Context

The Markdown tickets of the owner's repositories move into cowork in phase 6. Earlier
records fixed what an import must do: read the `/markdown` grammar and refuse a `/context`
document ([ADR 0044](0044-two-endpoints-markdown-is-the-canonical-ticket-context-is-the-ticket-with-what-surrounds-it.md)
D1, D3), keep the ticket numbers and advance the sequence ([ADR 0007](0007-a-ticket-key-is-globally-unique-tenant-slash-project-dash-number.md)
D6), map types by content and report per ticket for correction ([ADR 0008](0008-five-ticket-types-and-an-optional-parent-in-the-same-project.md)
D5), never infer `blocked` ([ADR 0009](0009-ticket-states-are-the-frontmatter-states-plus-blocked.md)),
refuse out-of-vocabulary values ([ADR 0010](0010-the-frontmatter-vocabularies-become-ticket-columns.md)
D5), turn `blocked-by: T<n>` and `filed-from` into links ([ADR 0012](0012-four-typed-directed-links-within-a-tenant.md)).
An importer that fires two hundred `POST`s from outside cannot be atomic, cannot reserve
numbers, needs two passes for links and produces a report the server might disagree with. A
generic batch API would be a subsystem for one caller that still needs the parser. The parser
exists anyway: the export writes the same grammar.

## Decision

**D1 — Import is a job on a project:** `POST /api/v1/tenants/{slug}/projects/{KEY}/imports`
takes an archive (`tar.gz` or `zip`) or a multipart set of Markdown files in the
`/markdown` grammar, creates an **import job** (an entity with status, report and the
actor), and answers with it. *(Made concrete 2026-10-06 by the implementer, open to the owner's
objection:)* the request is `multipart/form-data` with one or more parts named `file`, each a
`tar.gz` or a `zip` archive — known by its first bytes, whatever its name — or one Markdown file
named by the part's file name; the answer is `201` with the job and its `Location`. The files of
a repository's ticket grammar and the documents of an export are read alike
([ADR 0063](0063-the-importer-takes-whatever-the-user-hands-it-open-and-archived-tickets-alike.md)).

**D2 — Two phases, the first a dry run.** `dry_run=true` parses everything and produces the
report: per file the detected type, the state, the columns, the questions, the links
resolved to target keys inside the archive, warnings (`blocked-by: human` as a candidate for
`blocked`; a `filed-from` that names an event; a link to a key outside the archive, which is
omitted), and errors (an out-of-vocabulary value, a malformed frontmatter~~, a `/context`
document~~ *(amended 2026-10-09 by the owner: a `/context` document is skipped with its reason,
ADR 0063 D5)*). `dry_run=false` with the id of a dry run executes it, with optional corrections
per file (type, state, assignee) — the correctable report of ADR 0008 D5.

*(Made concrete 2026-10-06 by the implementer, open to the owner's objection:)* the two phases are
two routes, not a parameter: `POST …/imports` is always the dry run, and
`POST …/imports/{import}/execution` executes one, so no request imports without a dry run before
it. The dry run keeps the files it read, compressed, with the job; the execution reads them again
— never a second upload — and analyses them anew, with the corrections, against the project as it
stands then. A correction names a file by its path in the upload and sets its type, its state —
with the block when the state is `blocked` —, its assignee or none, or excludes it. ~~A file the
execution would import that has an error or a conflict refuses the whole execution,
`409 import_conflict` naming each such file: the person excludes it, or corrects the source and
makes a new dry run. The importer never leaves a file out on its own.~~

*(Amended 2026-10-09 by the owner:)* nothing in an import refuses. The execution imports every
file it can and leaves out each file with an error or a conflict — one whose number a ticket filed
since the dry run took included —, and the report names each with its `reason`; the person, or
their agent, reads it and acts: excludes, corrects and imports again, or files what is missing.
A correction that breaks a rule of the API document is still `400`, and a second execution of the
same dry run `409 import_executed` (D3). The importer guesses no parent: a file names its own
(`parent:`, an export's key), and a repository's family tickets are joined by the person's agent
after the import, through the ordinary routes, or not at all ("Das LLM sorgt dafür dass
Family-Tickets zusammenhängen oder eben nicht"). *(Made concrete 2026-10-09 by the implementer,
open to the owner's objection:)* a file left out takes nothing with it that the analysis does not
see: a reference to it is a line under `## Related`, a block on it an error, a link to it omitted,
as for any file that is not imported; and an assignee a file names by its identity is the member
the dry run named — an identity that resolves to anybody else at the execution assigns nobody,
with a warning, so the execution admits nobody to a ticket the report showed without them
([ADR 0065](0065-a-confidential-flag-replaces-the-file-name-embargo-set-automatically-lifted-only-by-a-person.md)
D9).

**D3 — Execution is atomic per job.** All tickets, questions, links and the sequence advance
in one transaction ([ADR 0027](0027-data-access-is-sqlc-over-pgx-behind-a-tenant-transaction-and-a-mutation-wrapper.md)
D3) or none; the numbers of ADR 0007 D6 are kept; every created row has an audit entry with
the actor and the `import_job_id`; each ticket records `imported_from` (file name, job id).
A dry run is executed at most once; a second execution of the same dry run answers `409`.
*(Made concrete 2026-10-06 by the implementer, open to the owner's objection:)* `imported_from` is
two columns of the ticket, `imported_from_file`, the file's path in the upload, and
`imported_from_job`; the API does not answer them on a ticket, and the ticket's `created` act
carries both. The audit rows name the job as `import_job`; the acts on what the execution creates
are written and not published, and the job's one act `imported` announces the import to the
streams as `project.changed`
([ADR 0054](0054-server-sent-events-per-tenant-carry-keys-not-content-polling-is-the-fallback.md)
D2). The second execution is `409 import_executed`. The job's report is kept for good, and the purge
of a ticket the import created takes that ticket's file out of it
([ADR 0024](0024-deletion-tickets-are-soft-deleted-and-purged-projects-archived-people-deactivated-tenants-deleted-explicitly.md)
D2), the summary still counting it.

**D4 — Export is the mirror:** `GET /api/v1/tenants/{slug}/projects/{KEY}/export` returns an
archive with every ticket of the project as its `/markdown` document, named by key, plus an
attachment manifest (names, types, sizes, URLs — never bytes), *(amended 2026-10-06 by the
owner:)* a links manifest beside the tickets, each link once, since `/markdown` carries no links
(ADR 0044 D1), and a `manifest.json` with the
project, the export time and the exporter. A tenant export is the union over its projects
(the backup record's second line).

*(Made concrete 2026-10-06 by the implementer, open to the owner's objection:)* the links
manifest lists every link with an end in the project *(built 2026-10-06: and whose other end the
reader sees too — a link to a ticket the reader cannot see would tell them it exists,
[ADR 0065](0065-a-confidential-flag-replaces-the-file-name-embargo-set-automatically-lifted-only-by-a-person.md)
D5)*, by its source key, its type and its target key, and a tenant export lists each link once. The importer reads it with the tickets under D2
and D9, so a restore through it keeps the links between the tickets of the project. A link to a
ticket of another project ([ADR 0012](0012-four-typed-directed-links-within-a-tenant.md) D2) is
listed, and the import reports it and omits it: its other end is neither in the archive nor in
the project (D2, D9).

*(Made concrete 2026-10-06 by the implementer, open to the owner's objection:)* the archive is a
`tar.gz`, answered as `application/gzip` with a file name of the tenant, the project and the day.
At its root are `manifest.json` — the format `cowork export v1`, the tenant, each project with its
key, name, whether it is archived, its count of documents and its count of the confidential
tickets left out (ADR 0065 D5), the time, the exporter written as grammar v1 writes a person, and
the totals —, `links.json` and `attachments.json` — each attachment's ticket, name, type, size
and the path of its bytes, which only a reader of the ticket fetches —; each ticket the reader sees
is `<tenant>/<PROJECT>-<n>.md`, its key, done and dropped ones included, deleted ones not. A tenant
export holds every project the reader sees, archived ones included.

**D5 — The round trip is the proof.** The integration tier exports a project, imports it into
an empty project, exports again and asserts the two archives are equal up to keys and times;
that test is what keeps the grammar of ADR 0044 D1 honest. *(Made concrete 2026-10-06 by the
implementer, open to the owner's objection:)* "up to keys and times" takes out the export's time
and the project's key and name; every document, the links manifest — a `relates-to` link's ends in
order, as it is stored once — and the attachments manifest must be equal. The test's project
holds no attachment: an import brings no attachment's bytes (D4), so a project with attachments
comes back without them (Residual risks).

**D6 — ~~Import is an administrator's act and never an agent's.~~** ~~Role `admin` in the tenant;
a flagged token ([ADR 0043](0043-agent-capabilities-are-chosen-per-token-the-default-is-everything-reversible-and-attributable.md)
D3) is refused.~~ *(Amended 2026-10-09 by the owner:)* **Import is a writer's act of the project,
as creating a ticket is, an agent's included:** the role `member` in the tenant — a restricted
project's list may lower it —, a token's `write` scope, and no capability; the dry run, the
execution and the read of a job alike; an agent's import assigns a confidential ticket to its own
person or to nobody, as an agent's filing does (ADR 0043 D3) *(and, made concrete 2026-10-09 with
the owner's rule of that day that an act which gives sight of a confidential ticket takes a browser
session: so does every import through a token, an agent's or not, the report saying why on the
file; a browser session's import assigns as the file says)*. Export follows the project's read
permission. ~~*(Made concrete 2026-10-06 by the implementer, open to the owner's objection:)* a dry
run and an execution need the role `admin` and a token's `admin` scope and refuse every agent, a
flagged token and a request with the agent header alike (`403 agent_forbidden`, the hard-off rule
"administration"); reading a job needs the role `admin` and the `read` scope and is never an
agent's either, since its report holds what the upload's files say, of those the import left out
too.~~ *(Made concrete 2026-10-09 by the implementer, open to the owner's objection:)* a job is its
maker's and the tenant's administrators': the policies of `import_jobs` admit nobody else, and
another writer's read or execution of it is `404` — a report holds what its upload's files say, an
embargoed finding's title and threat among them, and its maker uploaded them, while the
administrators read every confidential ticket anyway
([ADR 0065](0065-a-confidential-flag-replaces-the-file-name-embargo-set-automatically-lifted-only-by-a-person.md)
D4). The tenant export follows the tenant's read permission and holds the projects the reader sees;
an agent exports as its person reads.

**D7 — Sizes and validity.** `COWORK_MAX_IMPORT_BYTES` (default 50 MiB, `0` disables,
[ADR 0039](0039-no-request-budgets-size-and-time-limits-instead-configurable-and-switchable.md)
D2) bounds the archive; a dry run's report is valid for twenty-four hours and then expires.
*(Made concrete 2026-10-06 by the implementer, open to the owner's objection:)* the variable bounds
the request's body, with 64 KiB for the multipart framing, and the bytes the upload's files hold
together once unpacked, a file the import does not read counted by the size its archive declares
— both `413`. Beside it, fixed: at most 10,000 files an upload, a path of at most 1,024 bytes, and
one import at a time per replica, a dry run or an execution, since each holds its upload in memory;
the request timeout ([ADR 0039](0039-no-request-budgets-size-and-time-limits-instead-configurable-and-switchable.md)
D2) bounds both. A dry run past its day answers `404` to a read and to an execution, and the job
`import-expiry` deletes it with its files within the hour; an executed job stays, its report
without the files, because its tickets name it. *(Made concrete 2026-10-07 by the implementer, open
to the owner's objection:)* the texts an execution writes are held to the lengths the API holds
every write of them to — a body of at most 200,000 characters, a question's options and its answer
of at most 100,000 each, counted as the execution writes them, with the lines and the keys the
import adds —; a longer one is an error of its file, which ~~refuses the execution~~ *(2026-10-09:
the execution leaves out)* (D2). *(Made concrete 2026-10-09 by the implementer, open to the owner's
objection:)* so are a question of at most 2,000 characters and a recommendation of at most 10,000,
as the execution writes them, a threat, a block's reason and a dropped ticket's reason of at most
2,000, and a done ticket's note of at most 10,000; a `zip` is counted by the entries its bytes hold,
its directories included, before it is parsed. The export is streamed: it holds a page of tickets,
not its archive, and a replica runs one export at a time, as it runs one import.

**D8 — No generic batch API.** No `POST …/tickets:batch`, no transactional operation list.
A bulk need beyond import is a question of its own.

**D9 — Links that leave the archive are warnings, not guesses.** A `blocks` or `found-in`
whose target key is not in the archive and not already in the project is reported and
omitted; the person adds it after the import if it belongs. *(Made concrete 2026-10-10 on the
recommendation, open to the owner's objection, not built:)* a parent or a link into another team
([ADR 0005](0005-a-tenant-is-a-client-organisation-and-the-isolation-unit.md) D3) is exported by its key and nothing else, never the other ticket's head; the import
resolves such a key under [ADR 0008](0008-five-ticket-types-and-an-optional-parent-in-the-same-project.md) D2's rule — the importing person must be able to read the other
ticket — and reports it as not set where they cannot, as an unresolved parent is reported today;
nothing in an import refuses
([ADR 0063](0063-the-importer-takes-whatever-the-user-hands-it-open-and-archived-tickets-alike.md)).

## Consequences

- The importer is server code with tests, not a script; the parser is shared with the
  export; atomicity and numbers are the server's.
- One job entity, ~~two routes~~ five routes *(2026-10-06: the dry run, the read and the
  execution of a job, the project's and the tenant's export)*, an archive handler bounded by D7;
  the UI gets an import page that shows the report with the correction fields and an export
  button on the project.
- The round trip of D5 doubles as the backup verification the operations page can recommend.
- The owner's cut-over of the sibling project (phase 6) is: export nothing, import the
  directory, read the report, correct, execute, verify the count.

## Alternatives Considered

- **The importer as a client of the ordinary routes.** No new routes; hundreds of requests,
  no atomicity, numbers need a server path anyway, links need two passes, the report is the
  client's opinion. Lost.
- **A generic transactional batch API.** Universal; partial transactions, per-item
  idempotency, per-item authorization and errors — a subsystem for one caller that still
  needs the parser. Lost.
- **Import without the export mirror.** Saves one endpoint and loses the round-trip proof
  and the second backup line. Lost.

## Residual risks

- *(2026-10-09.)* An agent may import into every project its person may write, the owner's accepted
  tradeoff of 2026-10-09: it creates tickets in the states, horizons and answers its files carry,
  which the capabilities of ADR 0043 D4 would not let it reach by transitions, and an execution
  nobody reads the report of leaves files out unseen — the report names each, and
  `cowork-mcp import` prints it.
- D3's single transaction grows with the archive; fifty megabytes of Markdown is thousands
  of tickets and still seconds. A tenant-scale import is split by project.
- D2's type detection is heuristic by design; the report and the corrections are the
  guard, and an import executed without reading the report mis-types some tickets.
- *(2026-10-06.)* A restore through the importer keeps the links within a project only. Links
  may cross projects ([ADR 0012](0012-four-typed-directed-links-within-a-tenant.md) D2), an
  import is a job on one project (D1), and a link whose other end is outside the archive and
  the project is reported and omitted (D2, D9); a tenant export carries the link, but no tenant
  import exists that would resolve it. The person adds it again after the import, as D9 says.
- *(2026-10-06.)* What the archive does not carry does not come back through the importer: the
  comments, the time entries, the activity, the attachments' bytes, the rank — the open tickets
  of an import join the bottom of the rank, parents first and otherwise by number —, the interest,
  and the reporter, since the person who executes the import is the reporter of every ticket it
  creates. The export is the second line of a backup for the tickets themselves, not for
  everything around them ([ADR 0059](0059-backups-belong-to-the-operators-cowork-provides-the-export-and-makes-a-restores-inconsistency-visible.md)).
- *(2026-10-06.)* D7's bounds are not measured at their size. An import of this repository's
  56 ticket files (412 KiB) ran its dry run and its execution in under a second together in the
  integration tier; an upload of 50 MiB is held in memory several times over — its files, their
  compressed copy, the parsed text, the report — and its execution runs within the request
  timeout, 30 seconds by default, or rolls back. One import at a time per replica bounds the
  memory; the operations page says to keep the backend's memory limit well above the variable
  and to split an import by directory.

## References

- [ADR 0044](0044-two-endpoints-markdown-is-the-canonical-ticket-context-is-the-ticket-with-what-surrounds-it.md) D1, D3 — the grammar imported and exported
- [ADR 0007](0007-a-ticket-key-is-globally-unique-tenant-slash-project-dash-number.md) D6, [ADR 0008](0008-five-ticket-types-and-an-optional-parent-in-the-same-project.md) D5, [ADR 0009](0009-ticket-states-are-the-frontmatter-states-plus-blocked.md), [ADR 0010](0010-the-frontmatter-vocabularies-become-ticket-columns.md) D5, [ADR 0012](0012-four-typed-directed-links-within-a-tenant.md) — what the import must do
- [ADR 0043](0043-agent-capabilities-are-chosen-per-token-the-default-is-everything-reversible-and-attributable.md) D3, [ADR 0034](0034-three-tenant-roles-an-optional-project-restriction-no-implicit-role-for-the-global-administrator.md) — who may import
- [ADR 0059](0059-backups-belong-to-the-operators-cowork-provides-the-export-and-makes-a-restores-inconsistency-visible.md) D2 — the backup's second line, whose links within a project the links manifest keeps
