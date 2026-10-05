# ADR 0015: Comments Are a Flat Thread and Activity Is a Separate, Collapsible List — Both Rendered From the Record, an Act May Point at the Comment That Explains It

## Status

Accepted. Date: 2026-09-29. Decided by the owner as the answer to the catalog question
"comments and activity?": two views — a comment thread and a separate activity list — over
one merged timeline and over a threaded discussion tree. The rules of D3–D6 were put to the
owner with the question and were not objected to; D2's cross-reference is the adaptation
this record adds to keep an agent's act and its explanation findable across the two views.

Amended 2026-10-02 (D3: a comment's text never enters the audit record; D6: what the activity
withholds). A withdrawal has to hide the text from every route, and an append-only row cannot
forget it.

Amended 2026-10-04 by the owner's decision that every act made through a token is shown as such
([ADR 0036](0036-a-token-acts-as-its-person-an-agent-flag-is-the-floor-the-agent-header-only-narrows.md) D6): D3, a comment and each edit of it record the token they came through. Built the
same day ([migration 27](../../backend/internal/store/migrations/000027_acts_through_a_token.up.sql)).

**Partly built** (phase 2, 2026-10-02): D1–D4 and D6 — `comments` and `comment_revisions`
(migration 11), the thread oldest first or reversed, edits with their history, withdrawal by the
author, by the person for their agents' comments, by an agent for an agent's of the same person
and by a tenant administrator with an `admin`-scope token (who never edits), the explaining comment on transitions, field
changes and body changes, and the activity list over the audit record. ~~D5's mentions arrive with
the inbox.~~ *(2026-10-04: the inbox of [ADR 0020](0020-notifications-are-an-in-app-inbox-per-person.md)
is built, and a comment tells the ticket's watchers; the mention is not: D5 names `@person` without
saying how a comment's text names a person — a username, which only a local account has, an e-mail
address, which only administrators read, or an id the UI writes — and that is an open decision.)* *(2026-10-04.)* In the browser the author edits a comment over its
version and reads its earlier texts, and the author or a tenant administrator withdraws it after a
confirmation. *(2026-10-05:)* D1's score adoption: the sort of a project's rank by the score
([ADR 0014](0014-rank-is-the-decision-score-is-the-warning.md) D3) is one act of the project that
names every ticket it moved, and the activity of each of them lists it (`ListTicketActivity` in
[`queries/read/comments.sql`](../../backend/internal/store/queries/read/comments.sql)).

## Context

Every earlier record writes into a ticket's history: transitions with reasons
([ADR 0009](0009-ticket-states-are-the-frontmatter-states-plus-blocked.md)), body diffs
([ADR 0011](0011-a-ticket-is-a-markdown-body-plus-first-class-open-questions.md)), links
([ADR 0012](0012-four-typed-directed-links-within-a-tenant.md)), interest
([ADR 0013](0013-interest-is-a-persons-weighted-reasoned-stake-in-a-ticket.md)), rank moves
and score adoptions ([ADR 0014](0014-rank-is-the-decision-score-is-the-warning.md)), question
asked and answered (ADR 0011), urgency overrides
([ADR 0010](0010-the-frontmatter-vocabularies-become-ticket-columns.md)).
[ADR 0004](0004-cowork-is-a-team-product.md) D3 requires all of it attributable and shown.
Beside it, people and agents discuss: an agent's reasoning goes into a comment (ADR 0011),
a person asks back, a client's member explains a need. The owner chose to read the two
apart: the discussion as a thread, the acts as a list one can fold away.

## Decision

**D1 — Two views on a ticket, both chronological.** The **comment thread**: flat, oldest
first, comments only. The **activity list**: every recorded act — transition with reason or
note, body change with diff, field change, assignment, link created or removed, interest
set or changed, rank move, score adoption, question asked, answered or withdrawn, urgency
override, comment withdrawn — one line each, collapsible as a whole and collapsed by default
on a ticket with more than a screen of it.

**D2 — An act may point at the comment that explains it, and the list shows the pointer.**
When an actor performs an act and writes a comment in the same request (an agent finishing
its work, a person changing a decision), the act carries the comment's id; the activity line
reads "… — explained in a comment" and links into the thread, and the comment shows the act
it explains. This is how the two views stay one story.

**D3 — A comment is Markdown, sanitised like the body** (ADR 0011 D6), written by a person or
by an agent in a person's name with the agent mark. *(Amended 2026-10-04, [ADR 0036](0036-a-token-acts-as-its-person-an-agent-flag-is-the-floor-the-agent-header-only-narrows.md) D6: a comment
written through a token records the token, its id and name, beside the agent mark, and so does each
edit of it in the history; the thread shows the mark of the comment, the history that of each
edit.)* Its author may edit it; every edit keeps
the previous text in an edit history readable from the comment. A comment is never deleted:
its author or a tenant administrator may **withdraw** it, which hides the text, keeps the
entry, and writes an act into the activity list. *(Added 2026-10-02: the text lives in the comment and its
revisions only; the acts of commenting, editing and withdrawing carry no text, so a withdrawal
hides it from the thread, the history and the activity alike.)*

**D4 — An agent may edit or withdraw only the comments written by an agent of the same
person.** A person may edit or withdraw their own comments and those their agents wrote.

**D5 — A mention `@person` in a comment notifies the person and makes them a watcher**
(ADR 0013 D6). Mentions resolve inside the tenant only.

**D6 — Both views are rendered from the record, not stored as views.** The activity list is
a projection of the append-only audit record (the data record decides its table); the thread
is the comments table. Neither is a document that could drift from the acts it shows. *(Added 2026-10-02: the activity leaves out time entries and
the reads that leave the system, and shows an act that names a ticket the reader cannot see
without its payload ([ADR 0026](0026-one-append-only-audit-table-written-by-the-request-layer.md) D6).)*

## Consequences

- The thread reads as a conversation without field noise; the list reads as a ledger. A
  reader who wants the interleaving has D2's pointers, not a merged stream.
- An agent's explanation and its act live in different views; D2 costs one nullable column
  on the act and one back-reference in the comment, and without it the split would lose the
  "why" beside the "what".
- Comment edit history and withdrawal add two small tables' worth of rows, and the promise
  that nothing said in a ticket disappears.
- The UI has two components per ticket instead of one, and the fold state of the activity
  list is a per-person convenience.

## Alternatives Considered

- **One merged timeline** — the recommendation. Acts and comments in the order they
  happened, filters to show one kind. Not taken by the owner; D2 keeps the one property of it
  that the split would otherwise lose.
- **A threaded discussion tree.** Replies under replies; in a tool whose rule is "the body is
  the current state, comments are discussion", a tree becomes a second truth beside the
  body. Lost.
- **Deletable comments.** Would let a record disappear from a team product's history. Lost
  to withdrawal.

## Residual risks

- D2 covers acts and comments made in the same request; an explanation written a minute
  later is an ordinary comment with no pointer. Acceptable; a later "link this comment to
  that act" action is an amendment if it is missed.
- The default fold of the activity list (D1) hides acts a reader may want first — a
  transition to `blocked`, say. The ticket header shows the current state and its reason
  regardless of the list.

## References

- [ADR 0004](0004-cowork-is-a-team-product.md) D3 — attribution
- [ADR 0011](0011-a-ticket-is-a-markdown-body-plus-first-class-open-questions.md) D1, D6 — body diffs, sanitising, the agent's comment
- [ADR 0013](0013-interest-is-a-persons-weighted-reasoned-stake-in-a-ticket.md) D6 — the watcher set a mention joins
- [ADR 0009](0009-ticket-states-are-the-frontmatter-states-plus-blocked.md) D6 — transitions as recorded acts
