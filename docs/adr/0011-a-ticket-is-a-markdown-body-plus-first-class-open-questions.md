# ADR 0011: A Ticket Is a Markdown Body Plus First-Class Open Questions

## Status

Accepted, amended 2026-09-29 (D6: images only from the attachment endpoint, since the owner chose
attachments for the first release in the answer to the catalog question "attachments?"; the rule
for embedding them is the one
[ADR 0016](0016-attachments-live-in-s3-compatible-storage-and-are-served-only-through-the-backend.md)
D7 proposed with that choice) and 2026-10-01 (D2: an agent may record and change the answer its
person gave in chat — the owner's decision in the answer to the catalog question "repository
binding?", whose context
[ADR 0066](0066-repositories-are-bound-by-their-normalised-remote-identity-creation-proposed-by-the-agent-confirmed-by-the-person.md)
records; amended 2026-10-06: the rest of that rule, who may change such an answer and what the
ticket shows, moved into D2 from that record, and the Consequences' line on the agent marked in
place, by the owner's rule that every amendment is made in place in the record it changes; no rule
changes) and 2026-10-04 (D2: the token a question was asked through and the one its answer was recorded through, by the owner's decision that every act made through a token is shown as such,
[ADR 0036](0036-a-token-acts-as-its-person-an-agent-flag-is-the-floor-the-agent-header-only-narrows.md) D6; built the same day,
[migration 27](../../backend/internal/store/migrations/000027_acts_through_a_token.up.sql)).
Date: 2026-09-29. Decided by the owner as the answer to the catalog question "enrichment:
body text versus structured parts?": the body stays prose, the open questions become
entities. The additional rules of D3–D6 were put to the owner with the question and were not
objected to.

**Partly built** (phase 2, 2026-10-02): D1, D2 and D5 — the body replaced as a whole with the
previous and the new text on the act, questions as entities (migration 10) with their number,
the person asked, the answer only a person decides and an agent may record with
`record-answer`; D4's export half in [ADR 0044](0044-two-endpoints-markdown-is-the-canonical-ticket-context-is-the-ticket-with-what-surrounds-it.md)'s
`/markdown`. D3's person-level list arrives with the person-level views, D4's import with the
importer, D6's sanitiser with the first rendered Markdown. *(2026-10-04.)* In the browser the
body is edited as Markdown and replaced as a whole over the version its editor began with
([ADR 0050](0050-optimistic-concurrency-a-version-per-entity-if-match-where-a-write-overwrites.md)
D3), and the asker edits an open question's text; the body is still shown as text.
*(2026-10-05.)* D6 built: the server renders the body, a comment, a question's options and its answer
— goldmark parses, the tree is rewritten, bluemonday holds the HTML to an allow-list
([`internal/richtext`](../../backend/internal/richtext/richtext.go)) —; raw HTML is shown as the text
it is, a link keeps an `http`, `https`, `mailto` or relative address and carries
`rel="noopener noreferrer nofollow"` — D6's `noopener` and two more —, an image shows only as a raster
attachment of the same ticket (ADR 0016 D7). The body is answered on `GET …/body`, beside the Markdown
elsewhere; the detail page shows it through Angular's own sanitiser. The library and the allow-list,
a test fixture as D6 asks, are recorded on [docs/security/rendered-markdown.md](../security/rendered-markdown.md),
as the residual risk below foresaw.

## Context

The owner's method runs on decisions: an analysis names the options, marks a recommendation,
and waits for an answer — one question at a time, in an `## Open questions` section with an
`**Answer:**` line. Across twenty repositories those lines are what the owner cannot see
without opening files, and what an agent must never fill in. Everything else in a ticket —
the current state, the required changes, what is not verified — is prose that a person and
an agent write equally well and that nobody filters.

The tickets will be written by agents and read by people of other tenants, so whatever is
rendered from them in a browser is foreign input.

## Decision

**D1 — The body of a ticket is one Markdown document.** Its sections are the convention of
the tickets page (current state, required changes, not verified, related), not a schema. The
body is the **current state and nothing else**: an update replaces it, never appends to it;
each replacement is a recorded act with a diff in the timeline.

**D2 — An open question is an entity of its own.** Fields: the question, the context and the
options (Markdown), the recommendation, the answer (Markdown), who asked, whom it is asked
of, who answered, when, `recorded_by_agent`, *(added 2026-10-04, [ADR 0036](0036-a-token-acts-as-its-person-an-agent-flag-is-the-floor-the-agent-header-only-narrows.md) D6)* the token the
question was asked through and the token the answer was recorded through — each its id and name,
none for a browser session; a changed answer records its own —, and a status `open`, `answered`,
`withdrawn`. A
question belongs to exactly one ticket. **Only a person decides an answer**; ~~an agent may
ask, may withdraw its own question, and may not touch the answer~~ *(amended 2026-10-01,
[ADR 0066](0066-repositories-are-bound-by-their-normalised-remote-identity-creation-proposed-by-the-agent-confirmed-by-the-person.md)
D8: an agent with the `record-answer` capability may record and update the answer its person
gave in chat; the actor is the person, the record says an agent wrote it down)* *(amended
2026-10-06, the rest of that rule moved here from ADR 0066 D8: an agent changes only an answer an
agent recorded, and only its own person's; a person may always change their own answer; the ticket
shows the answer as the person's, recorded by the agent)*. An agent may
ask and may withdraw its own question.

**D3 — The person-level list "open decisions" is a query over questions,** filtered by "asked
of me" or "open in my tenants", ordered by the ticket's urgency — not a search for a marker
in prose.

**D4 — Import and export are one grammar.** The Markdown representation of a ticket is the
frontmatter (the columns of [ADR 0010](0010-the-frontmatter-vocabularies-become-ticket-columns.md)
and the state of [ADR 0009](0009-ticket-states-are-the-frontmatter-states-plus-blocked.md)),
the body, then the questions rendered as `### Q<n>: …` subsections with `**Answer:**` lines,
in a fixed order. The importer reads that form; the export writes it; what an LLM is handed
is that export, so a ticket reads the same in cowork and in a repository.

**D5 — Answering a question sets its status; it does not close anything.** The extraction the
tickets page requires — the answer becomes an ADR, the question is removed from the
catalog — is work recorded in the ticket, not a side effect of the answer.

**D6 — Rendered Markdown is sanitised on the server.** No script, no inline event handlers,
no raw HTML from the body or an answer reaches another person's browser; links are rendered
with `rel="noopener"`; images are allowed ~~only from the installation's own origin until an
attachment mechanism exists~~ *(amended 2026-09-29)* only from the installation's own
attachment endpoint, for raster-image attachments of the same ticket
([ADR 0016](0016-attachments-live-in-s3-compatible-storage-and-are-served-only-through-the-backend.md)
D7). The sanitiser's allow-list is a test fixture, and a change to it is a change to the
security page of the UI.

## Consequences

- One table more than the plain-body variant, one form, and an export that has to merge two
  sources in a stable order.
- The owner's "one decision at a time" ritual has a home: an agent files a question, the
  owner answers it in the UI or confirms in chat ~~and the agent records nothing~~ *(amended
  2026-10-01 with D2, marked here 2026-10-06: and an agent with `record-answer` may write down
  what the owner confirmed)* — the answer is the owner's act, in the owner's name.
- Diffs of the body in the timeline make a rewritten current state reviewable without
  keeping history in the body itself.
- D6 costs a sanitiser dependency and a test; without it, ADR 0004's team product would
  ship a stored-XSS surface between tenants' people.

## Alternatives Considered

- **One Markdown body, sections by convention only.** Import and export trivial; the open
  decisions would be a full-text search for `**Answer:** _open_` with no assignee, no
  attribution and no timestamp — the core of the method invisible to the tool. Lost.
- **Fully structured tickets** (findings, changes and unverified items as entities). Maximal
  queryability; an agent would fill forms instead of writing an analysis, the import would
  chop prose into items heuristically, and the text a person reads would be an aggregate.
  Lost.

## Residual risks

- D4's fixed order is what makes a round trip lossless; a body that itself contains a
  `## Open questions` heading (an imported one, say) has to be recognised and moved into
  entities, or the export will carry the section twice. The importer reports that case.
- D6 names the mechanism, not the library; the choice is made when the UI is built and is
  recorded in the security page, not here.

## References

- [docs/tickets/README.md](../tickets/README.md) — the body skeleton and the question form the import reads
- [ADR 0009](0009-ticket-states-are-the-frontmatter-states-plus-blocked.md), [ADR 0010](0010-the-frontmatter-vocabularies-become-ticket-columns.md) — the frontmatter the export writes
- [ADR 0004](0004-cowork-is-a-team-product.md) D3 — attribution of the answer
