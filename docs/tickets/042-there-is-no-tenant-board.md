---
id: T42
title: there is no tenant board with one swimlane per project
state: analysed
severity: medium
security: none
threat:
urgency: later        # rule 4: decided fix except the columns, which wait for Q1
effort: M
blocked-by:
filed-from: T26
opened: 2026-10-03
decided:
done:
---

## Current state

No tenant board exists. The project board does (T41:
[`board.ts`](../../frontend/src/app/features/project/board.ts),
[`board-card.ts`](../../frontend/src/app/features/project/board-card.ts),
[`board-model.ts`](../../frontend/src/app/features/project/board-model.ts)).
[ADR 0018](../adr/0018-the-views-of-the-first-release.md) D4: columns are the states, rows the
non-archived projects, cards as on the project board; a drag between columns is a transition, a
drag between rows is refused, rank is not edited here; lazy per swimlane, with a project filter.
D4 was not amended when D1's amendment of 2026-10-03 gave the project board its columns — Q1.

## Required changes

1. `/t/{slug}` board view (beside the overview of T28 until the dashboard of T43 takes the front
   page) with lazy swimlanes, the columns Q1 decides and the project filter; saved filters apply
   (T38).
2. The refusal of a drag across swimlanes is visible, not silent.
3. Unit tests; the e2e path of T29's tier drags a card across columns and is refused across rows.

## Open questions

### Q1: Which columns does the tenant board have?

ADR 0018 D4 says "columns are the states" and "cards as in D1". D1's amendment of 2026-10-03
replaced one column per state on the project board with Refinement, Ready, In Progress, Blocked
and Review over the open leaves of `now` and `release`, the column `next` beside them, and no
column for `done`, "which the owner found a waste of space"; D4 was not amended with it.

- **(a) The project board's columns in every swimlane:** `next`, Refinement, Ready, In Progress,
  Blocked and Review, over the same tickets; one model for both boards
  ([`board-model.ts`](../../frontend/src/app/features/project/board-model.ts)); D4 amended to
  say so.
- **(b) One column per state, as D4 reads:** eight columns from `filed` to `dropped`, every
  ticket of each project; a second model, and the `done` column the owner turned down on the
  project board.
- **(c) The project board's five columns without `next`:** the current work across projects,
  `next` left to each project's board.

Recommended: **(a)** — the owner's choice for the project board, a board of the current work
without a `done` column, would otherwise be reversed on the second board; one model keeps a card
in the same column on both, and D4's "cards as in D1" already points at D1.

**Answer:** _open_
