# ADR 0008: Five Ticket Types and an Optional Parent in the Same Project — a Parent Is a View, Not a Type

## Status

Accepted. Date: 2026-09-29. Decided by the owner as the answer to the catalog question "ticket
types and hierarchy?": a small fixed set of types and an optional parent, over labels only,
over configurable types, and over a parent expressed as a link.

**Partly built** (phase 2, 2026-10-02): D1 and D2 — the five types and the parent in the same
project (~~a composite key; a cycle refused by a walk under a per-project lock~~ *(2026-10-10: the
key is the ticket alone, and the walk crosses projects and teams under one lock for the
installation, D2)*). The views of
D3 ~~and the importer's mapping~~ arrive with them. *(2026-10-06.)* D5 is built with the importer
of [ADR 0051](0051-import-is-a-server-side-two-phase-atomic-job-export-is-its-mirror.md), except the family: the type comes from the title — a
question mark, a `live` or `boundary` finding, the words of a decision, a defect or a missing
capability, else a task, made concrete by the implementer and open to the owner's objection
([ADR 0063](0063-the-importer-takes-whatever-the-user-hands-it-open-and-archived-tickets-alike.md) D3) —, the report names the rule that matched, a correction
sets another, and an `## Open questions` section becomes question entities. A family ticket does
not become a parent with its findings as children: a repository's ticket file names no parent, and
no rule says which files are a family's, so the importer guesses none and reads a parent only from
a `parent:` key, which an export writes. Whether it should recognise a family is open to the owner; until then the
person sets the parents after the import. *(2026-10-04.)* The browser chooses the parent,
on filing and on the detail page, among the project's open tickets *(2026-10-10: and, once the
person types, among the open tickets of every team of theirs, below)*.

**Amended 2026-10-10 by the owner (~~not built~~ built the same day in the data layer and the API;
~~the browser's offer of a parent in another project or team not built~~ *(2026-10-10: and in the
browser, below)*):** D2 — a parent may be a
ticket of another project of the team or of another team of the installation, under the rules D2
now states; the answer that a tenant is a team inside an organisation's installation
([ADR 0005](0005-a-tenant-is-a-client-organisation-and-the-isolation-unit.md) D1, D3) made the
bound to the project too narrow, since a ticket of one team can need a change in another. Built
([migration 47](../../backend/internal/store/migrations/000047_relations_across_teams.up.sql)):
`parent_id` references the ticket alone; `TicketCreate.parent` and `TicketPatch.parent` take a
canonical key of any team, or a short key of any project of the child's team
([ADR 0007](0007-a-ticket-key-is-globally-unique-tenant-slash-project-dash-number.md) D3);
`Ticket.parent` answers the parent's canonical key — null for none and for a placeholder — and
`Ticket.parent_head` its head; the setter's sight of the parent is read in the write's own
transaction (`readable_ticket`), so nothing changes between the check and the write; and the
cycle walk, `parent_chain_reaches`, crosses teams. Both reads and the walk are crossings of
[ADR 0021](0021-row-level-security-is-the-second-line-of-tenant-isolation.md) D7. Built in the
browser the same day: the parent chooser, on filing and on the detail page, offers the open tickets
of the ticket's project before the person types, and once they type the open tickets of every team
of theirs that the person-level search finds for the words or the key
([ADR 0023](0023-the-tenant-is-in-the-path.md) D2,
[ADR 0025](0025-search-is-postgresql-full-text-under-the-same-policy-as-the-data.md)), each with its
team where it is of another, and sends the canonical key. The detail page shows the parent by its
head — its team, key, title, type and state, a link to it only where the person may open it, and
`<team> [Confidential]` for one they may not see, which they may still remove — and lists the
children, a page at a time, each by its head; the backlog's row and the board's card name a parent
of another project or team by its head. *(Made concrete by the implementer, open to the owner's
objection:)* the chooser offers open tickets alone, as it did inside the project, so the browser
sets no closed ticket as a parent; the compact cards of the board's *Next* name no parent.

**Amended again 2026-10-10 by the owner (built the same day in the data layer and the API; the
browser outstanding):** D2 — a parent relation is removed by a writer of either end. The adversarial
review of the build found that a viewer of team A who is a member of team B makes a B ticket the
child of an A ticket, whose derived progress A then shows and whose own progress A can no longer
set, and that nobody in A could end the relation, removing it being a write on the child in B; the
owner chose that a writer of either end removes it, recorded in both teams' records, over consent of
both teams to set it and over the gap written down. Built
([migration 52](../../backend/internal/store/migrations/000052_end_a_relation_from_either_end.up.sql)):
`DELETE …/tickets/{number}/children/{child}` detaches a child of any project or team from the
ticket in the path by the `id` `…/relations` gives the child's relation — the parent's side —, and
the child's side keeps `PATCH {"parent": null}`. *(Made concrete by the implementer, open to the
owner's objection:)* the child's relation id is the parent and the child sealed with the server's
key, as a cursor's position is, so that it shows no id of a child the reader may not see; it names,
it does not admit — the removal checks the writer of the parent and that the ticket is its child as
it stands. A child of the parent's own team is detached by the runtime role; one of another team by
the crossing `end_relation` of
[ADR 0021](0021-row-level-security-is-the-second-line-of-tenant-isolation.md) D7, which clears the
child's parent alone. The child's version stays, as at a purge: the removal is a write on the
parent, and in another team the crossing writes the parent column alone; a patch of the child that
raced the removal is refused by its compare-and-set, which covers the parent it read
([ADR 0050](0050-optimistic-concurrency-a-version-per-entity-if-match-where-a-write-overwrites.md)
D1 as made concrete 2026-10-10). The child records `updated` — in its own team's record, as
`system:relation` where the remover holds no role there — and the parent records `detached`
([ADR 0026](0026-one-append-only-audit-table-written-by-the-request-layer.md) D1), each naming the
other ticket in its refs; a patch that takes a child away from a parent of another team records
`detached` on that parent in its team's record. A relation that is no child of the ticket in the
path, or none, answers `404` "no such child", the same body whatever the reason.

## Context

The Markdown tickets cowork will import come in three shapes: a defect analysed to its root
(a status field that goes stale, a delete without a precondition), an undertaking with a
list of changes (install tooling beside a chart), and a family ticket that collects the
findings of one subject and is closed by the children. A board that cannot tell these apart
cannot be filtered, and a backlog that cannot group a family shows seven loose tickets or one
ticket with a checklist in its body. Configurable types would give every project its own
vocabulary — and a management screen before a single ticket exists.

## Decision

**D1 — Every ticket has exactly one type from a fixed set: `task`, `bug`, `feature`,
`decision`, `question`.** `decision` is a ticket whose outcome is a rule (in this
repository: an ADR); `question` is a ticket whose outcome is an answer from a named person.
The open-question entity inside a ticket (its own record) is not this: a `question` ticket is
the case where the question *is* the work.

**D2 — A ticket may have one parent, ~~which is a ticket of the same project~~** *(amended
2026-10-10, built the same day:)* **a ticket of any project of its team or of another team of the
installation.** The relation is a nullable column, not a link. Cycles are refused at write time;
depth is not bounded. ~~A parent in another project or another tenant is not a parent: a
dependency across projects is a `blocks` link (the links record), never a hierarchy.~~
*(Amended 2026-10-10 by the owner, built the same day in the API, ~~the browser's offer
outstanding~~ and in the browser:)*
Setting the parent is a write on the child: a
`member` or `admin` of the child's team may set it to a ticket they can read — at least a `viewer`
of the parent's team, the parent visible to them, a confidential one included. A parent the person
cannot read is refused exactly like one that does not exist, because ticket numbers are a sequence
per project ([ADR 0022](0022-uuidv7-everywhere-sequences-only-for-ticket-numbers.md)) and any key
taken would let a person try `SUT-1`, `SUT-2`, … and read the heads. ~~Removing it is a write on the
child alone.~~ *(Amended again 2026-10-10 by the owner, built the same day in the data layer and
the API:)* Removing it is a write on either end: a `member` or `admin` of the child's team clears it
on the child, one of the parent's team detaches the child from the parent, whether or not they read
the other end, and the removal is recorded in the records of both teams. The API names the parent by its canonical key
([ADR 0007](0007-a-ticket-key-is-globally-unique-tenant-slash-project-dash-number.md)); the browser
offers the tickets the person can read across their teams. A person who holds no role in the other
end's team sees it by its head only — or as `<team> [Confidential]` —
([ADR 0005](0005-a-tenant-is-a-client-organisation-and-the-isolation-unit.md) D3), and so does a
member to whom the other end's project is restricted
([ADR 0034](0034-three-tenant-roles-an-optional-project-restriction-no-implicit-role-for-the-global-administrator.md)
D4). The cycle check walks the parents across teams under a lock that spans the teams involved.
*(Made concrete 2026-10-10 by the implementer, open to the owner's objection:)* the lock spans the
installation — one advisory lock for every team's parents, `Writer.LockGraph(GraphParents)`, taken
before any other lock of the transaction ([docs/developer/data-access.md](../developer/data-access.md#advisory-locks)).
Locks per team taken in a fixed order were rejected: the set of teams a walk crosses is known only
after the walk, a concurrent writer can grow it, and four writers in a ring close a cycle that no
one of them sees. A ticket filed with a parent takes no walk, because a new ticket has no
descendants to close a cycle with. The miss a parent the person cannot read answers is `400
validation_failed` with "no such ticket" at `/parent`, the same body for a ticket that does not
exist.

**D3 — A parent is a view, not a type.** Any ticket of any type may have children. There is
no `epic`; a ticket with children is shown with its children, its progress is derived from
them, and it is closed by a person, not automatically when the last child closes.

**D4 — Backlog and board show one level by default.** The ranked backlog ranks tickets
without a parent and shows children indented under them; the board shows the tickets that
carry work (the leaves) and lets a parent be expanded. A filter can flatten either.

**D5 — The importer maps by content, and says what it did.** A Markdown ticket that describes
a defect (`severity` set and a defect in the title) becomes a `bug`; one that describes an
undertaking becomes a `feature` or a `task`; a family ticket becomes a parent with its
findings as children; an `## Open questions` section stays with its ticket as question
entities. The mapping is reported per ticket and can be corrected before the import is
committed.

## Consequences

- The board can colour and filter by type without anyone remembering a label; the backlog
  can group a family under its parent and rank the family as one item.
- A sixth type is a migration and a UI change, not a setting. That is intended: a type is a
  meaning cowork attaches behaviour to, not a tag.
- `parent` as a column keeps rank, board and progress queries simple; the same relation as a
  link would have made every list interpret links.
- The state set (the next record) applies to every type alike unless that record says
  otherwise; a `question` ticket in state `in-progress` is a question being worked, which is
  a legitimate state.

## Alternatives Considered

- **One type, labels for everything.** Least model; loses the structure the owner's
  tickets already have by convention, and a forgotten label is a ticket the board mis-shows.
  Lost.
- **Configurable types and hierarchy per project.** A management subsystem — type editor,
  per-type workflow, icons — before a ticket exists, for no known user. Lost.
- **A fixed set, hierarchy only through a `parent-of` link.** Same expressiveness as D2 with
  the parent as one link kind among several; every list would interpret links to show one
  level. Lost to the column.
- **An `epic` type.** Makes "has children" a property of the type rather than of the ticket;
  a bug that grows children would have to change type. Lost to D3.

## Residual risks

- Five types may prove one too many or one too few; the first hundred imported tickets will
  say. Changing the set is a migration, which is the intended cost.
- D5's content mapping is a heuristic and is presented for correction; an import that is
  committed without reading the report will mis-type some tickets.

## References

- [ADR 0006](0006-a-project-is-the-backlog-unit-of-a-tenant-and-owns-its-repositories.md) — the project a parent and its children share
- [ADR 0007](0007-a-ticket-key-is-globally-unique-tenant-slash-project-dash-number.md) — the key every ticket, parent or child, carries
- [docs/tickets/README.md](../tickets/README.md) — the family-ticket rule the importer reads
