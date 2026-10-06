# The Markdown grammar, v1

`GET …/tickets/{number}/markdown` answers a ticket as one Markdown document: the canonical
ticket and nothing else ([ADR 0044] D1), in the shape of the ticket files cowork replaces
([ADR 0011] D4). This page is grammar v1 exactly as
[`markdown.Render`](../../backend/internal/markdown/markdown.go) writes it; the golden files in
[`internal/markdown/testdata/`](../../backend/internal/markdown/testdata/) are its examples.
Read against the tree on 2026-10-05. Turning the Markdown people write into the HTML a browser shows
is another package and another page, [rendered-markdown.md](rendered-markdown.md).

The links are not in it: `/context` shows them, and the project export will write them once each
in a links manifest beside the tickets ([ADR 0051] D4, not built yet).

## The route

[`ExportTicket`](../../backend/internal/api/export.go) reads the ticket through the visibility
predicate, gathers the document in the same read transaction, records `exported` (with the
format `markdown v1` and the ticket's version) in a `Mutate` of its own, and answers
`text/markdown; charset=utf-8` with the ticket's `ETag`. It never answers `304`: every call is
data leaving the system and is recorded ([ADR 0044] D5, [ADR 0026] D5). The act is on neither
the activity list nor the event stream. `Render` is a pure function: the same ticket renders the
same bytes.

## The document

```
---
<frontmatter>
---

<body>

## Open questions

### Q<n>: <question>

<options>

**Recommendation:** <recommendation>

**Answer:** <answer>
```

**Frontmatter.** One `key: value` line per key, in this order; a key whose value is empty is
left out.

| Key | Value |
|---|---|
| `key` | the full key, `<tenant>/<PROJECT>-<n>` |
| `title` | the title |
| `type`, `state`, `severity`, `security` | the vocabulary values |
| `threat` | the threat; only when `security` is not `none` |
| `horizon` | the ticket's horizon: the one set on it, else `later`. The key was `urgency` until 2026-10-05, when the API took the word ([ADR 0010] D1); grammar v1 was amended in place, since nothing parses the export yet, and the importer reads `urgency` as `horizon` — an export written before and the ticket files of a repository name it so ([ADR 0044] D3) |
| `effort` | the effort |
| `progress-refinement` | always present: the refinement stage as the ticket shows it — derived while there are children, else the ticket's own ([ADR 0017] D2, D3) |
| `progress` | always present: the implementation stage, likewise |
| `progress-review` | always present: the review stage, likewise |
| `assignee` | the assignee as `Name <identity>`, see **Persons** below |
| `parent` | the parent's full key, when the reader can see the parent |
| `opened`, `decided`, `done` | dates, `YYYY-MM-DD` in UTC; `opened` always, the others when set |
| `shipped` | state `done`: the note of the last transition to done |
| `dropped-reason` | state `dropped`: the reason of the last transition to dropped |
| `blocked-by`, `blocked-reason`, `blocked-from` | state `blocked`: the block's kind, its text, the state it came from |
| `attachments` | when there are any: a list, one `  - <file name>` line each, in upload order |

**Values.** The three stages and the dates are written as they are. Every other value is written plain
when YAML reads it back as the same string — it matches `^[A-Za-z0-9][A-Za-z0-9 _./()+-]*$`, is
not `true`, `false`, `yes`, `no`, `on`, `off`, `null`, `~` in any case, does not start with a
digit followed only by digits and `._:+-`, and does not end in a space — and otherwise as a
double-quoted JSON string, which YAML reads the same (`scalar`). So `title: "No"` is quoted and
`title: Fix it` is not.

**Persons.** A person is written the way git writes an author, `Name <identity>` (`person`,
[ADR 0044] D1): the display name, then the identity in angle brackets — `local:<username>` for a
local account, `oidc:<issuer>#<subject>` for a person of the identity provider, the issuer ending
at the first `#`. The name loses `<` and `>`, so the first `<` starts the identity; a person
with neither identity, whom no route makes, is written by name alone. With an identity the value
has a `<` and is therefore double-quoted: `assignee: "Ada Lovelace <local:ada>"`.
[`exportAssignee`](../../backend/internal/api/export.go) reads the identity with the query
`ExportPerson` ([`export.sql`](../../backend/internal/store/queries/read/export.sql)), only when
the ticket row carries the display name, under the same read policy of `users` in the same
transaction: the document writes both or neither. The transaction is `READ COMMITTED`, so a
person whose membership is removed between the ticket row and that read is no longer readable;
the query then finds no row and the document writes neither, rather than failing
(`TestExportAssignee`).

**Body.** The body with surrounding whitespace trimmed, after a blank line; nothing when it is
empty.

**Questions.** `## Open questions` always follows, even without questions and even when the body
has a heading of the same name. Then every question of the ticket in number order, answered and
withdrawn ones included:

- `### Q<n>: <question>` with the question's whitespace collapsed to one line;
- the options, trimmed, as a paragraph, when there are any;
- `**Recommendation:** <text>`, when there is one;
- `**Answer:** ` followed by the trimmed answer, `_withdrawn_` or `_open_`.

Each part is preceded by a blank line.

## Example

[`testdata/blocked.md`](../../backend/internal/markdown/testdata/blocked.md):

```markdown
---
key: acme/VKO-13
title: Wait for the release
type: task
state: blocked
severity: low
security: none
horizon: release
effort: S
progress-refinement: 100
progress: 0
progress-review: 0
opened: 2026-10-01
blocked-by: release
blocked-reason: needs 2.0 out
blocked-from: review
---

## Open questions
```

`every-key.md` shows an open ticket with a threat, an assignee of the identity provider, a parent,
attachments and a body (`context-full.md` an assignee with a local account);
`questions.md` the three answer forms and a body with its own `## Open questions` heading;
`done.md` and `dropped.md` the notes of the terminal states.

## The context

`GET …/tickets/{number}/context` is the ticket for reading ([ADR 0044] D2):
[`markdown.RenderContext`](../../backend/internal/markdown/context.go), gathered by
[`ExportTicketContext`](../../backend/internal/api/context.go) in one read transaction from the
queries of the lists, recorded as `exported` with the format `context v1`, `text/markdown;
charset=utf-8` without an `ETag`. In order:

1. One line, `<!-- cowork: context of <key>, exported <RFC 3339 UTC> by <person> (via <agent>) —
   not an import format -->`; with it the document does not start with frontmatter, so it is no
   file of grammar v1. A request of a plain token says `(through the token <name>)` instead of
   `(via <agent>)`, and so does every line below where a token and no agent made the act
   (`through the token <name>`, `through a token` where the act did not record the name;
   [ADR 0036] D6, `via` in `context.go`); a person's own act says neither.
2. The canonical document, exactly as `Render` writes it.
3. `## Links`: `- <name read from this ticket> <key> — <title> (<state>, <assignee>)`.
4. `## Prerequisites`: `<open> of <all> open.`, then the tree of the tickets that block it, two
   spaces of indent per level, `- <key> — <title> (<state>, <assignee>, <implementation stage>%)`;
   the tree of `…/prerequisites` ([domain.md](domain.md#the-prerequisite-tree)) — eight levels at
   most, stopping at a ticket the reader cannot see — with each prerequisite once: under the first
   ticket it blocks, not repeated under the others.
5. `## Recent comments` — left out for `comments=0` —: the last ones, oldest of them first, each
   `**<author>** via <agent>, <time UTC>:` and its text as a block quote, or `[withdrawn]`.
6. `## Attachments`: `- <name> — <type>, <size> — <URL>`.
7. `## Recent activity` — left out for `activity=0` —: the last acts of the ticket's activity
   list, which leaves out time entries and what took data out (the exports among them), each
   `- <time UTC> — <actor> via <agent> — <action>`, with the states of a transition, the ends of a link, the fields of an update,
   `by score` after the sort of the project's rank that moved the ticket, an act on the horizon —
   `overridden` in the record — as `set the horizon to <value>` or `returned the ticket to later`
   (`horizonAct`), the reason and the note quoted and cut to 200 characters; an act that names a ticket the reader
   cannot see says so instead.

An empty section says `None.`. A document lists at most 200 links, 200 nodes of the tree and 200
attachments (`maxContext*` in `context.go`) and does not say when it stops at one.
`context-full.md` and `context-quiet.md` in `testdata/` are its golden files, `TestRenderContext`
their test; `context-token.md` and `TestRenderContextNamesTheTokenOfAPersonsAct` hold the token's
lines.

## Changing the grammar

The golden files are the specification's test: `TestRender` in
[`markdown_test.go`](../../backend/internal/markdown/markdown_test.go) compares `Render` with
them, `TestScalar` pins the quoting and `TestPerson` the form of a person. After a deliberate
change, `cd backend && go test ./internal/markdown -update` rewrites them; the diff of the golden
files is what a reviewer reads. The importer will read this form back ([ADR 0044] D3), so a
change of the grammar is a change of a contract. The question form and the state notes stay as
they are until the importer of phase 6 has read this repository's tickets with them ([ADR 0044]
D1): what it cannot map is the evidence for a change.

[ADR 0011]: ../adr/0011-a-ticket-is-a-markdown-body-plus-first-class-open-questions.md
[ADR 0010]: ../adr/0010-the-frontmatter-vocabularies-become-ticket-columns.md
[ADR 0017]: ../adr/0017-effort-is-a-size-progress-is-a-five-step-percentage-and-time-is-booked-by-people.md
[ADR 0026]: ../adr/0026-one-append-only-audit-table-written-by-the-request-layer.md
[ADR 0036]: ../adr/0036-a-token-acts-as-its-person-an-agent-flag-is-the-floor-the-agent-header-only-narrows.md
[ADR 0044]: ../adr/0044-two-endpoints-markdown-is-the-canonical-ticket-context-is-the-ticket-with-what-surrounds-it.md
[ADR 0051]: ../adr/0051-import-is-a-server-side-two-phase-atomic-job-export-is-its-mirror.md
