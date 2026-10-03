# ADR 0019: No Sprints and No Milestones — Continuous Flow, Measured by Calendar Week, With Optional WIP Limits per State

## Status

Accepted. Date: 2026-09-29. Decided by the owner as the answer to the catalog question
"iterations or sprints?": continuous flow, over sprints, over milestones (the
recommendation), and over deferring milestones.

**Partly built** (phase 2, 2026-10-02): D1 (no time box exists) and D3 — WIP limits per state
stored on the project, advisory, refusing nothing. D2's dashboard and D4's release ticket
arrive with the views.

## Context

The owner works across many tenants and projects and reorders priorities daily. A sprint is
a per-project ritual every two weeks — ten projects, ten rituals — with a carry-over step
that a flow model does not have. A milestone would have given a release date a home without
the ritual; the owner chose to keep the model without any time box at all. Order comes from
the rank ([ADR 0014](0014-rank-is-the-decision-score-is-the-warning.md)), progress from the
states ([ADR 0009](0009-ticket-states-are-the-frontmatter-states-plus-blocked.md)) and the
percentage ([ADR 0017](0017-effort-is-a-size-progress-is-a-five-step-percentage-and-time-is-booked-by-people.md)),
and the dashboard ([ADR 0018](0018-the-views-of-the-first-release.md) D6) measures throughput
and lead time.

## Decision

**D1 — There is no time box.** No sprint, no iteration, no milestone entity, no field on a
ticket that names one. Nothing is planned into a period and nothing is carried over.

**D2 — The dashboard measures by calendar week and by a chosen period.** Throughput is
`done` per ISO week; lead time is measured over a rolling window; both as ADR 0018 D6 has
them. No metric is "per sprint".

**D3 — A project may set a WIP limit per state.** An optional integer per state
(`analysed`, `decided`, `in-progress`, `blocked`); the board shows the column count against
the limit and marks an exceeded column. A limit never blocks a transition: it is
information, not a gate.

**D4 — A release date lives in the ticket that is the release.** A ticket of type `task` or
`feature` named for the release, with the work that gates it linked by `blocks`
([ADR 0012](0012-four-typed-directed-links-within-a-tenant.md)); the urgency rule "gates the
release" ([ADR 0010](0010-the-frontmatter-vocabularies-become-ticket-columns.md) D3) reads
that link. The date itself is text in the release ticket's title or body; cowork does not
model it.

## Consequences

- No planning ritual, no sprint entity, no closing step; the model stays the rank, the
  states and the links.
- "What must be done before the client's release" is answered by the `blocks` graph of the
  release ticket and the score, not by a filter on a date. The dashboard cannot show
  "open until milestone X"; the release ticket's children and blockers are the view.
- The urgency rule "gates the release" has a concrete object (a `blocks` link to a release
  ticket) and needs no label vocabulary for it.
- If a client insists on a date-driven overview, a milestone is the smallest amendment; this
  record says it is not there.

## Alternatives Considered

- **Iterations per project** with assignment, closing and carry-over. Scrum vocabulary and a
  ritual per project; the carry-over is the work a flow model does not have. Lost.
- **Milestones** — the recommendation: a date and a name per project, tickets optionally
  attached, no closing. The smallest entity that answers "what by when" and gives the urgency
  rule an object. Lost by the owner's decision; D4 gives the rule its object through a link
  instead.
- **Continuous flow now, milestones later.** The owner chose not to keep the door marked.

## Residual risks

- A release date as text (D4) is not queryable; a dashboard tile "next release" cannot exist
  without an amendment.
- D3's WIP limits are advisory; a team that wants them enforced needs an amendment, and
  ADR 0009's rule that a transition is a person's act with a reason argues for keeping them
  advisory.

## References

- [ADR 0014](0014-rank-is-the-decision-score-is-the-warning.md) — order without a time box
- [ADR 0018](0018-the-views-of-the-first-release.md) D6 — the metrics by week and period
- [ADR 0012](0012-four-typed-directed-links-within-a-tenant.md), [ADR 0010](0010-the-frontmatter-vocabularies-become-ticket-columns.md) D3 — the release ticket and the rule that reads it
