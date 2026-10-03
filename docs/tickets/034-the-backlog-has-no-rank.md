---
id: T34
title: the backlog has no rank — tickets show in number order, nothing can be dragged and the score is nowhere
state: decided
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

`tickets` has no rank column ([migration 8](../../backend/internal/store/migrations/000008_tickets.up.sql));
a project's list is ordered by number, and the backlog of T28 shows it so.
[ADR 0014](../adr/0014-rank-is-the-decision-score-is-the-warning.md) — a manual rank per
project as a sortable string, a computed score beside it, the person-level lists ordered by
score — is carried over from phase 2.

## Required changes

1. The rank column (expand-before-contract migration,
   [ADR 0028](../adr/0028-migrations-only-go-forward-no-down-files-expand-before-contract.md)),
   new tickets at the bottom, the move route that writes one row (D2), recorded and published.
2. The score as a versioned function in the code (D3, D4), returned beside the rank.
3. The backlog ordered by rank, dragged with the CDK
   ([ADR 0052](../adr/0052-primeng-with-the-angular-cdk-a-themes-preset-and-dark-mode-from-the-start.md) D1),
   the score's disagreement marked, children indented under their parent
   ([ADR 0018](../adr/0018-the-views-of-the-first-release.md) D1); a drag while an event moves
   the list keeps the person's drop.
4. Integration tests for concurrent moves; unit tests for the drag; the e2e path drags.
