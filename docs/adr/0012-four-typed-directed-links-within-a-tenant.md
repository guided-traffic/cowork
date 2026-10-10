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

**Amended 2026-10-10 by the owner (~~not built~~ built the same day in the data layer and the API;
~~the UI not built~~ *(2026-10-10: and in the UI, below)*):** D2, D4, D6, D7 — links cross teams,
under the
rules of a parent across teams
([ADR 0008](0008-five-ticket-types-and-an-optional-parent-in-the-same-project.md) D2), once a tenant
became a team inside an organisation's installation
([ADR 0005](0005-a-tenant-is-a-client-organisation-and-the-isolation-unit.md) D1, D3): "a ticket of
one team needs a change in another" is what `blocks` means, and one rule for every relation is the
simpler one to hold. The title's "within a tenant" is the rule before.

**Partly built** (phase 2, 2026-10-02): D1–D5 and D7 — `ticket_links` (migration 9) with the
reverse names read from either end, links across projects ~~and never across tenants (by the
API and by the schema's composite keys)~~ *(2026-10-10: and teams, below)*, an act on both tickets,
no self link, the `blocks` cycle refused by a walk over the ~~tenant's graph under a per-tenant
lock~~ *(2026-10-10: installation's graph under one lock for it, below)*, and `done` refused
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
*(2026-10-06.)* The importer of [ADR 0051](0051-import-is-a-server-side-two-phase-atomic-job-export-is-its-mirror.md) makes the links of the
Consequences — `blocked-by: T<n>` a `blocks` link, `filed-from: T<n>` a `found-in` link — and an
export's links manifest's: each once, never to its own ticket, a `blocks` link that would close a
cycle among the imported tickets left out by the analysis and one through the project's tickets by
D4's walk under the tenant's lock, each named in the report.
*(2026-10-10.)* The amendment is built in the data layer and the API
([migration 47](../../backend/internal/store/migrations/000047_relations_across_teams.up.sql)); ~~the UI is not~~ *(2026-10-10: the UI the same day, below)*. A link lives in its source's team, its target a
ticket of any team; `PUT …/links/{type}/{other_team}/{other}` sets one by the other end's canonical
key, the route by the short key stays the short form inside the team
([ADR 0007](0007-a-ticket-key-is-globally-unique-tenant-slash-project-dash-number.md) D3), and a key
the setter cannot read answers `404 not_found` "no such ticket", as a missing one does. *(Made
concrete by the implementer, open to the owner's objection:)* a link is removed by a writer of ~~its
source — a `relates-to` inside the team from either end, across teams from the end it was made
from, which is its source —~~ *(2026-10-10, D2 as amended again by the owner: either end, below)*, by
the other end's key or by its id (`DELETE …/links/{link}`), which
removes one whose other end is a placeholder; its acts are recorded on both tickets, the other
end's in that team's own record (D3), and the payload of an act that names a ticket of another team
is never shown to a reader of the act's team
([ADR 0065](0065-a-confidential-flag-replaces-the-file-name-embargo-set-automatically-lifted-only-by-a-person.md)
D4's redaction). D4's walk crosses teams under one lock for the installation's `blocks` graph, taken
before any other lock of the transaction: the transition to `blocked` and an import's execution
take it before they write a ticket ([docs/developer/data-access.md](../developer/data-access.md#advisory-locks)).
D5 across teams: a ticket that reaches `done` or `dropped` records `prerequisite_settled` on every
ticket of another team it blocks, in that team's record, naming itself ~~by its head as a person
outside its team reads it — the placeholder where it is confidential —~~ *(2026-10-10, after the
security review: in its refs alone, below)*, and the act tells the blocked ticket's watchers who see
it, as `blocker_closed`
([ADR 0026](0026-one-append-only-audit-table-written-by-the-request-layer.md) D1 gains the action).
D6: `GET …/tickets/{number}/prerequisite-tree`, and its mirror with `direction=up`, reads the tree
across teams, each node by its head; the walk goes on only from a ticket the caller reads, a head
and a placeholder are leaves, and only a node of the caller's own team they read shows its assignee
and its progress *(after the security review: and only a node the caller reads where its blocked
ticket came from, a head being five fields, migration 50)*; `…/prerequisites` keeps its meaning, the team's tickets the caller sees, and is
deprecated ([ADR 0046](0046-spec-first-the-openapi-document-is-the-contract.md) D7). D6's count
and D7's refusal read one rule: an open direct prerequisite counts, and refuses `done`, whenever the
caller reads its state in a head, of any team, and a placeholder neither counts nor refuses — so a
prerequisite of the caller's own team in a project restricted from them, hidden before, now counts
by its head.
*(2026-10-10.)* The UI: the link field offers the open tickets of the ticket's project from its
button and, once the person types, the tickets of every team of theirs that the person-level search
finds ([ADR 0023](0023-the-tenant-is-in-the-path.md) D2), and takes a key typed — canonical, or the
short form inside the team ([ADR 0007](0007-a-ticket-key-is-globally-unique-tenant-slash-project-dash-number.md)
D3) —, sending the canonical key; the detail page reads `…/relations` and `…/prerequisite-tree` and
shows each link end and each node by its head, a link to it only where the person may open it, the
placeholder `<team> [Confidential]` where they may not see it, and a node of the person's own team
they read with its assignee and its stage; ~~it removes a link by its id from its source — this
ticket for an outgoing link, a placeholder's included, the other ticket for an incoming one the
person reads. *(Made concrete by the implementer, open to the owner's objection:)* an incoming link
from a ticket the person may not open, or may not see, is offered no removal: its source is a
ticket the person cannot write, whose team removes it.~~ *(2026-10-10, D2 as amended again by the
owner, below:)* a writer of the ticket removes any of its links by the link's id, outgoing or
incoming, of any team, whatever they read of the other end, and a link that is gone already is no
failure. ~~The activity and the inbox name a settled prerequisite of another team by its head
(D5).~~ *(2026-10-10, after the security review:)* The activity and the inbox say that a ticket of
another team that blocks it was closed, naming none, as the act names it in its refs alone (D5).

**Amended again 2026-10-10 by the owner (built the same day in the data layer and the API; ~~the UI
outstanding~~ *(2026-10-10: and in the UI, below)*):** D2 — a link is removed by a writer of either
end. The adversarial review of the build
found that a viewer of team A who is a member of team B lets a B ticket block an A ticket, which A
then reaches `done` only over a person's override and A's agents cannot close, and that nobody in A
could remove the link, removing it being a write on its source in B; the owner chose that a writer of
either end removes it, recorded in both teams' records, over consent of both teams to set it and
over the gap written down
([ADR 0008](0008-five-ticket-types-and-an-optional-parent-in-the-same-project.md) D2 likewise).
Built ([migration 52](../../backend/internal/store/migrations/000052_end_a_relation_from_either_end.up.sql)):
`DELETE …/links/{link}`, `DELETE …/links/{type}/{other_team}/{other}` and the short form inside the
team remove a link of which the ticket in the path is the source or the target, whatever team keeps
it — the key routes the one the ticket is the source of first, the link its `PUT` made. A link the
team keeps is removed by the runtime role; one another team keeps onto the ticket by the crossing
`end_relation` of [ADR 0021](0021-row-level-security-is-the-second-line-of-tenant-isolation.md) D7,
which deletes that row alone. Each `…/relations` entry carries the relation's `id`, a link's own.
*(Made concrete by the implementer, open to the owner's objection:)* the removal by id answers a
link that does not touch the ticket in the path exactly as none, `404` "no such link", where it
answered `204` and removed nothing before; the key routes stay idempotent, `204` for a key that names
no link. Of two removals of one link at once, from its two ends, one removes it and records the act
on both tickets, and the other finds it gone — `404` by id, `204` by key — and records nothing.
Built in the UI the same day: the detail page offers a member or an administrator of the ticket's
team the removal of every link of the ticket, outgoing or incoming, of any team, whether or not they
may open the other end or see it, by the link's id at the ticket shown; the list loads again at
once, and a `404` — the link removed from its other end meanwhile — is taken for removed, not
toasted. A viewer is offered no removal. The end-to-end tier walks a member of one team removing a
`blocks` link another team keeps onto its ticket
([`relations.spec.ts`](../../frontend/e2e/relations.spec.ts)).

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

**D2 — ~~Links stay inside the tenant and may cross projects.~~ ~~A link whose two ends are in
different tenants is refused by the server, not hidden by the UI.~~** *(Amended 2026-10-10 by the
owner, built the same day in the API and the UI:)* **Links may cross projects and teams of the installation,
every type alike.** A
link is set by a `member` or `admin` of the source's team who can read the target, and refused
like a missing ticket where they cannot; the other end is shown to a person who holds no role in
its team, or to whom its project is restricted, by its head only — the team's name, the key, the
title, the type and the state — or as `<team> [Confidential]`
([ADR 0005](0005-a-tenant-is-a-client-organisation-and-the-isolation-unit.md) D3). *(Amended again
2026-10-10 by the owner, built the same day in the data layer and the API:)* A link is removed by a
`member` or `admin` of either end's team — the source's or the target's —, whether or not they read
the other end, and the removal is recorded in the records of both teams.

**D3 — A link is a recorded act.** It carries who created it and when; creating and removing
a link is a timeline entry on both tickets. Links to a `done` or `dropped` ticket are kept and
shown with that state.

**D4 — Integrity rules.** No link from a ticket to itself; `blocks` may not form a cycle
(checked at write time over the `blocks` graph ~~of the tenant~~ *(2026-10-10, built the same day:
across the teams it reaches, under a lock that spans them — one lock for the installation's
`blocks` graph)*); one link of one type between the same two
tickets in the same direction.

**D5 — No link sets a state.** A ticket whose `blocks` source is open shows a hint; the
`blocked` state is set by a person with a reason (ADR 0009 D2), and leaving it is a person's
act as well. When a blocking ticket reaches `done` or `dropped`, the blocked ticket is
notified, not moved. *(Made concrete 2026-10-10 by the implementer, open to the owner's
objection:)* a blocked ticket of another team is notified through an act in its own team's record,
`prerequisite_settled`, which names the prerequisite ~~by its head~~ and tells the blocked ticket's
watchers. *(Made concrete again 2026-10-10 by the implementer after the security review, open to
the owner's objection:)* the act names the prerequisite in its refs alone and stores no head of it,
so its entry in the activity is redacted for every reader of the blocked ticket's team, the inbox
names no blocker for it — the blocked ticket's relations show the prerequisite by its head as it
is now —, and nothing of the prerequisite outlives a later confidential flag or a purge in the other
team's record; a closer who holds no role in the blocked ticket's team is recorded there as
`system:relation`, with no token and no agent mark (ADR 0026 D1;
`TestTheWatchersOfAnotherTeamAreToldWhenAPrerequisiteSettles`).

**D6 — The prerequisites of a ticket are a first-class view** *(added 2026-10-01)*. The
prerequisites of B are the transitive closure of `blocks` edges into B: every A that blocks
B, every ticket that blocks such an A, and so on — a tree because D4 forbids cycles, ~~limited
to the tenant because D2 is~~ *(2026-10-10: across the teams D2 lets links reach)*. The view shows each node with its key, title, state, assignee
and progress, marks the `done` and `dropped` ones as settled, and gives the count of open
prerequisites; it is served at `…/tickets/{number}/prerequisites` and its mirror, the
dependents of a ticket, at `…/prerequisites` read upward. ~~The ticket card shows the count of
open prerequisites;~~ *(Amended 2026-10-03:)* the ticket card shows the count of the open
tickets that block it directly and that the reader can see, computed per read — the tickets
D7 would refuse `done` over —; the detail page shows the tree. A prerequisite in another project is
shown with its project; ~~the tree never crosses a tenant~~ *(amended 2026-10-10, built the same
day as `…/prerequisite-tree`:)* a
prerequisite in another team is shown with its team, by its head only — key, title, type and state,
no assignee and no progress — to a person who holds no role there.

**D7 — `done` over open prerequisites is refused unless a person overrides it with a reason**
*(added 2026-10-01, proposed)*. A transition to `done` on a ticket whose direct `blocks`
sources are not `done` or `dropped` is refused with the list of them; a person may repeat the
transition with an explicit override and a reason, which the activity list records as
"closed over open prerequisites". An agent cannot override. `dropped` is never refused by a
prerequisite. *(2026-10-10, built the same day:)* A prerequisite in another team counts like one of the
own team whenever the closer reads its state in its head. *(Amended 2026-10-06 by the owner:)* An agent may remove a `blocks` link, an open
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
- D6 is a recursive query per view (a `WITH RECURSIVE` over the ~~tenant's~~ `blocks` edges
  *(2026-10-10: of every team, stepping on only from a ticket the reader reads)*); the cycle check
  of D4 bounds it. ~~The count on the card is cached on the ticket and
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
- *(Added 2026-10-10 after the security review.)* D4's lock: the installation's lock of the `blocks` graph is one lock for every team, and an import's execution
  that may make a `blocks` link — one in `links.json`, or any file's `blocked-by`, whether or not
  the link it becomes touches a ticket the upload does not bring — takes it first and holds it for
  the whole execution, up to the request timeout (`COWORK_REQUEST_TIMEOUT`, 30 seconds by default,
  none at `0`). Meanwhile every `blocks` link, every block that names a ticket, and every other
  such import in any team of the installation waits: an import of 1,500 files with one `blocked-by`
  held it for 4 seconds in the security review, and a `PUT` of a `blocks` link in another team
  waited 3.7 seconds for it. The lock order — the graph locks first, before the rank's row lock
  — is what keeps an import from deadlocking with those writers, and whether an execution makes a
  link that touches an existing ticket is known only from the analysis under the rank's row lock,
  too late to take the graph lock then; a narrower lock is not built. Splitting a large import by
  directory bounds the wait.
- D1's table gives `relates-to` no consumer beyond navigation; if it stays that way it is
  still worth having as the honest name for "these belong together".
- *(Added 2026-10-06, accepted by the owner with D7's amendment.)* D7's override is a person's
  act, but an agent with `close` can remove the open `blocks` links into its ticket and then close
  it, so a prerequisite holds an agent only while its link stands. The removal is on the
  activity of both tickets, marked as the agent's; nothing refuses it. *(2026-10-10, with D2 as
  amended again by the owner:)* that includes a link another team keeps onto the agent's ticket,
  removed from the ticket's side: a prerequisite of team B holds team A's agent only while A lets
  its link stand, the remedy the owner gave the team a relation lands on. The act in B's record is
  `system:relation` where the agent's person holds no role in B, so B's record shows that the link
  went and when, not which agent removed it; A's record names the agent.

## References

- [ADR 0009](0009-ticket-states-are-the-frontmatter-states-plus-blocked.md) D2 — the `blocked` reason a link feeds
- [ADR 0010](0010-the-frontmatter-vocabularies-become-ticket-columns.md) D3, D4 — the derivation and the dissolved `filed-from`
- [ADR 0008](0008-five-ticket-types-and-an-optional-parent-in-the-same-project.md) D2 — the parent that is not a link
- [ADR 0005](0005-a-tenant-is-a-client-organisation-and-the-isolation-unit.md) D3 — no link across tenants
- [ADR 0043](0043-agent-capabilities-are-chosen-per-token-the-default-is-everything-reversible-and-attributable.md) D2, D3 — removing a link is an agent's baseline; overriding the refusal is hard-off
- [docs/tickets/README.md](../tickets/README.md) — the filing rule and the frontmatter fields the importer reads
