# ADR 0018: The Views of the First Release — Backlog, Board, Person Lists, Tenant Board With Swimlanes, Saved Filters, and a Fixed Dashboard

## Status

Accepted, amended 2026-10-01 (D1, D2: the prerequisite count on cards and the prerequisite
tree on the detail page, [ADR 0012](0012-four-typed-directed-links-within-a-tenant.md) D6)
and 2026-10-03 (D1: the backlog grouped by urgency; the board's columns as a view over the
states, its `next` column and the urgencies its other columns hold; D2: the three progress
stages and done by hand) and 2026-10-04 (D1: a project opens on its board, the Board tab left of
the Backlog tab; the backlog's groups are the horizons of ADR 0010 D3 as amended the same day)
and 2026-10-05 (D3: which tickets "next for me" lists — the owner's answer to the question that
the records gave the list its scope and its order but not its members, whether it holds the
person's own open tickets only, which would repeat "assigned to me", their own and the unassigned
open tickets of the projects they see, or every open ticket they can see, colleagues' assigned work
included; the owner chose the second, as recommended: it is the set a person picks from, and the
one `session_start` offers an agent; D4: the tenant board's columns are the project board's of D1), and made concrete
2026-10-05 (D6: one route, the definition of each tile, what the period bounds, the front page
beside the tiles; a time booking reaches the dashboard at its next reload, settled on the
recommendation, the owner reviewing the result), and amended 2026-10-06 by the owner (D5: a tenant
administrator unshares or deletes another person's shared filter).
Date: 2026-09-29. Decided by the owner as the answer to the catalog question "which views are
v1?": the widest option — the minimum the earlier records require, plus a
tenant-wide board with swimlanes per project, saved filters, and dashboards. The
recommendation was the minimum plus saved filters without the swimlane board and without
dashboards. The dashboard's fixed tile set (D6) and the board's drag rules (D4) are this
record's proposal for implementing the choice.

The amendment of 2026-10-03 is the owner's plan for the two views of a project. The backlog
stays a table, its open tickets grouped by `now`, `next` and `later`; asked where `release`
and `icebox` go, the owner chose groups of their own in the order of
[ADR 0010](0010-the-frontmatter-vocabularies-become-ticket-columns.md) — `release` on the
board as `now` is, `icebox` off it as `later` is — over three groups that hold the other two
with a badge, and over a board of `now` and `next` alone. The board is for the current work:
the columns Refinement, Ready, In Progress, Blocked and Review, which the owner chose as a view
over the states of [ADR 0009](0009-ticket-states-are-the-frontmatter-states-plus-blocked.md)
with a new state `review`, over renaming the states and over a column per state; no column
for `done`, which the owner found a waste of space; left of them a column `next`, a dimension
of its own, with compact cards in whatever state and a button that makes a ticket `now`; on
every card the effort as a T-shirt-size icon and the bar of the current one of the three
progress stages of [ADR 0017](0017-effort-is-a-size-progress-is-a-five-step-percentage-and-time-is-booked-by-people.md)
D2. The rest of the amended D1 — the drags between the columns, a child's place outside its
parent's group, the count that stands in for `done` — is this record's proposal and stays
open to objection until the board is built.

The amendment of 2026-10-05 is the owner's answer to which columns the tenant board has, put
when it was built, since D4 still read "columns are the states" after D1's amendment of
2026-10-03 had given the project board its columns. The owner chose **the project board's
columns in every swimlane** — `next`, Refinement, Ready, In Progress, Blocked and Review, over the
same tickets, one model for both boards, so that a card stands in the same column on both — over
one column per state as D4 read, which would have been a second model with the `done` column the
owner turned down on the project board, and over the project board's five columns without
`next`, which would have left `next` to each project's board.

The amendment of 2026-10-06 is the owner's answer to what becomes of a shared filter whose owner
left the tenant, which stayed shared under the name of a person who is no member any more, and
which only that person could change. The owner chose that a tenant administrator may unshare or
delete **any** shared filter of the tenant — such a filter among them —, each a recorded act, and
may neither change another person's filter otherwise, its name or its conditions, nor touch one
that is not shared. What the act needs beyond the role is not part of the answer and follows from
two records as they stand: `admin` scope, the scope of what the `admin` role allows
([ADR 0035](0035-personal-access-tokens.md) D3), as an administrator's withdrawal of another
person's comment takes it; and no agent, since
[ADR 0043](0043-agent-capabilities-are-chosen-per-token-the-default-is-everything-reversible-and-attributable.md)
D3 keeps every administration act and `admin` scope from an agent.

**Partly built** (phase 3, 2026-10-03): D1 as amended 2026-10-04 — the project opening on its
board, whose tab stands left of the backlog's, and the backlog's groups named as horizons
(2026-10-04); D1 as amended 2026-10-03 — the backlog as a table
grouped by urgency in the project's rank ([ADR 0014](0014-rank-is-the-decision-score-is-the-warning.md)),
loaded with the cursor, its rows dragged within and between the groups with the urgency override
or its withdrawal and moved by a row menu from the keyboard, `release` and `icebox` offered as
drop zones docked at the foot of the window while a row is dragged, the closed tickets on
request, filing; the board with the column `next` and its Now button, the columns Refinement,
Ready, In Progress, Blocked and Review with their WIP counts, the count of the tickets done in
the last fourteen days (the filter `done_after`,
[ADR 0049](0049-filters-are-explicit-repeatable-query-parameters-no-query-language.md) D1), cards
with the size, the bar of the current stage, the block and the count of the open tickets that
block a card directly and that the caller can see (`open_prerequisites`,
[ADR 0012](0012-four-typed-directed-links-within-a-tenant.md) D6 as amended 2026-10-03), and
drags between the columns as transitions. D2's detail page with its fields to edit, the three stage sliders with
the note when the last stage fills, the moves with done by hand and its withdrawal, questions
with the answer form, links, interest, comments, activity, attachments and time (the body as
text until it is rendered), and since 2026-10-04 the title and the body edited there, the parent
and the horizon chosen there, the confidential flag for a tenant administrator, the prerequisite
tree with its upward reading, comments edited, their earlier texts and their withdrawal, an open
question's text edited, uploads to a comment, the preview of raster images and the correction of
time; the time report; ~~and as the tenant's
front page, until D6's dashboard, each project's open tickets by state with the tickets updated
last~~ *(replaced 2026-10-05 by D6's dashboard, below, which carries both)*. *(2026-10-04:)* D3's "assigned to me", "open decisions" and the inbox, each across every tenant
of the person as a union of per-tenant reads with the tenant beside each key, in the navigation for
every person (`/me/assigned`, `/me/decisions`, `/me/inbox`, [`features/me/`](../../frontend/src/app/features/me/));
~~"assigned to me" and "open decisions" are ordered by the tenant, the project and the project's rank
until the score exists — the interim order written in [ADR 0014](0014-rank-is-the-decision-score-is-the-warning.md)'s
Status.~~ *(2026-10-05:)* D4 as amended the same day — the tenant board at `/t/{slug}/board`, in
the navigation beside the tenant's front page: a swimlane per non-archived project the person sees,
each with the project board's columns, cards, counts against its own project's WIP limits, card
menu and dialogs, made from the same parts as the project board
([`board-columns.ts`](../../frontend/src/app/features/project/board-columns.ts),
[`board-moves.ts`](../../frontend/src/app/features/project/board-moves.ts),
[`board-list.ts`](../../frontend/src/app/features/project/board-list.ts),
[`tenant-board.ts`](../../frontend/src/app/features/tenant/tenant-board.ts)); a swimlane loads its
list only while it is in view or a screen's height from it; the project filter is the address's
repeated `project`; a drag between swimlanes is refused visibly — the swimlane under the card says
no, and a toast says that a ticket never changes project on a board — and holds every swimlane
still while a card is dragged. *(2026-10-05:)* D7's search — a search box in the top bar that
searches, inside a tenant, that tenant first and offers every tenant of the person, anywhere else
every tenant, and a page of results with where each hit was found and its snippet, linked to the
comment or question it is in ([`features/search/`](../../frontend/src/app/features/search/),
[ADR 0025](0025-search-is-postgresql-full-text-under-the-same-policy-as-the-data.md)); and D2's
body, comments, options and answers shown as the server rendered them
([ADR 0011](0011-a-ticket-is-a-markdown-body-plus-first-class-open-questions.md) D6).
*(2026-10-05:)* D5 on the backlog: saved filters as named parameter sets of the lists
([ADR 0049](0049-filters-are-explicit-repeatable-query-parameters-no-query-language.md) D6, D7), a
person's own or shared with the tenant with the owner beside it, applied, saved, shared and deleted
from the backlog's filter bar ([`api/filters.go`](../../backend/internal/api/filters.go),
[`features/project/saved-filters.ts`](../../frontend/src/app/features/project/saved-filters.ts)).
*(2026-10-05:)* D1's score marker — among the siblings of each horizon's group, "score says higher"
or "lower" — and the sort by the score, which the page asks before it sends
([ADR 0014](0014-rank-is-the-decision-score-is-the-warning.md) D3); D3's "next for me" as amended,
`/me/next`, first in the navigation and the start page `/`
([ADR 0023](0023-the-tenant-is-in-the-path.md) D4 as amended 2026-10-05), each ticket beside its
tenant and its place in its project's backlog; "next for me", "assigned to me" and "open decisions"
in the score's order ([`my-tickets.ts`](../../frontend/src/app/features/me/my-tickets.ts),
[`backlog.ts`](../../frontend/src/app/features/project/backlog.ts)).
*(2026-10-05:)* D5 on the tenant list view, built on the recommendation, the owner reviewing the
result: the tenant's tickets across its projects at `/t/{slug}/tickets`, in the navigation beside
the tenant board — a table over `GET …/tickets`, newest first, the project beside each key, in
numbered pages ([ADR 0048](0048-cursor-pagination-on-every-list-numbered-pages-on-tables.md) D2) —,
whose address carries every filter of ADR 0049 D1, the backlog's filters and the project as the
selects of its bar, and to which saved filters apply as to the backlog, `project` included, through
the same filter bar ([`features/tenant/tenant-tickets.ts`](../../frontend/src/app/features/tenant/tenant-tickets.ts)).
*(2026-10-06:)* D5 on the tenant board, built on the recommendation, the owner reviewing the result:
the same filter bar beside the project filter; a saved filter's projects go into the address — one
it excludes, `!OPS`, leaves its swimlane out —, every other condition to each swimlane's list as a
pre-filter ([ADR 0049](0049-filters-are-explicit-repeatable-query-parameters-no-query-language.md)
D6), its `horizon` narrowing the board's `now`, `release` and `next` and never widening them, and
`include_terminal`, which would only add the closed tickets a board never shows, left out and named
under the bar ([`features/tenant/tenant-board.ts`](../../frontend/src/app/features/tenant/tenant-board.ts)).
A swimlane under a filter counts the cards the filter lets through, against the WIP limits too.
*(2026-10-06:)* D5 as amended the same day: `PATCH …/filters/{filter}` with `{"shared": false}` and
nothing else, and `DELETE …/filters/{filter}`, take another person's shared filter from a tenant
administrator with `admin` scope, never from an agent (`mayChangeFilter` in
[`api/filters.go`](../../backend/internal/api/filters.go)), each recorded as the owner's acts are;
[migration 39](../../backend/internal/store/migrations/000039_saved_filters_moderated_by_administrators.up.sql)
widens the restrictive policies of `saved_filters` for exactly that; the filter bar offers an
administrator *Stop sharing* and *Delete* on another person's filter it applies, at once as the
owner's own acts are ([`features/project/saved-filters.ts`](../../frontend/src/app/features/project/saved-filters.ts)).

**Built** (phase 3, 2026-10-05): D6 as made concrete the same day — `GET
/api/v1/tenants/{tenant}/dashboard` ([`dashboard.go`](../../backend/internal/api/dashboard.go),
[`queries/read/dashboard.sql`](../../backend/internal/store/queries/read/dashboard.sql)), each
tile's definition in its field of the API document's `Dashboard`, every query under the visibility
predicate and the deletion filter of [ADR 0024](0024-deletion-tickets-are-soft-deleted-and-purged-projects-archived-people-deactivated-tenants-deleted-explicitly.md)
D1, and each tile's definition, its hidden-ticket case and a deleted ticket an integration test
([`api_dashboard_test.go`](../../backend/test/integration/api_dashboard_test.go)); the tenant's
front page `/t/{slug}` is the dashboard ([`features/tenant/dashboard.ts`](../../frontend/src/app/features/tenant/dashboard.ts)),
live through the event stream at most once a second on its own tenant's events, its filters in the
page's address, its
charts bars in CSS over the preset's tokens. Looked at as the production build against a mocked
API in Chromium, in both schemes; ~~not verified: WebKit, real data, the owner's review, and how long
the queries take over a large tenant; no end-to-end test walks the page yet.~~ *(Amended
2026-10-06: an end-to-end path walks it with two identities over the built images, in Chromium and
WebKit and both schemes — the member's tiles leaving out a confidential ticket and a project
restricted away from the member, a filter chosen in the page kept in the address across a reload, a
ticket one identity moves moving the other's tile within five seconds, a deleted ticket leaving both
— and a picture per browser and scheme compares it
([ADR 0056](0056-end-to-end-playwright-against-the-built-containers-with-two-identities.md) D3).
Not verified: the owner's review, and how long the queries take over a large tenant.)*

## Context

Earlier records already require a number of views: a ranked backlog with the score marker
([ADR 0014](0014-rank-is-the-decision-score-is-the-warning.md)), the person-level lists
across tenants ([ADR 0005](0005-a-tenant-is-a-client-organisation-and-the-isolation-unit.md)
D3, ADR 0014 D5), the open decisions ([ADR 0011](0011-a-ticket-is-a-markdown-body-plus-first-class-open-questions.md)
D3), a member list ([ADR 0004](0004-cowork-is-a-team-product.md)), the time report
([ADR 0017](0017-effort-is-a-size-progress-is-a-five-step-percentage-and-time-is-booked-by-people.md)
D10), the comment thread and activity list ([ADR 0015](0015-comments-are-a-thread-and-activity-is-a-separate-list.md)).
The owner's founding problem is the overview across many undertakings; the owner chose to
have that overview in every form at once rather than grow into it.

## Decision

**D1 — Per project: the ranked backlog and the board.** The backlog orders by rank, marks
the score's disagreement, indents children under their parent, and is where rank is dragged.
The board has ~~one column per state of [ADR 0009](0009-ticket-states-are-the-frontmatter-states-plus-blocked.md)
in order~~ *(amended 2026-10-03: the columns below)*, shows the tickets that carry work (the leaves, [ADR 0008](0008-five-ticket-types-and-an-optional-parent-in-the-same-project.md)
D4) with type, severity, security class, assignee, progress bar, block reason and the count
of open prerequisites *(added 2026-10-01)* on the card; a drag between columns is a transition and asks for the reason or note the transition
requires.

*(Amended 2026-10-03.)* **The backlog is a table that groups its open tickets by urgency**,
in the order of [ADR 0010](0010-the-frontmatter-vocabularies-become-ticket-columns.md) D1 —
`now`, `release`, `next`, `later`, `icebox` — each group a section of the one table, ordered
by rank; `release` and `icebox` show while they hold a ticket or while a ticket is dragged. A
drag within a group moves the rank. A drag into another group moves the rank and sets the
urgency to the group's value: an override, or the override's withdrawal where the group is
the derived value (ADR 0010 D3, whose reason a person may leave out). A child is indented
under its parent where both sit in one group; in another group its row names the parent.
Closed tickets are in no group; the table shows them on request, below the groups.

*(Amended 2026-10-04.)* **A project opens on its board**, and the Board tab stands left of the
Backlog tab: the board is where the current work is carried out and what a person looks at first.
**The backlog's groups are the five horizons** of [ADR 0010](0010-the-frontmatter-vocabularies-become-ticket-columns.md)
D3 as amended 2026-10-04 — planning categories, not states: a ticket stands in any of them in
whatever state, a drag moves it freely within and between them, and the order within each is the
person's, the project's rank. An agent re-sorts with the same two acts, and files a ticket into a
horizon at a place in it; without a place it lands at the end of its horizon, without a horizon in
`later` ([ADR 0014](0014-rank-is-the-decision-score-is-the-warning.md) D2 as amended 2026-10-04).
The owner's words, 2026-10-04.

*(Amended 2026-10-03.)* **The board shows the current work:** the open leaves of urgency
`now` and `release` (the latter marked) in the columns **Refinement** (`filed` and
`analysed`, the card telling which), **Ready** (`decided`), **In Progress**, **Blocked** and
**Review**, each ordered by rank, which the backlog owns; `later` and `icebox` are not on the
board. `done` is no column — the board's header counts the tickets done in the last fourteen
days and leads to them — and neither is `dropped`, which a ticket reaches from its detail.
**Left of them, the column `next`** holds the leaves of urgency `next` in whatever open state
they are, as compact cards, each with a button that sets the urgency to `now`; the ticket
then shows in the column of its state. A drag between the other columns is a transition of
ADR 0009 and asks for the reason, note or block it requires; a column the transition does
not allow takes no drop. Every card carries the ticket's effort as a T-shirt-size icon and
the bar of its current progress stage (ADR 0017 D2): Refinement shows refinement, In Progress
implementation, Review review, Blocked the stage of the state it came from, Ready none.

**D2 — The ticket detail:** frontmatter columns, ~~the progress slider~~ *(amended
2026-10-03: the three progress stages of ADR 0017 D2, each a bar with its slider, and done by
hand with its withdrawal, ADR 0009 D5)*, the assignee, the
Markdown body, the open questions with their answer form, links with their reverse views,
the prerequisite tree — everything that has to be done before this ticket can be finished,
transitively, with state, assignee and progress per node *(added 2026-10-01, ADR 0012 D6)* —
interest, attachments, the comment thread and the collapsible activity list, time entries.

**D3 — Per person, across every tenant they belong to:** "next for me" and "assigned to me"
ordered by score with the tenant shown beside the key, "open decisions" (asked of me, and open
in my tenants), and the inbox. Each is a union of per-tenant queries (ADR 0005 D3). *(Amended
2026-10-05:)* **"Next for me" lists the person's open tickets and the unassigned open tickets of
the projects they see** — what the person, or their agent, could take up next; another person's
ticket is not "for me". The owner's words, 2026-10-05.

**D4 — Per tenant: a board with one swimlane per project.** ~~Columns are the states~~
*(amended 2026-10-05: the columns are the project board's of D1 — `next`, Refinement, Ready, In
Progress, Blocked and Review —, in every swimlane, over the same tickets: the open leaves of the
horizons `now` and `release` in the columns of their states, those of `next` in `next`; one model
for both boards, so a card stands in the same column on both)*, rows the
non-archived projects, cards as in D1. A drag between columns is a transition; **a drag
between rows is refused** — a ticket never changes project on a board. Rank is not edited on
this board; the backlog owns rank. The board loads lazily per swimlane and offers a project
filter, because a tenant with many projects would otherwise render everything.

**D5 — Saved filters.** A saved filter is a named set of the list filter parameters (the API
record defines them), owned by a person, optionally shared with the tenant, applicable to
the backlog, the tenant list view and the tenant board. It is a parameter set, not a query
language. *(Amended 2026-10-06 by the owner:)* Its owner changes and deletes it, and a tenant
administrator unshares or deletes any shared filter of the tenant — one whose owner left the
tenant among them —, each a recorded act and an administration act (`admin` scope, no agent);
the administrator changes nothing else of another person's filter and nothing of one that is not
shared.

**D6 — Per tenant: a dashboard with a fixed set of tiles, filterable by project and by
period.** The tiles of the first release: open tickets by state; open by severity; open
`security: live` and `boundary` with the oldest named; `blocked` with the oldest block and
its reason kind; age distribution of open tickets; throughput (`done` per week, last eight
weeks); lead time (median `filed`→`done`, last thirty days); open decisions with the oldest
named; time booked in the period by project. No tile is configurable and no custom
dashboard exists; a new tile is an amendment of this record.

*(Made concrete 2026-10-05, built the same day; this record's proposal for the implementation,
open to objection like the definitions themselves, Residual risks:)* **One route answers all nine
tiles** — `GET /api/v1/tenants/{tenant}/dashboard`, read in one transaction — because the page
shows them together under one set of filters, one transaction gives counts that agree, and a
reload costs one request that a weak `ETag` answers `304` while nothing changed; nine routes would
have meant nine requests per reload and nine snapshots. **Each tile's definition is written in the
API document** (`components/schemas.yaml#/Dashboard`), which the code and its tests follow, and
every tile counts only what the caller can see ([ADR 0034](0034-three-tenant-roles-an-optional-project-restriction-no-implicit-role-for-the-global-administrator.md)
D4, [ADR 0065](0065-a-confidential-flag-replaces-the-file-name-embargo-set-automatically-lifted-only-by-a-person.md)
D4). The filters are the ticket lists' `project` of [ADR 0049](0049-filters-are-explicit-repeatable-query-parameters-no-query-language.md)
D6 — an archived project counted only when named — and the period, `from` and `to`, UTC days, the
thirty days that end today by default. **The period bounds the time booked; throughput's eight
ISO weeks and lead time's thirty days end with its last day** ([ADR 0019](0019-no-sprints-and-no-milestones-continuous-flow-with-optional-wip-limits.md)
D2); the open tiles stand as the tickets do at the request, since nothing records how the open
tickets stood on an earlier day. The definitions this needed: *open* is every state but `done` and
`dropped`; the oldest finding is the earliest filed; a ticket is blocked since its latest recorded
act into `blocked` ([ADR 0026](0026-one-append-only-audit-table-written-by-the-request-layer.md)),
or its last update where none is recorded; the age buckets are under 7, 30, 90 and 365 days and
older; throughput counts the tickets done now in the week of their `done_at` — a reopened ticket
nowhere, as the board's count of done tickets has it —, and lead time is the median from `opened_at`
to `done_at` of the tickets done in its window; the open decisions are every open question on the
counted tickets, whomever asked and whatever the ticket's state. **Beside the tiles** the front page
keeps what the interim front page showed — each project's open tickets by state, which is tile 1
per project, and the eight open tickets updated last — from the same answer; neither is a tile.
*(Made concrete 2026-10-05, settled on the recommendation, the owner reviewing the result:)* **a
time booking reaches the dashboard at its next reload**, not live: time entries stay off the event
stream ([ADR 0054](0054-server-sent-events-per-tenant-carry-keys-not-content-polling-is-the-fallback.md)
D4), and the time tile follows another act of the tenant, a `resync`, the fallback's poll or the
page opened again — over publishing a booking, which would need the stream to judge the visibility
of time, and over a timer per open dashboard. A deleted ticket counts in no tile until it is
restored, as it answers like a missing one everywhere ([ADR 0024](0024-deletion-tickets-are-soft-deleted-and-purged-projects-archived-people-deactivated-tenants-deleted-explicitly.md)
D1).

**D7 — Search.** Full-text over title, body, comments and question texts within a tenant,
from the tenant's pages; the person-level pages search across the person's tenants as a
union. *(Made concrete 2026-10-05, built on the recommendation, the owner reviewing the result:)*
inside a tenant the search box opens that tenant's results, one list in one order with one cursor,
and every tenant of the person is one link away; the alternative — one page with the tenant's hits
on top and the other tenants' under them — was not built, since it shows another client's tenant
before the person asks for it.

**D8 — Not in the first release, by this record:** custom dashboards, a portfolio view across
tenants beyond D3, Gantt or timeline views, a calendar.

## Consequences

- Phase 3 of the plan is the largest phase of the project: seven kinds of pages, drag
  interactions in two boards with different rules, a dashboard with nine queries. The plan
  was adjusted in the same change.
- D4's refusal to change project by drag keeps the board honest about what a drag means; a
  ticket that belongs elsewhere is moved in its detail, as a recorded act.
- *(Added 2026-10-03.)* A new ticket ~~derives `later`~~ *(amended 2026-10-04: is `later`
  unless it is filed into another horizon)* (ADR 0010 D3) and is not on the board
  until it is moved to `next` or `now` in the backlog: the backlog is where work is planned,
  the board where it is carried out. A `next` ticket that is already in progress stays in the
  `next` column, its state on the card.
- D6's fixed tile set means the dashboard is a set of tested queries, not a widget system;
  every tile has a definition a reader can check against the record.
- Live updates become a question with weight: two boards and a dashboard that poll are a
  visible load; the frontend record on live updates decides.
- The end-to-end tier grows: each view is a path a Playwright test walks with two
  identities.

## Alternatives Considered

- **The minimum the records require** (D1–D3, D7). Everything a working day needs, no
  tenant-wide overview except search. Lost.
- **The minimum plus saved filters, without the swimlane board and the dashboard** — the
  recommendation. The tenant overview as a filtered, score-ordered list. Lost by the owner's
  decision.
- **Configurable dashboards.** A widget system before a tile has proven useful. Lost to D6.
- **Dragging a card across swimlanes to change its project.** Easy to do by accident,
  silently moves a ticket out of its backlog and rank. Lost to D4.

## Residual risks

- Scope. Every view in this record is a page to build, test and keep; the owner is, today,
  the person who will use them all. The two-identity test rule of ADR 0004 applies to every
  one.
- The dashboard's definitions (lead time over thirty days, throughput over eight weeks) are
  first guesses; they are visible on the tile and amendable.

## References

- [ADR 0009](0009-ticket-states-are-the-frontmatter-states-plus-blocked.md), [ADR 0014](0014-rank-is-the-decision-score-is-the-warning.md) — the states and the rank the boards and backlog show
- [ADR 0005](0005-a-tenant-is-a-client-organisation-and-the-isolation-unit.md) D3 — the person-level unions
- [ADR 0011](0011-a-ticket-is-a-markdown-body-plus-first-class-open-questions.md), [ADR 0015](0015-comments-are-a-thread-and-activity-is-a-separate-list.md), [ADR 0016](0016-attachments-live-in-s3-compatible-storage-and-are-served-only-through-the-backend.md), [ADR 0017](0017-effort-is-a-size-progress-is-a-five-step-percentage-and-time-is-booked-by-people.md) — what the detail page shows
