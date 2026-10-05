---
id: T34
title: the score's marker and the sort by score in the backlog have no end-to-end path and no look by the owner
state: in-progress
severity: low
security: none
threat:
urgency: later        # rule 4: decided fix
effort: S
blocked-by:
filed-from: T26
opened: 2026-10-03
decided: 2026-10-03
done:
---

## Current state

The rank of [ADR 0014](../adr/0014-rank-is-the-decision-score-is-the-warning.md) D1 and D2, the
score of D3–D5 and the rebalancing of the Consequences are built and tested in the unit and the
integration tier:

- **The score** is version 1 of [`domain.ScoreKey`](../../backend/internal/domain/score.go), stored
  as a key time does not move with its version ([migration 33](../../backend/internal/store/migrations/000033_ticket_score.up.sql)),
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

What is missing is what the browser shows: the end-to-end tier of T29 walks the backlog's drag but
neither the marker nor the sort, and the owner has not looked at either in `make dev` — the marker's
form (*score* with an arrow, dashed, the figure in its tooltip) and the toolbar button are this
change's proposal.

## Required changes

1. A path in [`backlog.spec.ts`](../../frontend/e2e/backlog.spec.ts): a project whose rank goes
   against the score shows the marker on the ticket out of place, *Sort by score* asks, and after
   the confirmation the rows read by score and the marker is gone — in Chromium and WebKit and both
   colour schemes.
2. The owner's look at the marker and the sort in `make dev`, and what follows from it for their
   form.

## Related

- T26 — the phase this belongs to
- T29 — the end-to-end tier the path belongs to
