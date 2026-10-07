# ADR 0063: The Importer Takes Whatever the User Hands It — Open and Archived Tickets Alike, With the Frontmatter Mapping Fixed and the Selection Left to the Person Importing

## Status

Accepted. Date: 2026-10-01. Decided by the owner as the answer to the catalog questions on
what is imported and how it is mapped: cowork decides neither which files a user imports nor
into which tenant and project; it decides what the importer can handle and how each field is
mapped. The owner expects to import most repositories' tickets including their archives, and
holds that the choice belongs to the person importing, not to this project. The mapping of
D3 follows the earlier records; D4–D6 were proposed with the question and not objected to.

Made concrete 2026-10-06 by the implementer where the record left the detail open, each marked
in place and open to the owner's objection: a dropped ticket's reason where its source has none
and how a done one is done (D2); the keys D3's table does not name, what an unfixed finding is,
and the forms of the questions and the mentions (D3); the columns of a record without frontmatter
(D4); and that a `/context` document is an error, not a skipped file, as the records it cites have
it (D5).

~~**Not built.** No importer.~~ **Built** (phase 6, 2026-10-06, in the API; the UI's import page
outstanding): D1–D5 — [`internal/importer`](../../backend/internal/importer/) reads a repository's
ticket files and the documents of an export, maps them as D3 says and reports every file of the
upload with its outcome; the person corrects or excludes a file at the execution
([ADR 0051](0051-import-is-a-server-side-two-phase-atomic-job-export-is-its-mirror.md) D2). The
fixtures of the Consequences are copies of this repository's ticket files in
[`testdata/tickets`](../../backend/internal/importer/testdata/tickets/), and the integration tier
reads this repository's whole `docs/tickets/` without an error
(`TestImportThisRepositorysTickets`). The mapping in full:
[docs/developer/import-and-export.md](../developer/import-and-export.md).

## Context

[ADR 0051](0051-import-is-a-server-side-two-phase-atomic-job-export-is-its-mirror.md) made
the import a server-side job with a dry run and a correctable report;
[ADR 0044](0044-two-endpoints-markdown-is-the-canonical-ticket-context-is-the-ticket-with-what-surrounds-it.md)
D1 fixed the grammar. The Markdown backlogs cowork will absorb hold open tickets with
frontmatter, archived tickets with frontmatter, and a few archived records without any — the
multi-item analyses that predate the one-file-per-ticket rule. The recommendation had been to
import open tickets only and to leave the archive in git as history; the owner ruled that
what to import is the user's discretion at each import, and that the tool must therefore
handle every case the directories contain.

## Decision

**D1 — cowork has no policy on what is imported.** The person importing chooses the files —
a whole `docs/tickets/` with its `archive/`, the open tickets only, a single file — and the
target tenant and project. The dry-run report lists every file with its proposed outcome, and
the person may exclude or correct any file before execution ([ADR 0051](0051-import-is-a-server-side-two-phase-atomic-job-export-is-its-mirror.md)
D2).

**D2 — The importer handles open and archived tickets alike.** A ticket whose frontmatter
says `done` or `dropped` is imported in that state; its `done:` or `dropped-reason:` becomes
the transition's data, and a `done` without a `shipped:` or verification line receives the
note `imported from archive; the source carried no verification note` so that
[ADR 0009](0009-ticket-states-are-the-frontmatter-states-plus-blocked.md) D5 holds without
inventing a verification. Archived tickets keep their numbers like open ones
([ADR 0007](0007-a-ticket-key-is-globally-unique-tenant-slash-project-dash-number.md) D6).
*(Made concrete 2026-10-06 by the implementer, open to the owner's objection:)* a `dropped`
without a `dropped-reason:` receives the reason `imported from archive; the source carried no
reason`, since a drop needs one (ADR 0009 D4). A `done` ticket is done from `in-progress`, by its
stages when all three are full and it has no children, by hand otherwise (ADR 0009 D5); `opened:`,
`decided:` and `done:` are its dates, and a ticket without `opened:` is opened at the execution.

**D3 — The mapping of a file, fixed by the earlier records:**

| Source | Target |
|---|---|
| `NNN` in the file name, `id: T<n>` | the ticket number; the sequence advances past the highest imported |
| `title` | title |
| `state` | the state ([ADR 0009](0009-ticket-states-are-the-frontmatter-states-plus-blocked.md) D1); `blocked` is never inferred, `blocked-by:` values are reported as candidates |
| `severity`, `security`, `threat`, `urgency`, `effort`, `opened` | the columns of [ADR 0010](0010-the-frontmatter-vocabularies-become-ticket-columns.md); a value outside the vocabulary is an error in the report, not a guess |
| `decided`, `done`, `shipped`, `dropped-reason` | the transitions' timestamps, note and reason (ADR 0009 D6) |
| `blocked-by: T<n>` | a `blocks` link from that ticket; `filed-from: T<n>` a `found-in` link ([ADR 0012](0012-four-typed-directed-links-within-a-tenant.md)); either naming a ticket outside the import and not already in the project becomes a text line under `## Related` with the source path, never a silent drop |
| `publication-accepted`, `local_` prefix, `security: live\|boundary` with an unfixed finding | the confidentiality flag (its own record) |
| the body sections | the body ([ADR 0011](0011-a-ticket-is-a-markdown-body-plus-first-class-open-questions.md) D1) |
| `## Open questions` with `### Q<n>` and `**Answer:**` | question entities, `answered` when the line is filled, `open` when it reads `_open_` (ADR 0011 D2, D4) |
| `T<n>` mentions in the body | rewritten to the full key of the imported ticket when it is in the import, left as text otherwise |
| type | detected by content and reported for correction ([ADR 0008](0008-five-ticket-types-and-an-optional-parent-in-the-same-project.md) D5) |

*(Made concrete 2026-10-06 by the implementer, open to the owner's objection:)* beside the table,
the importer reads `urgency` as `horizon`
([ADR 0044](0044-two-endpoints-markdown-is-the-canonical-ticket-context-is-the-ticket-with-what-surrounds-it.md)
D3), the three progress stages, `parent`, and an `assignee` by its identity only (ADR 0044 D1); a
key it does not know is a warning and not read, a key named twice an error. A `blocked-by:` beside a
state other than `blocked` that names a ticket is the `blocks` link of the table; one that names a
block kind, or an ADR (`adr-NNNN`, a decision), is the candidate the report names, and the body
keeps it as a line under `## Related` — nothing is dropped silently. A `filed-from:` that names an
event rather than a ticket is such a line too (ADR 0051 D2). An unfixed finding is one without a
`shipped:` line ([ADR 0065](0065-a-confidential-flag-replaces-the-file-name-embargo-set-automatically-lifted-only-by-a-person.md)
D7). The questions are the last `## Open questions` heading outside fenced code, up to the next
heading of its level; `_withdrawn_` is a withdrawn question, and a question without an answer line,
or with an empty one, is open with a warning. A `T<n>` is rewritten outside fenced code and code
spans, in the body and the questions of a repository's file; an export's document names full keys
and is left as it is. The type is detected from the title — a question mark, a `live` or
`boundary` finding, the words of a decision, a defect or a missing capability, in that order, else
a task — and the report names the rule that matched.

**D4 — A file without frontmatter is not refused; it is reported for a decision.** The
multi-item archive records become one ticket each, type `task`, state `done` with the import
note of D2, their numbered items kept as body text; the report says so per file, and the
person may exclude the file or accept it. *(Made concrete 2026-10-06 by the implementer, open to
the owner's objection:)* such a ticket has the severity `low`, the security class `none` and the
effort `S`, the number of its file name, and as its title the file's first `# ` heading, or its
name without the number.

**D5 — Files that are not tickets are skipped and listed.** `README.md`, notes, anything
not matching `NNN-<slug>.md` or `local_NNN-<slug>.md`, ~~and any `/context` document
([ADR 0044](0044-two-endpoints-markdown-is-the-canonical-ticket-context-is-the-ticket-with-what-surrounds-it.md)
D3)~~ appear in the report as skipped with the reason. *(Made concrete 2026-10-06 by the
implementer, open to the owner's objection:)* a `/context` document is not skipped but reported
as an error with the line where its first read-only section starts, as ADR 0044 D3 ("refused")
and ADR 0051 D2 (an error) have it: the execution waits until the person excludes it, so a
context export is never passed over unseen. An export's document, `<PROJECT>-<n>.md`, is a
ticket file too (ADR 0051 D4); a file that is no Markdown, and the export's `attachments.json`, are
skipped with their reason, and its `manifest.json` and `links.json` are read beside the tickets.

**D6 — Several repositories of one tenant import into several projects.** Keys do not collide
because the project is the namespace (ADR 0007 D1); the person chooses the project per
import, and the tenant and project of any particular repository are not this record's to
name.

## Consequences

- An archive imported into cowork is searchable and linkable; an archive left in git stays
  history — both are the user's call, and the tool is indifferent.
- The importer's test fixtures contain all four shapes: an open ticket, an archived one with
  frontmatter, an archived record without, and a non-ticket file.
- D2's import note is honest: nobody reads a `done` from an import as verified work.
- Links into files the person chose not to import survive as text with a path; nothing is
  silently lost and nothing is invented.

## Alternatives Considered

- **Import open tickets only, archive stays in git, links into the archive become text
  references** — the recommendation. A backlog that shows work, not history. The owner
  ruled the selection to be the user's; the text-reference mechanism is kept in D3 for
  whatever the user leaves out.
- **Import the archive only when an open ticket links to it.** Conditional logic for a choice
  the person makes in the report anyway. Lost.
- **Refuse files without frontmatter.** Would make the owner's own oldest records
  unimportable. Lost to D4.

## Residual risks

- Archived texts imported as `done` appear in full-text results as current words; the state
  filter hides them by default ([ADR 0049](0049-filters-are-explicit-repeatable-query-parameters-no-query-language.md)
  D1, `include_terminal`), and a hit shows its state.
- D4's one-ticket-per-record flattening loses the per-item structure of the oldest archive
  files; the body keeps the text, and the person may split them later by hand.

## References

- [ADR 0051](0051-import-is-a-server-side-two-phase-atomic-job-export-is-its-mirror.md) — the job, the dry run, the report
- [ADR 0044](0044-two-endpoints-markdown-is-the-canonical-ticket-context-is-the-ticket-with-what-surrounds-it.md) D1, D3 — the grammar and the refused document
- [ADR 0007](0007-a-ticket-key-is-globally-unique-tenant-slash-project-dash-number.md) D6, [ADR 0008](0008-five-ticket-types-and-an-optional-parent-in-the-same-project.md) D5, [ADR 0009](0009-ticket-states-are-the-frontmatter-states-plus-blocked.md), [ADR 0010](0010-the-frontmatter-vocabularies-become-ticket-columns.md) D5, [ADR 0011](0011-a-ticket-is-a-markdown-body-plus-first-class-open-questions.md), [ADR 0012](0012-four-typed-directed-links-within-a-tenant.md) — the mapping's sources
- [docs/tickets/README.md](../tickets/README.md) — the source format
