# ADR 0064: One Direction — Import and Export, No Synchronisation; cowork Never Touches a Repository, and a Repeated Import Is a Duplicate, Not an Update

## Status

Accepted, amended 2026-10-06 (the References: the project plan whose phase 6 held the cut-over
stages is consumed into the phase tickets and deleted,
[ADR 0074](0074-the-question-catalog-is-consumed-phases-become-tickets-when-they-start-in-their-own-session.md)
D4; the stages are this record's Consequences; no rule changes). Date: 2026-10-01. Decided by the owner as the answer to the catalog question
"direction after the import?": one direction, over two-way synchronisation, over a one-way
mirror written by cowork, and over an MCP convenience tool for the export. The rules of D4–D5
were put to the owner with the question and not objected to.

~~**Not built.** No importer, no exporter.~~ Amended 2026-10-04 (D4: the workflow plan that was to
carry the sentence beside the operations page is consumed).

**Built** (phase 6, 2026-10-06, in the API): D1–D3 and D4's sentence — the import and the export
of [ADR 0051](0051-import-is-a-server-side-two-phase-atomic-job-export-is-its-mirror.md), neither of
which holds a repository's credential or reaches one; the dry run reports a number the project
holds as a conflict naming the ticket's key, and the execution refuses while one remains
(`409 import_conflict`, [`imports.go`](../../backend/internal/api/imports.go) `blockingProblem`).
*(Made concrete 2026-10-06 by the implementer, open to the owner's objection:)* a number the
project holds is one of any of its tickets, a deleted one included, and a number a purged ticket
held is a conflict too, since a number is never handed out again
([ADR 0007](0007-a-ticket-key-is-globally-unique-tenant-slash-project-dash-number.md) D4). D4's
sentence is on [docs/operations/import-and-export.md](../operations/import-and-export.md); this
repository's own `docs/tickets/README.md` gets its note on the day its tickets are imported, which
has not happened. D5 found the `api` tool unfit for it (below).

## Context

cowork exists to replace Markdown tickets spread over many repositories with one backlog.
After an import the question is whether the files and the backlog stay connected. Any
connection cowork maintains itself means credentials for repositories inside the backend,
commit noise on every state change, conflict resolution between a body edited in cowork and
in a file, and audit and deletion rules ([ADR 0024](0024-deletion-tickets-are-soft-deleted-and-purged-projects-archived-people-deactivated-tenants-deleted-explicitly.md),
[ADR 0026](0026-one-append-only-audit-table-written-by-the-request-layer.md)) that would
have to reckon with a git history. Nothing leaves cowork on its own ([ADR 0020](0020-notifications-are-an-in-app-inbox-per-person.md)
D5); the export already produces the files in the same grammar on request
([ADR 0051](0051-import-is-a-server-side-two-phase-atomic-job-export-is-its-mirror.md) D4).
What a repository does with its `docs/tickets/` after an import is that repository's
decision.

## Decision

**D1 — After an import, cowork is the source.** Import brings files in; export writes files
out on request; neither watches the other. cowork holds no repository credential, opens no
repository, makes no commit.

**D2 — No synchronisation of any direction is built,** not two-way and not a periodic
mirror. An installation that wants the export in a repository schedules it outside cowork
with a read token, as the backup record has it ([ADR 0059](0059-backups-belong-to-the-operators-cowork-provides-the-export-and-makes-a-restores-inconsistency-visible.md)
D2), and commits it with its own tooling.

**D3 — A repeated import of the same files into the same project is a duplicate, not an
update.** The dry run reports every file whose number already exists in the project as a
conflict; execution refuses while any conflict remains. The importer updates nothing
([ADR 0051](0051-import-is-a-server-side-two-phase-atomic-job-export-is-its-mirror.md) D3's
atomicity stays simple).

**D4 — The documentation says the sentence.** The operations page ~~and the workflow plan~~
*(amended 2026-10-04: the workflow plan is consumed and deleted —
[ADR 0074](0074-the-question-catalog-is-consumed-phases-become-tickets-when-they-start-in-their-own-session.md)
D3 — and its copy of the sentence with it; the operations page of the import carries it when the
import is built)* state: after the import, cowork is the source; the files in the repository are history or
are removed — the repository decides. This repository's own `docs/tickets/README.md` carries
that note from the day its tickets are imported (phase 6, this repository first).

**D5 — An MCP convenience for writing the export into the working directory is not built;**
~~the `api` tool reaches `GET …/export` and a session may unpack it where it stands~~
([ADR 0042](0042-twelve-workflow-tools-and-one-escape-hatch.md)). A dedicated tool is an
amendment if sessions do it often. *(Found 2026-10-06, when the export was built: the `api` tool
answers a body as text cut at 100,000 bytes — `apiAnswer` in
[`tool_api.go`](../../backend/internal/tools/tool_api.go) —, so an archive does not reach a session
intact through it. A session that needs the export in its working directory runs
`cowork-mcp export <tenant>/<PROJECT> <dir>`, the subcommand of
[ADR 0070](0070-no-general-cli-the-mcp-binary-grows-workflow-subcommands.md) D2, as a command; the
rule of D5 stands.)*

## Consequences

- One truth, no conflict case, no git inside the backend.
- A person who keeps editing Markdown after an import edits history; D3 ensures a second
  import of those edits cannot overwrite the backlog.
- The export remains the only path back to files, and it is complete and lossless by
  ADR 0051 D5.
- The cut-over of a repository is: import, read the report, execute, then decide in that
  repository what becomes of the directory.

## Alternatives Considered

- **Two-way synchronisation.** Tickets stay visible in git; credentials in the backend,
  conflict resolution, commit noise, two grammars in step, deletion and audit against git
  history. A synchronisation product. Lost.
- **A one-way mirror written by cowork.** Readable history in git for free; credentials in
  the backend and commit noise for what a scheduled export outside cowork delivers without
  cowork knowing a repository. Lost.
- **A dedicated MCP export tool now.** Convenience the `api` tool already provides. Deferred
  by D5.

## Residual risks

- Someone importing a corrected file expects an update and gets a conflict (D3); the report
  says why and names the existing ticket, and the correction is made in cowork.

## References

- [ADR 0051](0051-import-is-a-server-side-two-phase-atomic-job-export-is-its-mirror.md) — import and export
- [ADR 0063](0063-the-importer-takes-whatever-the-user-hands-it-open-and-archived-tickets-alike.md) — what the importer handles
- [ADR 0059](0059-backups-belong-to-the-operators-cowork-provides-the-export-and-makes-a-restores-inconsistency-visible.md) D2 — the scheduled export outside cowork
- [ADR 0020](0020-notifications-are-an-in-app-inbox-per-person.md) D5 — nothing leaves cowork on its own
