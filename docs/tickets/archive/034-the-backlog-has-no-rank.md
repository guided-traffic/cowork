---
id: T34
title: the score's marker and the sort by score in the backlog have no end-to-end path and no look by the owner
state: done
severity: low
security: none
threat:
urgency: later        # rule 4: decided fix
effort: S
blocked-by:
filed-from: T26
opened: 2026-10-03
decided: 2026-10-03
done: 2026-10-09
shipped: built and released in phase 3, before 0.8.0
---

## Current state

The rank of [ADR 0014](../adr/0014-rank-is-the-decision-score-is-the-warning.md) D1 and D2, the
score of D3–D5 and the rebalancing of the Consequences are built and tested in the unit and the
integration tier:

- **The score** is version 1 of [`domain.ScoreKey`](../../backend/internal/domain/score.go), stored
  as a key time does not move with its version ([migration 34](../../backend/internal/store/migrations/000034_ticket_score.up.sql)),
  scored again by the write that changes an input — a filing, the severity, the horizon, a stake
  ([`api/score.go`](../../backend/internal/api/score.go) `refreshScore`) — and shown on the ticket as
  `score` and `score_version`. `TestTheScoreFollowsItsInputs`, `TestScoreMigrationScoresEveryTicket`,
  the unit tests of [`score_test.go`](../../backend/internal/domain/score_test.go).
- **The sort by score**, `PUT …/projects/{project}/rank`, one act of the project in the activity of
  every ticket it moved, published as `project.changed`, a hidden ticket keeping its key and place
  (`TestSortByScore`); the backlog's *Sort by score* asks first
  ([`backlog.ts`](../../frontend/src/app/features/project/backlog.ts)).
- **The marker** among the siblings of each horizon's group (`scoreMarks` in
  [`backlog-model.ts`](../../frontend/src/app/features/project/backlog-model.ts)).
- **The rebalancing** before a gap runs out (`rebalanceRank` in
  [`rank.go`](../../backend/internal/api/rank.go)); 800 moves into one gap succeed
  (`TestEightHundredMovesIntoOneGap`).

In the browser, [`backlog.spec.ts`](../../frontend/e2e/backlog.spec.ts) walks the marker and the
sort: a ticket of a higher score that the rank puts last carries the only marker, *Sort by score*
asks, and after the confirmation the rows read by score and the marker is gone — in Chromium and
WebKit, each in both schemes, in three local runs of the whole tier with two workers on 2026-10-06.

What is missing is the owner's look at both in `make dev`: the marker's form (*score* with an arrow,
dashed, the figure in its tooltip) and the toolbar button are this change's proposal.

## Required changes

None. The owner reviews it in use ("Lass mich doch erstmal anfangen das Tool zu verwenden", 2026-10-09): T55 holds the review of the built pages as one item, and what he wants changed becomes a ticket of its own.

## Related

- T26 — the phase this belongs to
- T29 — the end-to-end tier the path belongs to
