# ADR 0044: Two Endpoints — `/markdown` Is the Canonical Ticket for Import and Export, `/context` Is the Ticket With What Surrounds It, for Reading

## Status

Accepted. Date: 2026-10-01. Decided by the owner as the answer to the catalog question
"context delivery to the LLM?": two endpoints, over one document with a read-only marker
(the recommendation), over JSON, and over a token-budgeted document. The rules of D4–D6 were
put to the owner with the question and not objected to.

Amended 2026-10-02 (D1: the key list of grammar v1; D6: the key that lists the attachments).
D1 named no key for the transition's note or reason, for the block, or for the attachments;
the first implementation spells them as the tickets page of this repository does, a v1 that
is reviewed after experience.

**Partly built** (phase 2, 2026-10-02): D1, D5 and D6 for `/markdown`
([`internal/markdown`](../../backend/internal/markdown/), golden files in its `testdata/`); every
call is recorded, and in phase 2 every caller is a token. D2's `/context` and D4 arrive with
the MCP server, D3 with the importer of [ADR 0051](0051-import-is-a-server-side-two-phase-atomic-job-export-is-its-mirror.md).

## Context

[ADR 0011](0011-a-ticket-is-a-markdown-body-plus-first-class-open-questions.md) D4 fixed the
Markdown form of a ticket — frontmatter from the columns, the body, the open questions in a
fixed order — as the one grammar the importer reads and the export writes. An LLM reading a
ticket needs more than the ticket: its links, the tree of what must be done before it can
close ([ADR 0012](0012-four-typed-directed-links-within-a-tenant.md) D6), the last comments,
the attachments, the recent activity. None of those are fields of the ticket; they are other
entities pointing at it, and an import that read them back would duplicate them. The owner
chose to keep the two documents apart by URL rather than by a marker inside one.

## Decision

**D1 — `GET …/tickets/{number}/markdown` returns the canonical ticket and nothing else:**
the frontmatter rendered from the columns (`key`, `title`, `type`, `state`, `severity`,
`security`, `threat`, `urgency`, `effort`, `progress`, `assignee`, `parent`, `opened`,
`decided`, `done`, and the transition note or reason where the state has one), the body,
and `## Open questions` with their `**Answer:**` lines, in the fixed order of ADR 0011 D4.
It is what the importer reads and what a repository file looks like; a round trip through it
is lossless. *(Made concrete 2026-10-02, grammar v1:)* the keys in that order, an absent value
omitted — `threat` only when `security` is not `none`, `urgency` the effective value,
`assignee` the display name, `parent` the full key, dates as UTC dates — then the state's note:
`shipped` (the verification note of the `done` act), `dropped-reason`, and for `blocked` the
keys `blocked-by` (the kind), `blocked-reason` and `blocked-from`; then `attachments` (D6).
Strings are written plain when YAML reads them back unchanged, otherwise double-quoted. After
the body, `## Open questions` is always written and is the last heading of that name;
`### Q<n>: …` in number order, the options verbatim, `**Recommendation:** …` when there is
one, and `**Answer:**` with the answer, `_open_` or `_withdrawn_`. The response is
`text/markdown; charset=utf-8` with the ticket's `ETag` and is never answered `304`.

**D2 — `GET …/tickets/{number}/context` returns the ticket for reading:** the whole of D1,
followed by read-only sections in a fixed order — `## Links` (typed, with the reverse view
and each target's key, state and assignee), `## Prerequisites` (the tree, state, assignee and
progress per node, the count of open ones), `## Recent comments` (the last `comments`,
default 10, with actor, agent mark and time; a withdrawn comment as `[withdrawn]` without
text), `## Attachments` (name, type, size, URL; never content), `## Recent activity` (the
last `activity`, default 10). `comments=0` or `activity=0` omits a section. The document
starts with one line `<!-- cowork: context of <key>, exported <time> by <person> (via
<agent>) — not an import format -->`.

**D3 — The importer reads D1's form only.** A file that carries D2's sections is refused
with the line where the first read-only section starts, so a context export is never
imported by mistake.

**D4 — `get_ticket` of the MCP server calls `/context`;** `session_start` calls it for the
active ticket with `comments=5&activity=10` ([ADR 0042](0042-twelve-workflow-tools-and-one-escape-hatch.md)).
A session that wants the canonical file calls `/markdown` through `api`.

**D5 — Both exports through a token are recorded** ([ADR 0026](0026-one-append-only-audit-table-written-by-the-request-layer.md)
D5): data left the system either way.

**D6 — Attachments appear as metadata and a URL in both documents' scope: never as
content** ([ADR 0016](0016-attachments-live-in-s3-compatible-storage-and-are-served-only-through-the-backend.md)
D5); D1 lists them in the frontmatter as names only, D2 with type, size and URL. *(Made
concrete 2026-10-02: D1's key is `attachments`, a list of the names in upload order.)*

## Consequences

- Two formats, each with one job: `/markdown` is a file, `/context` is a reading. The
  importer's grammar stays exactly ADR 0011 D4 and gains a refusal, not a marker to skip.
- An LLM session reads one document (`/context`) per ticket; nothing is lost against the
  one-document variant except that a context export cannot be re-imported — which D3 makes
  an explicit refusal rather than a silent skip.
- Two routes to document and test; the context sections are rendered from the same queries
  the UI's detail page uses, so they cannot drift from it.
- A token-budgeted variant (`?budget=`) is an amendment to `/context` alone, if context
  windows ever press.

## Alternatives Considered

- **One document, two zones separated by a marker** — the recommendation: one call, one
  format, the importer skips below the marker. The owner preferred two URLs over a convention
  inside a document. Lost.
- **JSON for the LLM.** Precise; more tokens than Markdown for the same content, and the body
  is Markdown anyway. Lost.
- **A token budget that trims sections from the bottom.** Useful when windows press; a
  heuristic count today. Deferred as an amendment to D2.

## Residual risks

- D1's frontmatter now carries `assignee` and `parent`; an import maps them by name and key
  and reports what it could not resolve, as the import record will say.
- Two documents mean two caches and two ETags for one ticket; the API record treats them as
  two resources.

## References

- [ADR 0011](0011-a-ticket-is-a-markdown-body-plus-first-class-open-questions.md) D4 — the canonical grammar `/markdown` keeps
- [ADR 0012](0012-four-typed-directed-links-within-a-tenant.md) D6, [ADR 0015](0015-comments-are-a-thread-and-activity-is-a-separate-list.md), [ADR 0016](0016-attachments-live-in-s3-compatible-storage-and-are-served-only-through-the-backend.md) D5 — what `/context` adds and what it never includes
- [ADR 0042](0042-twelve-workflow-tools-and-one-escape-hatch.md) — the tools that call each
- [ADR 0026](0026-one-append-only-audit-table-written-by-the-request-layer.md) D5 — exports are recorded
