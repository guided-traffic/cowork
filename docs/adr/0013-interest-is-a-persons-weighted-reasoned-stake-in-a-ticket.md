# ADR 0013: Interest Is a Person's Weighted, Reasoned Stake in a Ticket — `watch`, `need`, `urgent` — and Feeds Priority Without Touching Rank

## Status

Accepted, amended 2026-10-01 (D4: an agent token with the `interest` capability of
[ADR 0043](0043-agent-capabilities-are-chosen-per-token-the-default-is-everything-reversible-and-attributable.md)
may register `need` and `urgent`) and 2026-10-04 (D1: a stake records the mark of the write that
set it, by the owner's rule that an act an agent or a token makes is always marked,
[ADR 0036](0036-a-token-acts-as-its-person-an-agent-flag-is-the-floor-the-agent-header-only-narrows.md) D6; built the same day, [migration 28](../../backend/internal/store/migrations/000028_filing_and_stake_marks.up.sql)). Date: 2026-09-29. Decided by the owner as the answer to the
catalog question on "users link tickets to take part in prioritisation": a person-to-ticket interest relation
with a weight and a note, over plain votes, over direct rank editing by members, and over a
point budget. The additional rules of D4–D5 were put to the owner with the question and were
not objected to.

**Partly built** (phase 2, 2026-10-02): D1, D2, D4 and D5 — `ticket_interest` (migration 12),
one stake per person and ticket set and removed by its person, visible with the ticket, `watch`
for viewers and agents and `need` and `urgent` for an agent with `interest`, kept and shown as
settled when the ticket is done or dropped, and the lists' `interest` filter. D3's score and
D6's watcher set arrive with the score and the notifications.

## Context

The founding brief wants users to take part in prioritisation by linking themselves to
tickets. [ADR 0004](0004-cowork-is-a-team-product.md) D5 made that a multi-user feature;
[ADR 0012](0012-four-typed-directed-links-within-a-tenant.md) kept it out of the ticket
links, because it relates a person to a ticket. What prioritisation needs from such a stake
is three answers — who, how badly, why — and what notifications need is the list of people
who want to hear about a ticket. A vote gives none of the three; letting members drag the
backlog turns priority into a tug of war and takes away the one thing rank is: the project
owner's decision.

## Decision

**D1 — Interest is a row (person, ticket, weight, note, since).** *(Amended 2026-10-04,
[ADR 0036](0036-a-token-acts-as-its-person-an-agent-flag-is-the-floor-the-agent-header-only-narrows.md) D6: and the mark of the write that set it as it stands — the agent mark of an agent that
set it in the person's name, the token it came through, its id and name — none for the person's
own browser session; the ticket shows it beside the holder.)* One row per person and
ticket, changeable, removable. Weights: `watch` (tell me what happens), `need` (I need this),
`urgent` (this blocks me). The note is free text and is expected for `need` and `urgent`
("for release X on the 15th").

**D2 — Interest is visible inside the tenant.** The ticket shows who holds a stake, with
weight and note. There is no anonymous vote.

**D3 — Interest feeds the computed priority and never moves the rank.** The priority score
(its own record) reads the count and the weights of `need` and `urgent`; `watch` does not
count. The manual rank of a project stays the project's decision.

**D4 — An agent may `watch`, and ~~only `watch`~~** *(amended 2026-10-01: `need` and `urgent`
as well when its token carries the `interest` capability, ADR 0043 D4)*. A session that wants
to follow a ticket registers a `watch` in its person's name.

**D5 — Interest outlives the ticket's work.** When a ticket reaches `done` or `dropped`, its
interest rows are kept and shown as settled, so the people who needed it are notified of the
outcome and the record of who wanted what remains.

**D6 — The watchers of a ticket are: every person with an interest row of any weight, the
assignee, the reporter, and whoever asked or was asked an open question on it.** This is the
set notifications address; the notifications record decides the channel.

## Consequences

- Prioritisation has an input beyond severity and urgency that comes from the people who
  wait for the work, with a reason attached; the owner reads the reasons, not a number.
- The watcher list of D6 exists without a separate "subscribe" feature.
- A client's people can express need without being able to reorder a backlog; the roles
  record does not have to invent a "may prioritise" permission.
- Three weights are a vocabulary; a fourth is a migration.

## Alternatives Considered

- **A plain +1 vote per ticket.** Cheap; says neither who nor why nor how badly, and a person
  who votes on ten tickets has prioritised none. Lost.
- **Direct rank editing by every member.** Priority becomes a contest and the rank loses its
  meaning as a decision; the roles record would have to regulate it. Lost.
- **A point budget per person and tenant** (N `need` points to distribute). Prevents
  inflation by forcing choice, at the cost of budget administration and a poor fit for people
  in many tenants. Possible later if inflation appears; not now.

## Residual risks

- Inflation of `need` and `urgent` is not prevented, only visible (D2). If it happens, the
  budget alternative is the amendment.
- D6's watcher set is generous; a busy ticket notifies many people. The notifications record
  decides digesting and muting.

## References

- [ADR 0004](0004-cowork-is-a-team-product.md) D5 — a multi-user feature
- [ADR 0012](0012-four-typed-directed-links-within-a-tenant.md) — why this is not a link
- [ADR 0010](0010-the-frontmatter-vocabularies-become-ticket-columns.md) — severity and urgency, the other priority inputs
