---
id: T34
title: the backlog has no score beside its rank, and nothing rebalances the rank keys
state: in-progress
severity: medium
security: none
threat:
urgency: later        # rule 4: decided fix
effort: L
blocked-by:
filed-from: T26
opened: 2026-10-03
decided: 2026-10-03
done:
---

## Current state

The rank of [ADR 0014](../adr/0014-rank-is-the-decision-score-is-the-warning.md) D1 and D2 is
built: the `rank` column ([migration 17](../../backend/internal/store/migrations/000017_ticket_rank.up.sql)),
the move `PUT …/tickets/{number}/rank` ([`rank.go`](../../backend/internal/api/rank.go)), a
project's list in rank order, and the backlog as a table grouped by urgency whose rows are dragged
within and between the groups, the latter with the urgency override or its withdrawal
([`backlog.ts`](../../frontend/src/app/features/project/backlog.ts),
[ADR 0018](../adr/0018-the-views-of-the-first-release.md) D1). The score of D3 and D4 — a
versioned function, stored with its version, shown beside the rank — does not exist, so the
backlog marks no disagreement. Nothing rebalances the keys: about 630 to 760 moves into one and
the same gap, depending on the side they land on, exhaust it, and the next move there fails as an
internal error. The backlog's drag has its end-to-end path in the tier of T29,
[`backlog.spec.ts`](../../frontend/e2e/backlog.spec.ts): a row dragged to the top of its horizon
moves first in the project's rank, and one dragged into another horizon takes that horizon, in
Chromium and WebKit and both colour schemes.

## Required changes

1. The score as a versioned function in the code (D3, D4), recomputed when an input changes,
   stored with its version and returned on the ticket — the rank key itself stays unshown
   (D2); the "sort by score" act; the backlog marks the score's disagreement.
2. A rebalancing of a project's keys before a gap runs out (ADR 0014 Consequences), with its
   integration test.
