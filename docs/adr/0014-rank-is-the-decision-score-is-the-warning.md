# ADR 0014: Rank Is the Decision, Score Is the Warning — a Manual Rank per Project Wins, a Versioned Score Suggests and Orders the Person-Level Lists

## Status

Accepted. Date: 2026-09-29. Decided by the owner as the answer to the catalog question
"priority model?": both a manual rank and a computed score, the rank winning inside a
project, over rank only, score only, and score-with-pins. The formula of D4 was proposed with
the question; the owner chose the option without objecting to it, and it stands as version 1
until amended.

**Not built.** No `tickets` table exists.

## Context

A backlog of two hundred tickets across ten projects cannot be kept in order by hand alone:
a new `security: live` finding with `urgency: now` lands at the bottom until somebody drags
it, and the rank goes stale without anyone noticing. A backlog ordered by a formula alone
has the opposite defect: the order stops being a decision, and every deviation has to be
forced by bending the inputs — which [ADR 0010](0010-the-frontmatter-vocabularies-become-ticket-columns.md)
D3 ruled out for urgency. The inputs of any formula are already fixed by earlier records:
severity and urgency (ADR 0010), the `need` and `urgent` stakes of
[ADR 0013](0013-interest-is-a-persons-weighted-reasoned-stake-in-a-ticket.md), and age.
The person-level lists of [ADR 0005](0005-a-tenant-is-a-client-organisation-and-the-isolation-unit.md)
D3 span projects and tenants and therefore cannot have a shared rank at all.

## Decision

**D1 — Every project has a manual rank over its non-terminal tickets, and the rank wins.**
The backlog of a project is ordered by rank. A `blocked` ticket keeps its rank
([ADR 0009](0009-ticket-states-are-the-frontmatter-states-plus-blocked.md)); a terminal ticket
has none. Children are ranked among their siblings under their parent
([ADR 0008](0008-five-ticket-types-and-an-optional-parent-in-the-same-project.md) D4).

**D2 — The rank is a sortable string, not a position number.** A move writes one row (the
moved ticket's key between its new neighbours' keys) and never renumbers the list. A move is
a recorded act.

**D3 — Every non-terminal ticket has a computed score, shown beside the rank.** Where the
score order and the rank order disagree, the backlog marks the ticket ("score says higher" /
"lower"). A "sort by score" action reorders a project's backlog to the score in one recorded
act; it is never automatic.

**D4 — The score is a versioned function in the code, not a setting.** Version 1:

```
score = severity_weight + urgency_weight + Σ interest_weight + age_days / 30

severity_weight:  critical 8, high 5, medium 3, low 1, cosmetic 0
urgency_weight:   now 8, release 5, next 3, later 1, icebox -5
interest_weight:  need 1, urgent 2   (per person; watch 0)
age_days:         days since opened_at
```

The score is recomputed when an input changes and stored with the version of the function
that produced it. A change to the function is an amendment of this record and a migration
that recomputes.

**D5 — The person-level lists are ordered by score.** "Next for me" and "assigned to me"
have no shared rank across projects and tenants; they show the score order, with the ticket's
project rank as a secondary indicator.

## Consequences

- The rank says what the project decided; the score says where the facts disagree with
  that decision; the owner reads the disagreement, not the number.
- `/me/next` has a defined order for the first time; it is the score's, and the score's
  weights are therefore the one place where a person's cross-project priorities are tuned.
- D2 avoids the classic renumbering write storm on every drag; the cost is that the string
  occasionally needs rebalancing, which is a maintenance task, not a user-visible event.
- Two numbers per card. The UI shows the score as a marker, not a figure, unless asked.

## Alternatives Considered

- **Manual rank only.** No formula to defend; the rank goes stale silently and the
  person-level lists have no order. Lost.
- **Score only.** Never stale; the rank as a decision disappears and deviations must be
  forced through the inputs. Lost.
- **Score by default, rank as a pin.** Fewer drags; a list with six pinned tickets and a
  computed rest reads worse than a ranked list with markers, and a pin is a rank under another
  name. Lost.
- **A configurable formula per tenant.** A settings page for a function nobody has used yet;
  D4's versioning gives a change a home without one. Lost.

## Residual risks

- Version 1's weights are a first guess; the first months of use will show whether age or
  interest dominates in the wrong way. The amendment path is cheap by design.
- D3's marker can nag: a project that deliberately ranks against the score sees the marker on
  every card. If that happens, a per-project "acknowledge score" is the amendment, not a
  softer formula.

## References

- [ADR 0010](0010-the-frontmatter-vocabularies-become-ticket-columns.md) — severity and urgency
- [ADR 0013](0013-interest-is-a-persons-weighted-reasoned-stake-in-a-ticket.md) — the interest weights
- [ADR 0009](0009-ticket-states-are-the-frontmatter-states-plus-blocked.md) — rank and `blocked`
- [ADR 0005](0005-a-tenant-is-a-client-organisation-and-the-isolation-unit.md) D3 — the person-level lists
