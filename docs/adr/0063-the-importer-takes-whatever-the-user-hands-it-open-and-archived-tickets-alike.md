# ADR 0063: The Importer Takes Whatever the User Hands It — Open and Archived Tickets Alike, With the Frontmatter Mapping Fixed and the Selection Left to the Person Importing

## Status

Accepted. Date: 2026-10-01. Decided by the owner as the answer to the catalog questions on
what is imported and how it is mapped: cowork decides neither which files a user imports nor
into which tenant and project; it decides what the importer can handle and how each field is
mapped. The owner expects to import most repositories' tickets including their archives, and
holds that the choice belongs to the person importing, not to this project. The mapping of
D3 follows the earlier records; D4–D6 were proposed with the question and not objected to.

**Not built.** No importer.

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

**D4 — A file without frontmatter is not refused; it is reported for a decision.** The
multi-item archive records become one ticket each, type `task`, state `done` with the import
note of D2, their numbered items kept as body text; the report says so per file, and the
person may exclude the file or accept it.

**D5 — Files that are not tickets are skipped and listed.** `README.md`, notes, anything
not matching `NNN-<slug>.md` or `local_NNN-<slug>.md`, and any `/context` document
([ADR 0044](0044-two-endpoints-markdown-is-the-canonical-ticket-context-is-the-ticket-with-what-surrounds-it.md)
D3) appear in the report as skipped with the reason.

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
