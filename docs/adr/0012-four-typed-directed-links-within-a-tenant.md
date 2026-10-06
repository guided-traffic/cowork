# ADR 0012: Four Typed, Directed Links Within a Tenant — `blocks`, `relates-to`, `duplicates`, `found-in` — and No Link Sets a State

## Status

Accepted, amended 2026-10-01 (D1's meaning of `blocks` sharpened to "prerequisite", D6 and
D7 added: the transitive prerequisite view and the refusal of `done` over open
prerequisites), 2026-10-03 (D6: the card counts the open tickets that block it directly and
that the reader can see, computed per read) and 2026-10-06 by the owner (D7: an agent may remove
an open `blocks` link before `done`, and so step around the override, a risk the owner accepts —
the answer to the review after experience of the agent acts no record had listed, recorded with
[ADR 0043](0043-agent-capabilities-are-chosen-per-token-the-default-is-everything-reversible-and-attributable.md)
D2). Date: 2026-09-29. Decided by the owner as the answer to the catalog question
"links between tickets?": a small typed set, over an untyped relation, over links that drive
state automatically, and over tenant-defined link types. The additional rules of D4 were put
to the owner with the question and were not objected to.

**Amended 2026-10-01 on the owner's request:** a ticket may require another ticket, which
may require others, and the owner wants to see at once everything that has to be done to
close a given ticket. That is the `blocks` relation read from the other end; no second link
type is added. D6 makes the transitive closure a view, D7 (proposed with the amendment, open
to objection) stops a ticket from being closed over open prerequisites without a recorded
override.

**Partly built** (phase 2, 2026-10-02): D1–D5 and D7 — `ticket_links` (migration 9) with the
reverse names read from either end, links across projects and never across tenants (by the
API and by the schema's composite keys), an act on both tickets, no self link, the `blocks`
cycle refused by a walk over the tenant's graph under a per-tenant lock, and `done` refused
over open direct prerequisites the closer can see unless a person overrides with a reason
(an agent cannot). D6's prerequisite view arrives with the ticket detail. ~~Removing a `blocks`
link is open to agents until the agent gates are reviewed after experience
([ADR 0043](0043-agent-capabilities-are-chosen-per-token-the-default-is-everything-reversible-and-attributable.md)).~~
*(2026-10-06:)* Removing a `blocks` link stays an agent's by the owner's decision (D7 as amended
that day, ADR 0043 D2); the code did not change
([`UnlinkTickets`](../../backend/internal/api/links.go)).
*(2026-10-03.)* Every ticket carries `open_prerequisites`, built as D7 counts: the open tickets
that block it directly and that the caller can see, computed per read under the visibility
predicate ([ADR 0065](0065-a-confidential-flag-replaces-the-file-name-embargo-set-automatically-lifted-only-by-a-person.md)
D4, D5). The owner chose on 2026-10-03 that the card shows this count (D6 as amended): the
count of the transitive closure cached on the ticket, as D6 had it, would be the same for every
caller and would count prerequisites some of them cannot see; a transitive walk per read over
what the reader can see would either tell that a hidden link exists or undercount.
*(2026-10-04.)* D6's view is built: `GET …/tickets/{number}/prerequisites`, and its
mirror, the dependents, as the same route with `direction=up`, shown on the detail page
([`prerequisites.go`](../../backend/internal/api/prerequisites.go)); eight levels deep, the
context's depth; one recursive query per direction that steps only to tickets the caller can see,
so a hidden node and what lies only behind it are absent (ADR 0065 D5). D4 forbids cycles, not two
paths to one ticket: a ticket the tree reaches under two others stands in full once, under the
first, and as a `repeated` leaf under each other one, and `open` counts each ticket once. The walk
keeps each link once per depth instead of each path — the context's walk before it carried each
path and took seconds on a few dozen densely linked tickets — and the context shows this tree, each
prerequisite once.

## Context

Relations between tickets already exist in the Markdown backlog, each in its own place:
`blocked-by: T<n>` in the frontmatter, `filed-from` naming the analysis a finding came out of,
a `## Related` section in prose, and the filing rule that merges a new finding into the ticket
of the same subject. Earlier records gave three of these a consumer: the `blocked` transition
takes a ticket as its reason ([ADR 0009](0009-ticket-states-are-the-frontmatter-states-plus-blocked.md)
D2), the urgency derivation reads "gated on a release" and "blocked by a decision"
([ADR 0010](0010-the-frontmatter-vocabularies-become-ticket-columns.md) D3), and `filed-from`
was dissolved into a link (ADR 0010 D4). A parent is a column, not a link
([ADR 0008](0008-five-ticket-types-and-an-optional-parent-in-the-same-project.md) D2), and
nothing crosses a tenant ([ADR 0005](0005-a-tenant-is-a-client-organisation-and-the-isolation-unit.md) D3).

## Decision

**D1 — Four link types, each directed, each with a meaning cowork acts on:**

| Type | A → B means | Consumer |
|---|---|---|
| `blocks` | A must be **done** before B can be **finished**: A is a prerequisite of B. Whether work on B may start is the person's call, not the link's *(sharpened 2026-10-01)* | the `blocked` reason kind `ticket`; the urgency derivation; the prerequisite view of D6; the refusal of D7 |
| `relates-to` | A and B concern each other; symmetric, stored once | navigation |
| `duplicates` | A is a duplicate of B; A is normally dropped with that reason | the filing rule; the importer's merge report |
| `found-in` | A was found while working on B | provenance; replaces `filed-from` |

The reverse of a directed link is a view ("requires" / "blocked by", "duplicated by", "found
here"), never a second row.

**D2 — Links stay inside the tenant and may cross projects.** A link whose two ends are in
different tenants is refused by the server, not hidden by the UI.

**D3 — A link is a recorded act.** It carries who created it and when; creating and removing
a link is a timeline entry on both tickets. Links to a `done` or `dropped` ticket are kept and
shown with that state.

**D4 — Integrity rules.** No link from a ticket to itself; `blocks` may not form a cycle
(checked at write time over the `blocks` graph of the tenant); one link of one type between
the same two tickets in the same direction.

**D5 — No link sets a state.** A ticket whose `blocks` source is open shows a hint; the
`blocked` state is set by a person with a reason (ADR 0009 D2), and leaving it is a person's
act as well. When a blocking ticket reaches `done` or `dropped`, the blocked ticket is
notified, not moved.

**D6 — The prerequisites of a ticket are a first-class view** *(added 2026-10-01)*. The
prerequisites of B are the transitive closure of `blocks` edges into B: every A that blocks
B, every ticket that blocks such an A, and so on — a tree because D4 forbids cycles, limited
to the tenant because D2 is. The view shows each node with its key, title, state, assignee
and progress, marks the `done` and `dropped` ones as settled, and gives the count of open
prerequisites; it is served at `…/tickets/{number}/prerequisites` and its mirror, the
dependents of a ticket, at `…/prerequisites` read upward. ~~The ticket card shows the count of
open prerequisites;~~ *(Amended 2026-10-03:)* the ticket card shows the count of the open
tickets that block it directly and that the reader can see, computed per read — the tickets
D7 would refuse `done` over —; the detail page shows the tree. A prerequisite in another project is
shown with its project; the tree never crosses a tenant.

**D7 — `done` over open prerequisites is refused unless a person overrides it with a reason**
*(added 2026-10-01, proposed)*. A transition to `done` on a ticket whose direct `blocks`
sources are not `done` or `dropped` is refused with the list of them; a person may repeat the
transition with an explicit override and a reason, which the activity list records as
"closed over open prerequisites". An agent cannot override. `dropped` is never refused by a
prerequisite. *(Amended 2026-10-06 by the owner:)* An agent may remove a `blocks` link, an open
one into a ticket it is about to close included, and the owner accepts that an agent with `close`
steps around the override that way: it removes the links of the open prerequisites and closes,
two acts of its own, each recorded on both tickets and marked as the agent's. The refusal holds
an agent only while the links stand; a person who wants it to hold gives the agent no `close`.

## Consequences

- The urgency derivation has its inputs: "blocked by a decision" is a `blocks` link from a
  `decision` ticket, "gated on a release" a `blocks` link from a ticket labelled as the
  release (the labels question is open; until then the rule is evaluated on `blocks` alone).
- The importer turns `blocked-by: T<n>` into `blocks` and `filed-from: T<n>` into `found-in`;
  a `filed-from` that names an event rather than a ticket stays a note in the body.
- A fifth type is a migration and a UI change, as with types, states and vocabularies.
- D5 keeps state transitions attributable to people at the cost of one manual act per
  unblock; the notification of D5 makes the act cheap.
- D6 is a recursive query per view (a `WITH RECURSIVE` over the tenant's `blocks` edges);
  the cycle check of D4 bounds it. ~~The count on the card is cached on the ticket and
  recomputed when a link or a prerequisite's state changes.~~ *(Amended 2026-10-03:)* The
  count on the card is one indexed lookup per row, under the reader's visibility; in a chain
  A blocks B blocks C, C shows 1 while A is open behind B, and the tree tells the rest.
- D7 turns "requires" from documentation into a check; the override keeps a person able to
  close a ticket whose prerequisite turned out not to matter, with the reason on record.

## Alternatives Considered

- **Untyped "related".** Nothing can be derived, merged or explained from it; every link would
  need prose beside it that nothing reads. Lost.
- **Links that set and clear `blocked` automatically.** Convenient; but ADR 0009 binds every
  transition to an actor and a reason, and an automatic unblock can throw a ticket back into
  `in-progress` while nobody is working on it. Lost to D5.
- **Tenant-defined link types.** A type editor, and the consumers of D1 would not know the
  new types, which makes them `relates-to` with a label. Lost.
- **Interest ("I need this") as a link type.** It is a relation between a person and a ticket,
  not between two tickets; its own record.

## Residual risks

- The `blocks` cycle check is a graph walk per write; cheap at the sizes expected, and the
  place to look if link creation ever gets slow.
- D1's table gives `relates-to` no consumer beyond navigation; if it stays that way it is
  still worth having as the honest name for "these belong together".
- *(Added 2026-10-06, accepted by the owner with D7's amendment.)* D7's override is a person's
  act, but an agent with `close` can remove the open `blocks` links into its ticket and then close
  it, so a prerequisite holds an agent only while its link stands. The removal is on the
  activity of both tickets, marked as the agent's; nothing refuses it.

## References

- [ADR 0009](0009-ticket-states-are-the-frontmatter-states-plus-blocked.md) D2 — the `blocked` reason a link feeds
- [ADR 0010](0010-the-frontmatter-vocabularies-become-ticket-columns.md) D3, D4 — the derivation and the dissolved `filed-from`
- [ADR 0008](0008-five-ticket-types-and-an-optional-parent-in-the-same-project.md) D2 — the parent that is not a link
- [ADR 0005](0005-a-tenant-is-a-client-organisation-and-the-isolation-unit.md) D3 — no link across tenants
- [ADR 0043](0043-agent-capabilities-are-chosen-per-token-the-default-is-everything-reversible-and-attributable.md) D2, D3 — removing a link is an agent's baseline; overriding the refusal is hard-off
- [docs/tickets/README.md](../tickets/README.md) — the filing rule and the frontmatter fields the importer reads
