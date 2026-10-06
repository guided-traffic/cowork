# ADR 0051: Import Is a Server-Side, Two-Phase, Atomic Job With a Correctable Report; Export Is Its Mirror; No Generic Batch API

## Status

Accepted. Date: 2026-10-01. Decided by the owner as the answer to the catalog question "bulk
and import?": a server-side import job with dry run and an export endpoint that mirrors it,
over the importer as a client of the ordinary routes, over a generic batch API, and over the
import job without the export. The rules of D6–D9 were put to the owner with the question
and not objected to.

Amended 2026-10-06 by the owner (D4: the links manifest), answering whether the links enter the
export: into the project export only, each link once, over `/markdown`'s frontmatter, which
would write a link at both its ends and change what one ticket's document is
([ADR 0044](0044-two-endpoints-markdown-is-the-canonical-ticket-context-is-the-ticket-with-what-surrounds-it.md)
D1), and over an export without them, which would lose the graph of a backup restored through
the importer ([ADR 0059](0059-backups-belong-to-the-operators-cowork-provides-the-export-and-makes-a-restores-inconsistency-visible.md)
D2). The manifest is built with the export.

**Not built.** No import, no export, no job entity.

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
actor), and answers with it.

**D2 — Two phases, the first a dry run.** `dry_run=true` parses everything and produces the
report: per file the detected type, the state, the columns, the questions, the links
resolved to target keys inside the archive, warnings (`blocked-by: human` as a candidate for
`blocked`; a `filed-from` that names an event; a link to a key outside the archive, which is
omitted), and errors (an out-of-vocabulary value, a malformed frontmatter, a `/context`
document). `dry_run=false` with the id of a dry run executes it, with optional corrections
per file (type, state, assignee) — the correctable report of ADR 0008 D5.

**D3 — Execution is atomic per job.** All tickets, questions, links and the sequence advance
in one transaction ([ADR 0027](0027-data-access-is-sqlc-over-pgx-behind-a-tenant-transaction-and-a-mutation-wrapper.md)
D3) or none; the numbers of ADR 0007 D6 are kept; every created row has an audit entry with
the actor and the `import_job_id`; each ticket records `imported_from` (file name, job id).
A dry run is executed at most once; a second execution of the same dry run answers `409`.

**D4 — Export is the mirror:** `GET /api/v1/tenants/{slug}/projects/{KEY}/export` returns an
archive with every ticket of the project as its `/markdown` document, named by key, plus an
attachment manifest (names, types, sizes, URLs — never bytes), *(amended 2026-10-06 by the
owner:)* a links manifest beside the tickets — every link with an end in the project, each once,
by its source key, its type and its target key, since `/markdown` carries no links (ADR 0044
D1) — and a `manifest.json` with the
project, the export time and the exporter. A tenant export is the union over its projects
(the backup record's second line), each link in it once. The importer reads the links manifest
with the tickets (D2, D9), so a restore through it keeps the graph.

**D5 — The round trip is the proof.** The integration tier exports a project, imports it into
an empty project, exports again and asserts the two archives are equal up to keys and times;
that test is what keeps the grammar of ADR 0044 D1 honest.

**D6 — Import is an administrator's act and never an agent's.** Role `admin` in the tenant;
a flagged token ([ADR 0043](0043-agent-capabilities-are-chosen-per-token-the-default-is-everything-reversible-and-attributable.md)
D3) is refused. Export follows the project's read permission.

**D7 — Sizes and validity.** `COWORK_MAX_IMPORT_BYTES` (default 50 MiB, `0` disables,
[ADR 0039](0039-no-request-budgets-size-and-time-limits-instead-configurable-and-switchable.md)
D2) bounds the archive; a dry run's report is valid for twenty-four hours and then expires.

**D8 — No generic batch API.** No `POST …/tickets:batch`, no transactional operation list.
A bulk need beyond import is a question of its own.

**D9 — Links that leave the archive are warnings, not guesses.** A `blocks` or `found-in`
whose target key is not in the archive and not already in the project is reported and
omitted; the person adds it after the import if it belongs.

## Consequences

- The importer is server code with tests, not a script; the parser is shared with the
  export; atomicity and numbers are the server's.
- One job entity, two routes, an archive handler bounded by D7; the UI gets an import page
  that shows the report with the correction fields and an export button on the project.
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

- D3's single transaction grows with the archive; fifty megabytes of Markdown is thousands
  of tickets and still seconds. A tenant-scale import is split by project.
- D2's type detection is heuristic by design; the report and the corrections are the
  guard, and an import executed without reading the report mis-types some tickets.

## References

- [ADR 0044](0044-two-endpoints-markdown-is-the-canonical-ticket-context-is-the-ticket-with-what-surrounds-it.md) D1, D3 — the grammar imported and exported
- [ADR 0007](0007-a-ticket-key-is-globally-unique-tenant-slash-project-dash-number.md) D6, [ADR 0008](0008-five-ticket-types-and-an-optional-parent-in-the-same-project.md) D5, [ADR 0009](0009-ticket-states-are-the-frontmatter-states-plus-blocked.md), [ADR 0010](0010-the-frontmatter-vocabularies-become-ticket-columns.md) D5, [ADR 0012](0012-four-typed-directed-links-within-a-tenant.md) — what the import must do
- [ADR 0043](0043-agent-capabilities-are-chosen-per-token-the-default-is-everything-reversible-and-attributable.md) D3, [ADR 0034](0034-three-tenant-roles-an-optional-project-restriction-no-implicit-role-for-the-global-administrator.md) — who may import
- [ADR 0059](0059-backups-belong-to-the-operators-cowork-provides-the-export-and-makes-a-restores-inconsistency-visible.md) D2 — the backup's second line, whose graph the links manifest keeps
- [docs/planning/project-plan.md](../planning/project-plan.md) — phase 6, the cut-over
