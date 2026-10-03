# The domain

The rules of tickets and what hangs off them, as the code enforces them: where each rule sits
— the schema, [`internal/domain`](../../backend/internal/domain/), a handler in
[`internal/api`](../../backend/internal/api/) — and the record that decided it. Read against the
tree on 2026-10-03.

## Projects, keys and the counter

A project's key matches `^[A-Z][A-Z0-9]{1,9}$`: upper case, 2 to 10 characters, no hyphen, so
the last hyphen of a ticket key ends it ([ADR 0007] D1). Keys are unique in the tenant for good:
`ProjectKeyTaken` looks past the visibility predicate, and a taken key is
`409 project_key_taken`. A project is archived, never deleted (`archiveProject`, an
administration act); an archived project stays readable, leaves the project list unless
`include_archived`, and refuses new tickets with `409 project_archived` ([ADR 0006] D4).

A ticket's key is `<tenant-slug>/<PROJECT>-<number>`, the short form `<PROJECT>-<number>`
([ADR 0007]). Only the number is stored. Creating a project inserts its `ticket_counters` row in
the same act; filing a ticket takes the next number with `UPDATE ticket_counters … RETURNING`
in the filing's own `Mutate`, so the row lock orders concurrent filings and a filing that rolls
back hands its number out again ([ADR 0022] D2). [`domain.ParseTicketKey`](../../backend/internal/domain/ticket.go)
reads both forms, splits at the last hyphen and refuses a number that is not positive, has a
leading zero or exceeds `int32`; the path's `{number}` stops at 2147483647 as well. Every answer
carries the full key (`domain.FullKey`). `GET /api/v1/tickets/{tenant}/{key}` resolves a short
key in one path segment to the same body and `ETag` as the ticket's own route.

## Fields and vocabularies

| Field | Values | Rule |
|---|---|---|
| `type` | `task`, `bug`, `feature`, `decision`, `question` | [ADR 0008] D1 |
| `state` | `filed`, `analysed`, `decided`, `in-progress`, `blocked`, `done`, `dropped` | changed by transitions only, below |
| `severity` | `critical`, `high`, `medium`, `low`, `cosmetic` | [ADR 0010] D1 |
| `security` | `live`, `boundary`, `hardening`, `none` | `threat` is required unless the class is `none`, and absent with `none` — `400` at `/threat` (`checkThreat`) and a table `CHECK` ([ADR 0010] D2) |
| `urgency` | `now`, `release`, `next`, `later`, `icebox` | derived, may be overridden; below |
| `effort` | `XS`, `S`, `M`, `L` | a size ([ADR 0017] D1) |
| block kind | `decision`, `human`, `product`, `release`, `external`, `ticket` | only while blocked |

Each vocabulary is a PostgreSQL enum (migration `000008_tickets`), a Go type in
[`domain`](../../backend/internal/domain/ticket.go) that sqlc maps the enum onto
([`sqlc.yaml`](../../backend/sqlc.yaml)), and an enum of the API document whose generated type
validates list filters. A change of vocabulary touches all three.

## Who sees a ticket

Two predicates decide it, in the data layer ([ADR 0034] D3, D4; [ADR 0065] D4); the event
stream applies the confidential rule once more in Go, to each event it holds
(`events.Filter`, [events.md](events.md)):

- **The project.** An unrestricted project is visible to everyone in the tenant; a restricted
  one to the tenant's administrators, and to the people on its list (`project_access`) with at
  most the role of their entry. A token restricted to one project sees that project only
  ([ADR 0035] D3).
- **The ticket.** A confidential ticket is visible, inside a visible project, to the tenant's
  administrators, its assignee and its reporter only.

What a reader may not see does not exist for them: its routes answer `404`, lists, links and
the event stream leave it out, and an act that names it is shown without its payload
([ADR 0065] D5). The SQL is [data-access.md](data-access.md#visibility-in-sql).

## Parent

A parent is a ticket of the same project: the composite foreign key
`(tenant_id, project_id, parent_id)` holds it, `resolveParent` refuses another project at
`/parent`, and a ticket is never its own parent ([ADR 0008] D2). Re-parenting takes the project's
lock (`LockParents`) and then walks the chain (`ParentChainContains` over
`ticket_ancestor_or_self`): a parent that is the ticket or one of its descendants is
`409 parent_cycle`. The answer shows the parent by its full key, or `null` when the caller cannot
see it.

## Confidential tickets and assignment

`live` and `boundary` set the confidential flag: on filing, and on a change *to* one of them from
another class while the flag is down ([ADR 0065] D2). A class that merely stays `live` does not
set again what an administrator lifted; nothing lifts the flag automatically (D3).
`PUT …/confidential` is a tenant administrator's act with admin scope, never an agent's (D6),
and lifting needs a reason (D3). The acts are `confidential_set` (with the class as reason when it
is automatic) and `confidential_lifted`.

An assignee must be a member who can see the project (`CanSeeProject`, `400` at `/assignee`
otherwise). Assignment admits a person to a confidential ticket, never to a restricted project
([ADR 0065] D9). It is an act of its own, `assigned`. A write that takes the ticket out of the
writer's sight — a confidential ticket reassigned away from its writer — still answers with the
row it wrote (`GetWrittenTicket`).

## What a ticket's version counts

`UpdateTicketFields` (the fields of `PATCH`: type, title, severity, security, threat, effort,
parent, assignee, progress, the flag set with a class), `UpdateTicketBody`,
`SetUrgencyOverride`, `SetConfidential`, `TransitionTicket` and `MoveTicketRank` (a move in the
rank) raise `version`. A re-derived urgency, a derived progress and the first key the rank gives
an unranked ticket (`RankUnrankedTicket`) do not: they are caused by other tickets' writes and
would fail a concurrent writer for nothing ([ADR 0050] D1). Comments, questions, links, interest,
attachments and time entries are entities of their own and leave the ticket's version alone.

## Urgency

Rule set v1 is [`domain.DeriveUrgency`](../../backend/internal/domain/ticket.go), first match
([ADR 0010] D3):

| Inputs | Urgency | `urgency_rule` |
|---|---|---|
| blocked on `release` | `release` | `v1:release-block` |
| blocked on `decision`, `human` or `product` | `icebox` | `v1:icebox-block` |
| an open ticket of type `decision` blocks it | `icebox` | `v1:icebox-decision` |
| anything else | `later` | `v1:default` |

`now` and `next` therefore come only from an override: `PUT …/urgency-override` with a value and
a reason (an agent needs `override-urgency`), `DELETE` to withdraw; both with `If-Match`, both
recorded as `overridden`. The ticket shows the override when one stands, else the derived value;
`urgency_derived` and `urgency_rule` are always there.

`rederive` in [`links.go`](../../backend/internal/api/links.go) re-applies the rules when an
input may have changed, comparing `UrgencyInputs.Normalized()` before and after: the state only
as blocked or not, the block kind only while blocked, and whether an open decision blocks the
ticket. A real change writes the new derivation with `RederiveUrgency`, which clears any
override without raising the version, and records `overridden` with the reason "an input of the
urgency derivation changed" when one stood. It runs for the target of a `blocks` link that is
added or removed, for the ticket itself on every transition, and for the tickets a decision
blocks when the decision opens or settles (a transition between open and terminal) or when an
open ticket becomes or stops being a decision. The inputs are read past the visibility
predicate — the derivation is the ticket's, not the reader's — and only the derived value
leaves.

## Links

Four types, directed, inside one tenant and across its projects ([ADR 0012]):
`PUT …/links/{type}/{other}` makes the ticket in the path the source and `other` (a short key)
the target; `GET …/links` lists both directions, each read from the ticket's side
(`LinkType.Name`):

| Type | From the source | From the target |
|---|---|---|
| `blocks` | blocks | blocked by |
| `duplicates` | duplicates | duplicated by |
| `found-in` | found in | found here |
| `relates-to` | relates to | relates to |

No link to the ticket itself, one link per type and direction (table constraints);
`relates-to` is stored once with the smaller id as source. A new `blocks` link takes the
tenant's lock (`LockBlocks`) and refuses a cycle (`blocks_path_exists`) with
`409 link_cycle`. Both ends are read through the predicate — an end the caller cannot see is
`404` — and a listed link whose other end the caller cannot see is absent. A link is an act on
both tickets (`linked`, `unlinked`, each with the other ticket in `Refs`). An existing link is
`200` without a second act, a new one `201`; removing a missing link is `204`.

## Transitions

`POST …/transitions` with `from`, `to` and what the move needs
([`transitions.go`](../../backend/internal/api/transitions.go)). `from` must be the current
state, else `409 state_conflict` with the current state in `errors[]` ([ADR 0045] D2). The
matrix is [`domain.ClassifyMove`](../../backend/internal/domain/transition.go)
([ADR 0009]):

| Move | From → to | Needs | An agent needs |
|---|---|---|---|
| forward | `filed` → `analysed` → `decided` → `in-progress`, one step | — | `decide` for → `decided` |
| backward | `in-progress` → `decided` or `analysed`; `decided` → `analysed` | `reason` | — |
| block | `filed`, `analysed`, `decided`, `in-progress` → `blocked` | `reason` (the block's text) and `block` | — |
| unblock | `blocked` → the state it came from | — | — |
| done | `in-progress` → `done` | `note`, the verification | `close` |
| drop | any open state, `blocked` included → `dropped` | `reason` | `drop` |
| reopen | `done` or `dropped` → `filed` | `reason` | — |

Any other pair is `409 state_conflict`. A `block` on another move, or `override_prerequisites`
on anything but done, is `400`.

- **Effects** (`TransitionTicket`): done sets `progress` to 100 and `done_at`; a reopen clears
  `done_at`; reaching `decided` sets `decided_at` each time; entering `blocked` stores where it
  came from and the block, every other move clears them. Done and dropped clear `rank`; a
  reopen ranks the ticket at the bottom (`reopenRank`, before the transition writes anything);
  every other move keeps the rank, `blocked` included ([rank](#rank)).
- **A block that names a ticket** — required for kind `ticket` — reads it through the predicate
  and adds `<that ticket> blocks <this one>` when the link is missing, with its acts, lock and
  cycle check.
- **Prerequisites** ([ADR 0012] D7): done over open direct `blocks` sources the caller can see is
  `409 open_prerequisites`, listing them in `errors[]`, unless `override_prerequisites` with a
  reason — a person's act, hard-off for agents. The act then names the overridden keys and
  carries them in `Refs`.
- An `Idempotency-Key` sent with a transition is recorded on the act.

## Rank

A project's open tickets have a manual order, the rank ([ADR 0014] D1, D2); the code is
[`api/rank.go`](../../backend/internal/api/rank.go) and
[`domain/rank.go`](../../backend/internal/domain/rank.go).

- **A key** is `tickets.rank`, `text COLLATE "C"` (migration `000017_ticket_rank`): a base-62
  fraction over `0-9A-Za-z`, whose ASCII order the `C` collation compares, 1 to 128 characters,
  never ending in `0` — a `CHECK` and `domain.ValidRank`. A key belongs to one ticket of its
  project (the unique index `tickets_by_rank`). A done or dropped ticket has none (a key the
  previous release left on one is read as none, below). A key is computed over tickets the
  caller may not see, so no answer shows one — not a ticket (`ticketView`), not an act, not a
  cursor ([security/tenancy.md](../security/tenancy.md#h-3), H-3); what a client reads of the
  rank is the list's order.
- **`domain.RankBetween(a, b)`** is a key strictly between two keys, `""` an open end: the
  middle between two keys; at an open end the shortest key that moves by no more than the square
  of the distance to that end, so runs of filings at the bottom or moves to the top stay within
  five characters for ten thousand keys. A gap that keeps taking moves halves each time; after
  635 moves directly before the same ticket, or 762 directly after it (`TestRankOneGapRunsOut`),
  the next key would pass 128 characters, and `ErrRankTooLong` fails the move as an internal
  error — no rebalancing is built.
- **The rank lock** is the project's `ticket_counters` row. A filing holds it from
  `NextTicketNumber`; a move and a reopen take it with `LockProjectRank` first. Every key is
  computed from keys read after the lock, over every ticket of the project — those the caller
  cannot see included (`LastRank`, `NextRankedTicket`, `PreviousRankedTicket`) — so two writes
  never compute a key from the same neighbours and no key is handed out twice.
- **A filing and a reopen** get `rankAtBottom`: the key after the greatest of the project.
- **A move** is `PUT …/{number}/rank` with `{"after": n}` or `{"before": n}`, a number of the
  same project. Under the lock it reads the ticket and the neighbour again, and beside the
  neighbour on that side (`beside`) the first open ticket the caller can see
  (`NextSeenRankedTicket`, `PreviousSeenRankedTicket`) and the next key of any ticket
  (`NextRankedTicket`, `PreviousRankedTicket`). When the ticket the caller sees there is the
  moved one, it already sits there and the answer is `200` unchanged, without an act, whatever
  hidden ticket sits between them (`planRank`). Otherwise `MoveTicketRank` writes a key between
  the neighbour's and that next key and raises the version, and the act `ranked` names the
  neighbour's full key under `after` or `before`, with the neighbour in `Refs` — no rank key.
  No `If-Match`: the last move wins ([ADR 0050] D4). A member's act with `write` scope; an
  agent needs `rank`.
- **Refusals:** a body with neither or both of `after` and `before`, and the ticket as its own
  neighbour, are `400 validation_failed`; a neighbour that does not exist or that the caller
  cannot see is the same `400` at `/after` or `/before`; a done or dropped ticket, or neighbour,
  is `409 state_conflict` naming its state; a ticket that went done or dropped between the read
  and the write is `409` as well.
- **Tickets without a key.** The previous release files and reopens tickets without one, and
  its done and dropped leave a key in place ([ADR 0028] D3). Before a write hands out a key, it
  ranks its project's open tickets without one at the bottom, in number order
  (`rankUnranked`) — no act and no version; one that went done or dropped meanwhile, which
  takes no rank lock, gets none (`RankUnrankedTicket` checks the state). A move that turns out
  to change nothing rolls those keys back with it. The key a done or dropped ticket kept is
  read as none: the list orders by `rankedKey` in
  [`store/tickets.go`](../../backend/internal/store/tickets.go), `TicketOrder.Position` leaves it
  out, and a move's `NextSeenRankedTicket` counts open tickets only.
- **The project's list** orders the ranked tickets by their key, then the unranked — done,
  dropped, and open ones of the previous release — by number. Its cursor carries the key and
  the number sealed ([api.md](api.md#paging)). Migration 17 ranked every project's open tickets
  in number order, evenly spaced.

## Questions

A question belongs to a ticket and has a number there, taken under the ticket's question lock
(`LockQuestions`, `NextQuestionNumber`) and kept when others are withdrawn, so the export's
`### Q<n>` stays stable ([ADR 0011] D2, D4). The routes address it by that number. Rules in
[`questions.go`](../../backend/internal/api/questions.go):

| Act | Who |
|---|---|
| ask | a member; `asked_of`, when set, must be a member who can see the ticket (`CanSeeTicket`); without it the question is open in the tenant |
| edit (`If-Match`) | the asker, while it is open |
| answer | the person asked, or any member when it is open in the tenant; a person changes their own answer, with `If-Match` |
| answer as an agent | needs `record-answer`; the answer stays its person's, `recorded_by_agent` is set, and an agent changes only an answer an agent recorded ([ADR 0066] D8) |
| withdraw | the asker; an agent only what an agent asked |

A withdrawn question takes no answer; an answered or withdrawn one no edit.

## Comments and the activity list

[`comments.go`](../../backend/internal/api/comments.go), [ADR 0015]:

- **The thread** is oldest first, `order=desc` reverses it. A comment is written by a person, or
  by an agent in its person's name with the agent mark.
- **Edits** keep the previous text in `comment_revisions`. **Withdrawal** keeps the entry and
  hides its text: `body` is `null` in every answer and the revision list is empty. Nothing is
  deleted. A person changes their own comments and those their agents wrote; an agent only those
  an agent of the same person wrote; a tenant administrator withdraws any comment and edits none
  (D3, D4).
- **Comment texts never enter the audit record**, which cannot forget: `commented`, `edited`
  and `withdrawn` carry no text.
- **An explaining comment** — the `comment` field of `PATCH` on a ticket, `PUT …/body` and
  `POST …/transitions` — is written in the same transaction as the act; the act's
  `explained_by_comment_id` names it, and the comment's `explains` lists the actions it explains
  (D2).
- **The activity list** is a projection of the ticket's audit rows (`ListTicketActivity`),
  without time entries and without `downloaded`, `exported`, `booked`, `voided` and `locked`.
  An act whose `refs` name a ticket the reader cannot see is shown with `redacted: true` and no
  `before`, `after`, `reason` or `note` ([ADR 0065] D4).

## Interest

One stake per person and ticket, `watch`, `need` or `urgent` with a note, which outlives the
ticket's work and shows as `settled` once it is done or dropped ([ADR 0013]). `PUT …/interest`
sets the caller's own (`201` new, `200` changed or unchanged), `DELETE` removes it (`204`, also
when there is none). `watch` is open to viewers; `need` and `urgent` need a member, and an agent
needs `interest`; an agent may remove its person's stake. The act is `interest`, with the person
as entity.

## Progress

`progress` is 0 to 100 in steps of five ([ADR 0017] D2). `PATCH` sets it on an open ticket
without children; on a done or dropped ticket, or one whose progress is derived, it is
`409 state_conflict` with the current value.

A ticket with children shows the derived progress (`ticket_derived_progress`, migration
`000008`): the mean of the children weighted by effort (XS 1, S 2, M 3, L 5), a dropped child
left out, a done child counted as 100, rounded to the nearest five with halves up; 0 when every
child is dropped. `refreshProgress` derives it again up the ancestors, as far as the value
changes, after a child is filed, re-parented, changes effort or progress, or moves; the version
stays. When the last child leaves, the ticket's own value starts at the last derived one. The
progress a ticket shows is 100 when done, else the derived value while it has children, else its
own (D3, D5).

## Time

[`time.go`](../../backend/internal/api/time.go), [ADR 0017] D6–D10:

- A person books their own time on a ticket: 1 to 1440 minutes, a day, a note. Booking,
  correcting and voiding need a member with write scope and are hard-off for agents — refused
  before an agent's missing `Idempotency-Key` would be.
- Only the author corrects (`If-Match`; the previous values go to `time_entry_revisions`) or
  voids an entry; nothing is deleted. A voided entry counts in no sum and takes no correction;
  voiding it again changes nothing.
- A tenant administrator closes a period by moving `time_locked_until` (`PATCH` on the tenant,
  recorded as `locked`). A write on a day on or before it — for a correction the old and the new
  day — is `409 period_locked`; the lock is read `FOR SHARE`, so a concurrent move waits.
- Who sees an entry is `app_time_visible` next to the ticket's predicate
  ([data-access.md](data-access.md#visibility-in-sql)); a ticket's list carries the visible sum
  (`total_minutes`), the tenant's list and the report (by ticket, project, person or tenant) answer
  JSON or CSV.
- Time entries appear neither in a ticket's activity nor on the event stream.

## Not built

The score of [ADR 0014] D3–D5 is not built — no score beside the rank, and no person-level
lists for it to order — nor is the rebalancing of the rank's keys. There is no `deleted_at` and
no deletion or purge ([ADR 0024]). No route creates memberships, entries on a restricted
project's list or tokens; the tests and `make dev-seed` write them over the administrative
connection ([testing.md](testing.md#fixtures-of-the-integration-tier)).

[ADR 0006]: ../adr/0006-a-project-is-the-backlog-unit-of-a-tenant-and-owns-its-repositories.md
[ADR 0007]: ../adr/0007-a-ticket-key-is-globally-unique-tenant-slash-project-dash-number.md
[ADR 0008]: ../adr/0008-five-ticket-types-and-an-optional-parent-in-the-same-project.md
[ADR 0009]: ../adr/0009-ticket-states-are-the-frontmatter-states-plus-blocked.md
[ADR 0010]: ../adr/0010-the-frontmatter-vocabularies-become-ticket-columns.md
[ADR 0011]: ../adr/0011-a-ticket-is-a-markdown-body-plus-first-class-open-questions.md
[ADR 0012]: ../adr/0012-four-typed-directed-links-within-a-tenant.md
[ADR 0013]: ../adr/0013-interest-is-a-persons-weighted-reasoned-stake-in-a-ticket.md
[ADR 0014]: ../adr/0014-rank-is-the-decision-score-is-the-warning.md
[ADR 0015]: ../adr/0015-comments-are-a-thread-and-activity-is-a-separate-list.md
[ADR 0017]: ../adr/0017-effort-is-a-size-progress-is-a-five-step-percentage-and-time-is-booked-by-people.md
[ADR 0022]: ../adr/0022-uuidv7-everywhere-sequences-only-for-ticket-numbers.md
[ADR 0024]: ../adr/0024-deletion-tickets-are-soft-deleted-and-purged-projects-archived-people-deactivated-tenants-deleted-explicitly.md
[ADR 0028]: ../adr/0028-migrations-only-go-forward-no-down-files-expand-before-contract.md
[ADR 0034]: ../adr/0034-three-tenant-roles-an-optional-project-restriction-no-implicit-role-for-the-global-administrator.md
[ADR 0035]: ../adr/0035-personal-access-tokens.md
[ADR 0045]: ../adr/0045-idempotency-put-where-it-is-free-a-required-key-on-agent-posts-stored-with-the-act.md
[ADR 0050]: ../adr/0050-optimistic-concurrency-a-version-per-entity-if-match-where-a-write-overwrites.md
[ADR 0065]: ../adr/0065-a-confidential-flag-replaces-the-file-name-embargo-set-automatically-lifted-only-by-a-person.md
[ADR 0066]: ../adr/0066-repositories-are-bound-by-their-normalised-remote-identity-creation-proposed-by-the-agent-confirmed-by-the-person.md
