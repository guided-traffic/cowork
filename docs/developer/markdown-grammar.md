# The Markdown grammar, v1

`GET …/tickets/{number}/markdown` answers a ticket as one Markdown document: the canonical
ticket and nothing else ([ADR 0044] D1), in the shape of the ticket files cowork replaces
([ADR 0011] D4). This page is grammar v1 exactly as
[`markdown.Render`](../../backend/internal/markdown/markdown.go) writes it; the golden files in
[`internal/markdown/testdata/`](../../backend/internal/markdown/testdata/) are its examples.
Read against the tree on 2026-10-03.

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
| `urgency` | the urgency the ticket shows: the override when one stands, else the derived value |
| `effort` | the effort |
| `progress-refinement` | always present: the refinement stage as the ticket shows it — derived while there are children, else the ticket's own ([ADR 0017] D2, D3) |
| `progress` | always present: the implementation stage, likewise |
| `progress-review` | always present: the review stage, likewise |
| `assignee` | the assignee's display name |
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
urgency: release
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

`every-key.md` shows an open ticket with a threat, an assignee, a parent, attachments and a body;
`questions.md` the three answer forms and a body with its own `## Open questions` heading;
`done.md` and `dropped.md` the notes of the terminal states.

## Changing the grammar

The golden files are the specification's test: `TestRender` in
[`markdown_test.go`](../../backend/internal/markdown/markdown_test.go) compares `Render` with
them and `TestScalar` pins the quoting. After a deliberate change, `cd backend && go test
./internal/markdown -update` rewrites them; the diff of the golden files is what a reviewer
reads. The importer will read this form back ([ADR 0044] D3), so a change of the grammar is a
change of a contract.

[ADR 0011]: ../adr/0011-a-ticket-is-a-markdown-body-plus-first-class-open-questions.md
[ADR 0017]: ../adr/0017-effort-is-a-size-progress-is-a-five-step-percentage-and-time-is-booked-by-people.md
[ADR 0026]: ../adr/0026-one-append-only-audit-table-written-by-the-request-layer.md
[ADR 0044]: ../adr/0044-two-endpoints-markdown-is-the-canonical-ticket-context-is-the-ticket-with-what-surrounds-it.md
