# ADR 0018: The Views of the First Release — Backlog, Board, Person Lists, Tenant Board With Swimlanes, Saved Filters, and a Fixed Dashboard

## Status

Accepted, amended 2026-10-01 (D1, D2: the prerequisite count on cards and the prerequisite
tree on the detail page, [ADR 0012](0012-four-typed-directed-links-within-a-tenant.md) D6).
Date: 2026-09-29. Decided by the owner as the answer to the catalog question "which views are
v1?": the widest option — the minimum the earlier records require, plus a
tenant-wide board with swimlanes per project, saved filters, and dashboards. The
recommendation was the minimum plus saved filters without the swimlane board and without
dashboards. The dashboard's fixed tile set (D6) and the board's drag rules (D4) are this
record's proposal for implementing the choice.

**Not built.** The frontend is a shell with a version footer.

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
The board has one column per state of [ADR 0009](0009-ticket-states-are-the-frontmatter-states-plus-blocked.md)
in order, shows the tickets that carry work (the leaves, [ADR 0008](0008-five-ticket-types-and-an-optional-parent-in-the-same-project.md)
D4) with type, severity, security class, assignee, progress bar, block reason and the count
of open prerequisites *(added 2026-10-01)* on the card; a drag between columns is a transition and asks for the reason or note the transition
requires.

**D2 — The ticket detail:** frontmatter columns, the progress slider, the assignee, the
Markdown body, the open questions with their answer form, links with their reverse views,
the prerequisite tree — everything that has to be done before this ticket can be finished,
transitively, with state, assignee and progress per node *(added 2026-10-01, ADR 0012 D6)* —
interest, attachments, the comment thread and the collapsible activity list, time entries.

**D3 — Per person, across every tenant they belong to:** "next for me" and "assigned to me"
ordered by score with the tenant shown beside the key, "open decisions" (asked of me, and open
in my tenants), and the inbox. Each is a union of per-tenant queries (ADR 0005 D3).

**D4 — Per tenant: a board with one swimlane per project.** Columns are the states, rows the
non-archived projects, cards as in D1. A drag between columns is a transition; **a drag
between rows is refused** — a ticket never changes project on a board. Rank is not edited on
this board; the backlog owns rank. The board loads lazily per swimlane and offers a project
filter, because a tenant with many projects would otherwise render everything.

**D5 — Saved filters.** A saved filter is a named set of the list filter parameters (the API
record defines them), owned by a person, optionally shared with the tenant, applicable to
the backlog, the tenant list view and the tenant board. It is a parameter set, not a query
language.

**D6 — Per tenant: a dashboard with a fixed set of tiles, filterable by project and by
period.** The tiles of the first release: open tickets by state; open by severity; open
`security: live` and `boundary` with the oldest named; `blocked` with the oldest block and
its reason kind; age distribution of open tickets; throughput (`done` per week, last eight
weeks); lead time (median `filed`→`done`, last thirty days); open decisions with the oldest
named; time booked in the period by project. No tile is configurable and no custom
dashboard exists; a new tile is an amendment of this record.

**D7 — Search.** Full-text over title, body, comments and question texts within a tenant,
from the tenant's pages; the person-level pages search across the person's tenants as a
union.

**D8 — Not in the first release, by this record:** custom dashboards, a portfolio view across
tenants beyond D3, Gantt or timeline views, a calendar.

## Consequences

- Phase 3 of the plan is the largest phase of the project: seven kinds of pages, drag
  interactions in two boards with different rules, a dashboard with nine queries. The plan
  was adjusted in the same change.
- D4's refusal to change project by drag keeps the board honest about what a drag means; a
  ticket that belongs elsewhere is moved in its detail, as a recorded act.
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
