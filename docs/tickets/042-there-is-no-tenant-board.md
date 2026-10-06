---
id: T42
title: there is no tenant board with one swimlane per project
state: in-progress
severity: medium
security: none
threat:
urgency: later        # rule 4: decided fix
effort: M
blocked-by:
filed-from: T26
opened: 2026-10-03
decided: 2026-10-05
done:
---

## Current state

The tenant board is built at `/t/{slug}/board`
([`tenant-board.ts`](../../frontend/src/app/features/tenant/tenant-board.ts),
[`board-lane.ts`](../../frontend/src/app/features/tenant/board-lane.ts),
[`tenant-board-model.ts`](../../frontend/src/app/features/tenant/tenant-board-model.ts)) as
[ADR 0018](../adr/0018-the-views-of-the-first-release.md) D4 as amended 2026-10-05 has it, and
the navigation links it as *Board* right after *Overview*: a swimlane per non-archived project the
person sees, each with the project board's columns over the same tickets, made of the parts the
project board now shares with it
([`board-columns.ts`](../../frontend/src/app/features/project/board-columns.ts),
[`board-moves.ts`](../../frontend/src/app/features/project/board-moves.ts),
[`board-list.ts`](../../frontend/src/app/features/project/board-list.ts)); a swimlane loads its
list only while it is in view or a screen's height from it; the project filter is the address's
repeated `project`; a drag between the columns of a swimlane is the transition with the project
board's dialogs; a drag onto another swimlane is refused visibly — the swimlane under the card
says no while it is there, and a toast says that a ticket never changes project on a board —;
every swimlane holds still while a card is dragged; rank is not edited; the event stream keeps the
swimlanes in or near view live. No backend change and no migration. The description lives in
[frontend.md](../developer/frontend.md#the-tenant-board).

**Verified** on 2026-10-05: `make frontend-test` (101 files, every test passing), `make
frontend-lint`, `make frontend-build` (only the bundle-budget warning of the initial bundle that
stood before; the tenant board's code is in lazy chunks). The unit tests cover the model
([`tenant-board-model.spec.ts`](../../frontend/src/app/features/tenant/tenant-board-model.spec.ts)),
and in [`tenant-board.spec.ts`](../../frontend/src/app/features/tenant/tenant-board.spec.ts) the
swimlanes and their columns, the lazy loading, the filter, the drags inside a swimlane and the
refused drag across swimlanes; taking out the refusal, the lazy load or the hold of the other
swimlanes makes them fail. The project board's own spec runs unchanged against the shared parts.

The end-to-end path, [`tenant-board.spec.ts`](../../frontend/e2e/tenant-board.spec.ts) — "drags a
card across columns and is refused across rows" —, passes (run 37285901009 of commit `65337eb`). **Not verified:** the
board has not been looked at in a browser: the observer's root, the
shell's content area, and the pointer under a dragged card are covered by fakes in the unit tests
only.

**Left out on purpose:** saved filters are T38's and are being built separately; nothing here
builds or depends on them. Until they exist the board's filter is the address's `project`, the
parameter set T38's filter bar applies to it.

## Required changes

1. The owner looks at the board in both schemes under `make dev`. The end-to-end tier ran
   `tenant-board.spec.ts` in Chromium and WebKit and both schemes on a runner on 2026-10-05 (run
   37269590545) and passed.
   The look takes in the board's saved-filter bar, built on the recommendation, and its choices:
   only the projects go into the address (a reload keeps them, drops the rest); a column's WIP
   count under a filter counts the cards shown, so its over-limit mark can go; a closed state or
   `done_after` in a filter loads tickets no column shows; the line "Also filtered by …" is the
   board's alone; "Show every project" clears excluded projects too.

## Open questions

### Q1: Which columns does the tenant board have?

ADR 0018 D4 said "columns are the states" and "cards as in D1", while D1's amendment of
2026-10-03 had given the project board Refinement, Ready, In Progress, Blocked and Review over the
open leaves of `now` and `release`, the column `next` beside them, and no column for `done`.

- **(a) The project board's columns in every swimlane:** `next`, Refinement, Ready, In Progress,
  Blocked and Review, over the same tickets; one model for both boards
  ([`board-model.ts`](../../frontend/src/app/features/project/board-model.ts)); D4 amended to
  say so.
- **(b) One column per state, as D4 read:** eight columns from `filed` to `dropped`, every
  ticket of each project; a second model, and the `done` column the owner turned down on the
  project board.
- **(c) The project board's five columns without `next`:** the current work across projects,
  `next` left to each project's board.

Recommended: **(a)** — the owner's choice for the project board, a board of the current work
without a `done` column, would otherwise be reversed on the second board; one model keeps a card
in the same column on both, and D4's "cards as in D1" already points at D1.

**Answer:** (a) — the project board's columns in every swimlane, over the same tickets, one model
for both boards, a card in the same column on both (the owner, 2026-10-05). Recorded in ADR 0018
D4 as amended 2026-10-05.
