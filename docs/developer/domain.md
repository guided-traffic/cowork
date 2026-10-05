# The domain

The rules of tickets and what hangs off them, as the code enforces them: where each rule sits
— the schema, [`internal/domain`](../../backend/internal/domain/), a handler in
[`internal/api`](../../backend/internal/api/) — and the record that decided it. Read against the
tree on 2026-10-04.

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

## Repositories

A project owns zero or more repositories ([ADR 0006] D3), each bound by its normalised remote
identity ([ADR 0066] D1), [`domain/repository.go`](../../backend/internal/domain/repository.go)
and [`api/repositories.go`](../../backend/internal/api/repositories.go):

- **The identity.** `NormaliseRemote` reduces an SSH, scp-style, git or HTTP(S) URL to
  `host/path`: the scheme, the user, a default port, a trailing `.git` and trailing slashes
  removed, the host lower-cased, the path's case and all of its segments kept;
  `git@github.com:acme/app.git`, `https://github.com/acme/app` and
  `ssh://git@github.com:22/acme/app/` are `github.com/acme/app`, and a non-default port stays
  (`gitlab.example.com:2222/group/sub/repo`). A local path or a `file://` URL names no host and
  binds nothing (`ErrNotARemote`). `TestNormaliseRemote` is the table.
- **The binding** (`project_repositories`, migration 23) holds the identity, an optional
  sub-directory of a monorepo (`NormaliseRepositoryPath`: relative, no `..`, `""` for the whole
  repository) and the remote as it was last given, without credentials (`SanitiseRemote`: an
  HTTP(S) URL loses its user information, another URL its password). The identity and the
  sub-directory are unique in the tenant: a repository is in at most one project of a tenant
  ([ADR 0066] D6). Across tenants nothing holds it to one; the lookup reports several.
- **Binding and unbinding** (`POST`, `DELETE …/projects/{project}/repositories`) are the act of
  creating a project (`creating`): an administrator, or a member while the tenant lets members
  create projects, judged by the role in the project; `write`; an agent with `create-project`
  ([ADR 0043] D4). Binding is idempotent over the identity and the sub-directory — `201` for a
  new binding, `200` for one the project holds, its remote updated to the form given —, and one
  another project of the tenant holds is `409 repository_bound`, naming that project only to a
  caller who sees it. Unbinding answers `204` also when the binding is gone. The acts are
  `linked`, `updated` and `unlinked` on the entity `repository`.
- **Creating a project for a repository** (`POST …/projects` with `repository`,
  [ADR 0066] D3, D5): the project, its counter and the binding in one act, `created` with the
  repository in its `after`. When a project of the tenant binds the repository already the
  answer is `200` with that project and nothing is created — `409 repository_bound` when the
  caller cannot see it.
- **The lookup** (`GET /api/v1/me/repositories/lookup`, D2) normalises every remote, keeps
  their order, and reads each of the person's tenants — a token's restriction narrows them — for
  the bindings of projects the caller sees. The first remote with a binding whose sub-directory
  covers the working directory's (`PathCovers`) decides, the most specific sub-directory
  first: one binding is `bound`, several `ambiguous`. With none, the proposal comes from the
  first remote with an identity: the tenants where the caller may create a project — none for
  a project-restricted token — narrowed to the only one (`only-tenant`), else to the one that
  binds repositories under the same owner, the identity without its last segment (`remote-owner`;
  several of them, or none, are the list to `choose` from); the repository's name as the name;
  and per tenant a key free there — `ProposeProjectKey`, the initials of the parts a hyphen, an
  underscore or a dot divides (`valkey-operator` is `VO`), else the first three letters, then
  `KeyCandidate` with 2, 3, … appended until `ProjectKeyTaken` says free.

## Fields and vocabularies

| Field | Values | Rule |
|---|---|---|
| `type` | `task`, `bug`, `feature`, `decision`, `question` | [ADR 0008] D1 |
| `state` | `filed`, `analysed`, `decided`, `in-progress`, `review`, `blocked`, `done`, `dropped` | changed by transitions, and by the progress stages that close and reopen a ticket; below |
| `severity` | `critical`, `high`, `medium`, `low`, `cosmetic` | [ADR 0010] D1 |
| `security` | `live`, `boundary`, `hardening`, `none` | `threat` is required unless the class is `none`, and absent with `none` — `400` at `/threat` (`checkThreat`) and a table `CHECK` ([ADR 0010] D2) |
| `urgency` | `now`, `release`, `next`, `later`, `icebox` | the ticket's horizon, set by a person or an agent; below |
| `effort` | `XS`, `S`, `M`, `L` | a size ([ADR 0017] D1) |
| block kind | `decision`, `human`, `product`, `release`, `external`, `ticket` | only while blocked |

Each vocabulary is a PostgreSQL enum (migration `000008_tickets`; `review` added by
`000018_ticket_state_review`), a Go type in
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
parent, assignee, the three progress stages, the flag set with a class), `UpdateTicketBody`,
`SetUrgencyOverride`, `SetConfidential`, `TransitionTicket`, `EndDoneByHand` and
`MoveTicketRank` (a move in the rank) raise `version` by one. A `PATCH` whose stages close or
reopen the ticket writes its state with `TransitionTicket` too, with `bump` false: one request,
one version. Derived stages and the first key the rank gives an unranked ticket
(`RankUnrankedTicket`) do not: they are caused by other tickets' writes and would fail a
concurrent writer for nothing ([ADR 0050] D1). Comments, questions, links, interest,
attachments and time entries are entities of their own and leave the ticket's version alone.

## Urgency, the horizon

The five values are the ticket's horizon — a planning category a person or an agent sets, in
whatever state the ticket is ([ADR 0010] D3 as amended 2026-10-04). Nothing derives it: rule set
v2 has one row, [`domain.UrgencyDefault`](../../backend/internal/domain/ticket.go) `later` with the
rule `UrgencyRuleDefault` `v2:default`, which every filing writes as `urgency_derived` and
`urgency_rule`; no state, block or link changes them.

What a person or an agent sets is stored as the override: `PUT …/urgency-override` with a value,
`DELETE` to return the ticket to `later`; both with `If-Match`, both recorded as `overridden`, an
agent's needing `override-urgency`. The reason is optional for a person — a drag between the
backlog's groups — and required of an agent, whose request without one is `400` at `/reason`
(`overrideInputs`); `urgency_override.reason` is `null` without one. A filing names its horizon
with `urgency`: another than `later` is written as the override by `InsertTicket`, set by the
filer and without a reason, and an agent needs `override-urgency` for it (`filing.capabilities`
in [`tickets.go`](../../backend/internal/api/tickets.go)). The ticket shows the override when one
stands, else `later`. Migration 29 turned what rule set v1 had derived — `release` and `icebox`
for blocked tickets and those an open decision blocked — into overrides set by nobody, so no
ticket moved when the derivation was retired.

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

### The prerequisite tree

`GET …/{number}/prerequisites` ([ADR 0012] D6,
[`prerequisites.go`](../../backend/internal/api/prerequisites.go)) is the tree of the tickets that
block a ticket, what blocks those, and so on; `direction=up` reads the `blocks` links the other
way, the dependents. The walk is SQL, `ListPrerequisites` and its mirror `ListDependents` in
[`links.sql`](../../backend/internal/store/queries/read/links.sql):

- **Each link once per depth, never each path.** The recursive part keeps `(ticket, the ticket it
  blocks, depth)` with `UNION`, so a dense graph costs its links times the depth. A walk that
  carried each path — the context's before this route — costs the number of paths: forty tickets
  in eight layers of five, every one blocking the five below, are 5^8 paths and took eleven seconds
  for one request; the same graph answers in milliseconds now
  (`TestPrerequisiteTreeCostsItsLinksNotItsPaths`).
- **Eight levels** (`treeDepth`), depth first, siblings by id — in the order they were filed.
- **A ticket under two others** stands in full once, under the first of them nearest the root (the
  smallest depth, then the earliest filed), and under each other one as a `repeated` leaf, without
  what lies behind it. The context's `## Prerequisites` leaves the repeated ones out.
- **Visibility.** Every step calls `app_ticket_visible` on the ticket it steps to: the walk never
  passes a ticket the caller cannot see, so that ticket and whatever lies only behind it are absent,
  and nothing is counted for them ([ADR 0065] D5).
- **`open`** counts the open tickets of the whole tree, each once, on every page (a window count
  before the page is cut); `settled` marks done and dropped.
- **Paging.** The cursor carries the node's path — its ids from the first level down, sixteen
  bytes each in base64url, which at eight levels keeps the cursor within the document's 512
  characters (`TestATreeCursorFitsTheDocument`) — bound to the ticket and the direction.

## Transitions

`POST …/transitions` with `from`, `to` and what the move needs
([`transitions.go`](../../backend/internal/api/transitions.go)). `from` must be the current
state, else `409 state_conflict` with the current state in `errors[]` ([ADR 0045] D2). The
matrix is [`domain.ClassifyMove`](../../backend/internal/domain/transition.go)
([ADR 0009]), given the state a blocked ticket came from or a done ticket was done from:

| Move | From → to | Needs | An agent needs |
|---|---|---|---|
| forward | `filed` → `analysed` → `decided` → `in-progress` → `review`, one step | — | `decide` for → `decided` |
| backward | `in-progress` → `decided` or `analysed`; `decided` → `analysed`; `review` → `in-progress` | `reason` | — |
| block | `filed`, `analysed`, `decided`, `in-progress`, `review` → `blocked` | `reason` (the block's text) and `block` | — |
| unblock | `blocked` → the state it came from | — | — |
| done by hand | any open state, `blocked` included → `done` | `note`, the verification | `close`, and `from` `in-progress` or `review` |
| withdraw | `done` → the state it was done from (`done_from`) | `reason` | — |
| drop | any open state, `blocked` included → `dropped` | `reason` | `drop` |
| reopen | `dropped` → `filed` | `reason` | — |

Any other pair is `409 state_conflict`, and so is every transition out of a ticket done by its
stages ("lower a stage to reopen it"): its way out is a `PATCH` ([progress](#progress)). Done by
its stages is a done ticket without children whose three stages are full and whose
`done_by_hand` is false; every other done is by hand (`doneByHand`, `domain.DoneByStages`). A
`block` on another move, or `override_prerequisites` on anything but done, is `400`. An agent
closing from another state is `403 agent_forbidden`, detail `close covers in-progress and
review: …` (`mayClose`, which the done act of the stages calls as well).

- **Effects** (`TransitionTicket`): done sets `done_at`, `done_from` — the state it left — and
  `done_by_hand`, and leaves the stages as they are; leaving done clears all three; reaching
  `decided` sets `decided_at` each time; entering `blocked` stores where it came from and the
  block, and every other move clears them — but done from `blocked` keeps the block, and the
  way back to `blocked` takes it back (`keepsBlock`; the `CHECK`s `tickets_block_check` and
  `tickets_block_kept_check` of migration 19). Done and dropped clear `rank`; a reopen and a
  withdrawal rank the ticket at the bottom (`reopenRank`, before the transition writes
  anything); every other move keeps the rank, `blocked` included ([rank](#rank)).
- **Withdrawing a done by hand** (`MoveWithdraw`) returns the ticket to `done_from` — a ticket
  closed by a release before the stages has none and returns to `in-progress` (`origin`) —
  unless it has no children and its three stages are full
  (`domain.WithdrawalStaysDone`): then it stays done, by its stages, and the act is `updated`,
  `done_by_hand` from true to false, with the reason (`keepDoneByStages`, `EndDoneByHand`).
- **A block that names a ticket** — required for kind `ticket` — reads it through the predicate
  and adds `<that ticket> blocks <this one>` when the link is missing, with its acts, lock and
  cycle check.
- **Prerequisites** ([ADR 0012] D7): the done act — by hand or by the stages — over open direct
  `blocks` sources the caller can see is `409 open_prerequisites`, listing them in `errors[]`
  under the field that closes (`/to`, or the stage of the `PATCH`), unless
  `override_prerequisites` with a reason — a person's act, hard-off for agents (`closeOver`).
  The act then names the overridden keys and carries them in `Refs`. Every ticket carries the
  count of those prerequisites as `open_prerequisites`, a subquery of the ticket's columns
  under the predicate: a prerequisite the caller cannot see is never counted.
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
  `NextTicketNumber`; a move and a return to an open state — a reopen, a withdrawal, a lower
  stage that reopens — take it with `LockProjectRank` first. Every key is
  computed from keys read after the lock, over every ticket of the project — those the caller
  cannot see included (`LastRank`, `NextRankedTicket`, `PreviousRankedTicket`) — so two writes
  never compute a key from the same neighbours and no key is handed out twice.
- **A filing, a reopen, a withdrawal and a lower stage that reopens** get `rankAtBottom`: the key
  after the greatest of the project — for a filing the end of its horizon, since a horizon's group
  is the rank read over it.
- **A filing with a place** — `after` or `before`, a number of the same project — gets
  `rankBeside` ([ADR 0014] D2 as amended 2026-10-04): under the lock, the neighbour the caller can
  see, open and in the horizon the ticket is filed into (else `400` at the pointer, or `409
  state_conflict` for a done or dropped one), and a key strictly between its key and the next key
  of any ticket on that side, as a move computes it. The filing act names the neighbour.
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
| answer through a token | the token is `answered_by_token`, an agent's or the person's own; every answer sets or clears it, so a changed answer carries its own ([ADR 0036] D6) |
| withdraw | the asker; an agent only what an agent asked |

A withdrawn question takes no answer; an answered or withdrawn one no edit.

## Comments and the activity list

[`comments.go`](../../backend/internal/api/comments.go), [ADR 0015]:

- **The thread** is oldest first, `order=desc` reverses it. A comment is written by a person, or
  by an agent in its person's name with the agent mark; one written through a token carries the
  token as well, and so does each revision ([who made an act](#who-made-an-act)).
- **Edits** keep the previous text in `comment_revisions`. **Withdrawal** keeps the entry and
  hides its text: `body` is `null` in every answer and the revision list is empty. Nothing is
  deleted. A person changes their own comments and those their agents wrote; an agent only those
  an agent of the same person wrote; a tenant administrator withdraws any comment and edits none
  (D3, D4).
- **Comment texts never enter the audit record**, which cannot forget: `commented`, `edited`
  and `withdrawn` carry no text.
- **Mentions** are a list of person ids beside the text, `comments.mentions` (migration 40,
  [ADR 0015] D5): `checkMentions` admits each like a question's `asked_of` — a member of the tenant
  who sees the ticket (`CanSeeTicket`) — and refuses the first that is not at `/mentions/<i>`; the
  API reads no text, so a name typed without the list mentions nobody. A new comment's act tells the
  persons it mentions `mentioned` before it tells the watchers `commented`, and one act tells a
  person once about a ticket ([who is told](#who-is-told)). An edit without `mentions` keeps the
  list; with one it replaces it, checks and tells only the persons it adds — those it keeps were
  checked when they came — and a person it drops watches by it no more. A withdrawn comment answers
  `mentions: []`, and its mentions watch by it no more either. An explaining comment mentions
  nobody.
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
as entity; the stake carries the mark of the write that set it ([who made an act](#who-made-an-act)).

## Progress

Three stages, each 0 to 100 in steps of five ([ADR 0017] D2): `progress_refinement`, `progress`
— the implementation stage, under the name of the first release — and `progress_review`
(migration `000019_progress_stages`, which backfilled them: refinement 100 from `decided` on,
`blocked` from one of those included, review 100 when done). `PATCH` sets each in every state
but `dropped` on a ticket without children; on a dropped ticket, or one whose stages are
derived, it is `409 state_conflict` with the current value (`applyStages`).

A ticket with children shows each stage derived from the same stage of its children
(`ticket_derived_stage(tenant, id, stage)`, migration 19; `progress_derived`,
`progress_refinement_derived`, `progress_review_derived`): the mean weighted by effort (XS 1,
S 2, M 3, L 5), a dropped child left out, a done child counted as 100 in each stage, rounded to
the nearest five with halves up; 0 when every child is dropped. `refreshProgress` derives them
again up the ancestors, as far as a value changes, after a child is filed, re-parented, changes
effort or a stage, or moves; the version stays. When the last child leaves, each own value
starts at the last derived one. A ticket shows its stages as they are — derived while it has
children, else its own — done or not (D3, D5); `progress_derived` says they are derived.
`progress_derived` alone says whether there are children: the release before the stages, run
over this schema in a rollback ([ADR 0028] D4), derives the implementation stage alone, and when
a parent's last child leaves it clears `progress_derived` and leaves the derived refinement and
review as they were. So those two count only while `progress_derived` is set — in `stagesOf`,
and for a child in `ticket_derived_stage`.

The stages move the state ([ADR 0009] D5), decided by the pure
[`domain.EffectOfStages`](../../backend/internal/domain/progress.go):

- **The done act.** A `PATCH` that brings the last of the three stages of an open ticket without
  children to 100 makes it done in the same transaction: `done_by_hand` false, `done_from` the
  state it left, the rank taken away, a `transitioned` act with the note beside the `updated`
  act. It needs `note` (`400` at `/note`, the detail saying the change completes the ticket),
  the prerequisite rule above, and of an agent `close` and a ticket in `in-progress` or `review`
  (`stageInputs`, `mayClose`); without them nothing is written and the stage keeps its value.
  An open ticket whose three stages are full already — a parent whose last child left with its
  children's stages full — is closed by hand: no `PATCH` brings a stage to 100 then.
- **The reopen.** A `PATCH` that lowers a stage of a ticket done by its stages returns it to
  `done_from` with a `reason` (`400` at `/reason` otherwise), ranked at the bottom under the
  project's lock, which `planStageMove` takes before any ticket row is written; the act is
  `transitioned` with the reason.
- **Done by hand** keeps the ticket done while its stages change, lowered or filled. The
  view's `done_by_hand`, the refusal of a transition out and the effect of a stage write read
  done by hand as the [transitions](#transitions) define it, not the column alone. A done
  ticket short of full without the flag comes from the release before the stages: its done in
  a rollback, or a parent it closed that lost its last child, which migration 19 backfills as
  done by hand.
- `note` and `override_prerequisites` on a `PATCH` that is not the done act, and `reason` on one
  that moves no state, are `400`.
- **A parent is never done by its stages.** Its stages take no write, and a done ticket that
  gains children is done by hand from then on (`RefreshDerivedProgress` sets `done_by_hand`).

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
- Time entries appear neither in a ticket's activity nor on the event stream; an entry and each
  revision carry the token they came through ([who made an act](#who-made-an-act)), which is where
  a booking through a token shows.

## Who made an act

[ADR 0036] D1, D6: the actor of an act through a token is its person, and the act says it came
through the token. Beside the agent mark (`agent`, `asked_by_agent`, `recorded_by_agent`,
`reporter_agent`), the rows record the token's id and name, copied from the principal when the act
is written (`actAgent`, `actToken`):

| Row | Token columns | API |
|---|---|---|
| `tickets`, the filing | `reporter_token_id`, `reporter_token_name`, beside `reporter_agent` | `Ticket.reporter_agent`, `Ticket.reporter_token` |
| `ticket_interest`, the stake as last set | `token_id`, `token_name`, beside `agent` | `Interest.agent`, `Interest.token` |
| `audit_events` | `token_id`, `token_name` | `Activity.token`; the tenant's audit view `token_name` |
| `comments`, `comment_revisions` | `token_id`, `token_name` | `Comment.token`, `CommentRevision.token` |
| `attachments` | `token_id`, `token_name` | `Attachment.token` |
| `questions` | `asked_by_token_id`, `asked_by_token_name`; `answered_by_token_id`, `answered_by_token_name` | `Question.asked_by_token`, `Question.answered_by_token` |
| `time_entries`, `time_entry_revisions` | `token_id`, `token_name` | `TimeEntry.token`, `TimeEntryRevision.token` |

A session's act — the chat's included — writes none. The name is a copy because a reader may not
read another person's `tokens` row ([ADR 0021] D6) and must still read it after a revocation; it is
`null` only on an audit row written before migration 27, which named the token by its id alone.
A stake's write sets its mark or clears it, so the stake shows who set it as it stands. The context
document and the summary of `session_start` name a plain token's act `through the token <name>`
where they name an agent's `via <agent>` (`markdown.via`, `tools.actLine`). A link's creator and an
urgency override's setter carry no mark of their own — no view of the UI shows them; the activity
marks their acts ([tokens.md H-50](../security/tokens.md#h-50)).

## Who is told

A person's inbox ([ADR 0020]) holds a notification for each act of D2 that concerns them: a ticket
assigned to them, a question asked of them, a question they asked answered, a state change of a
ticket they watch — a block's reason comes only with a move into `blocked` —, a comment on one, a
ticket that blocks one they watch reaching `done` or `dropped`, and an `urgent` stake on a ticket
assigned to them. The watchers are everyone with a stake of any weight, the assignee, the reporter
and whoever asked or was asked an open question on the ticket, or is mentioned by a comment on it
that is not withdrawn ([ADR 0013] D6, [ADR 0015] D5), and a comment that mentions a person tells
them that they are mentioned. A person's own act tells them nothing, nor does their agent's, and a
person who cannot see the ticket is told nothing of it ([ADR 0065] D5); an act that names a person
for two reasons — a watcher the comment mentions — tells them once, by the first. The table and the
store's side are [data-access.md](data-access.md#notifications).

## Not built

The score of [ADR 0014] D3–D5 is not built — no score beside the rank; the person-level lists,
which it would order, are ordered by the tenant, the project and the project's rank meanwhile — nor
is the rebalancing of the rank's keys. There is no `deleted_at` and
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
[ADR 0020]: ../adr/0020-notifications-are-an-in-app-inbox-per-person.md
[ADR 0021]: ../adr/0021-row-level-security-is-the-second-line-of-tenant-isolation.md
[ADR 0022]: ../adr/0022-uuidv7-everywhere-sequences-only-for-ticket-numbers.md
[ADR 0024]: ../adr/0024-deletion-tickets-are-soft-deleted-and-purged-projects-archived-people-deactivated-tenants-deleted-explicitly.md
[ADR 0028]: ../adr/0028-migrations-only-go-forward-no-down-files-expand-before-contract.md
[ADR 0034]: ../adr/0034-three-tenant-roles-an-optional-project-restriction-no-implicit-role-for-the-global-administrator.md
[ADR 0035]: ../adr/0035-personal-access-tokens.md
[ADR 0036]: ../adr/0036-a-token-acts-as-its-person-an-agent-flag-is-the-floor-the-agent-header-only-narrows.md
[ADR 0043]: ../adr/0043-agent-capabilities-are-chosen-per-token-the-default-is-everything-reversible-and-attributable.md
[ADR 0045]: ../adr/0045-idempotency-put-where-it-is-free-a-required-key-on-agent-posts-stored-with-the-act.md
[ADR 0050]: ../adr/0050-optimistic-concurrency-a-version-per-entity-if-match-where-a-write-overwrites.md
[ADR 0065]: ../adr/0065-a-confidential-flag-replaces-the-file-name-embargo-set-automatically-lifted-only-by-a-person.md
[ADR 0066]: ../adr/0066-repositories-are-bound-by-their-normalised-remote-identity-creation-proposed-by-the-agent-confirmed-by-the-person.md
