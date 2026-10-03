---
id: T34
title: the backlog has no rank to drag — nothing can be dragged and the score is nowhere
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

The rank of [ADR 0014](../adr/0014-rank-is-the-decision-score-is-the-warning.md) D1 and D2 is in
the backend: the `rank` column ([migration 17](../../backend/internal/store/migrations/000017_ticket_rank.up.sql)),
filings and reopens at the bottom, done and dropped without a key, the move
`PUT …/tickets/{number}/rank` ([`rank.go`](../../backend/internal/api/rank.go)), recorded as
`ranked` and published, and a project's list in rank order. The backlog of T28 shows that order
but cannot change it: nothing can be dragged. The score of D3 and D4 — a versioned function,
stored with its version, shown beside the rank — does not exist. Nothing rebalances the keys:
about 630 to 760 moves into one and the same gap, depending on the side they land on, exhaust
it, and the next move there fails as an internal error.

## Required changes

1. The score as a versioned function in the code (D3, D4), recomputed when an input changes,
   stored with its version and returned on the ticket — the rank key itself stays unshown
   (D2); the "sort by score" act.
2. The backlog dragged with the CDK
   ([ADR 0052](../adr/0052-primeng-with-the-angular-cdk-a-themes-preset-and-dark-mode-from-the-start.md) D1)
   on the move route, the score's disagreement marked, children indented under their parent
   ([ADR 0018](../adr/0018-the-views-of-the-first-release.md) D1); a drag while an event moves the
   list keeps the person's drop.
3. A rebalancing of a project's keys before a gap runs out (ADR 0014 Consequences), with its
   integration test.
4. Unit tests for the drag; the e2e path drags.
