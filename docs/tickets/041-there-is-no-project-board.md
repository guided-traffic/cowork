---
id: T41
title: the project board has no end-to-end path
state: in-progress
severity: medium
security: none
threat:
urgency: later        # rule 4: decided fix
effort: M
blocked-by:
filed-from: T26
opened: 2026-10-03
decided: 2026-10-03
done:
---

## Current state

The board is built at `/t/{slug}/p/{KEY}/board`
([`board.ts`](../../frontend/src/app/features/project/board.ts),
[`board-card.ts`](../../frontend/src/app/features/project/board-card.ts)) as
[ADR 0018](../adr/0018-the-views-of-the-first-release.md) D1 amended on 2026-10-03 has it: the
column `next` with its Now button, the open leaves of urgency `now` and `release` in Refinement,
Ready, In Progress, Blocked and Review with their WIP counts, the count of the tickets done in the
last fourteen days; cards with the size, the bar of the current stage, the block and the count of
the open tickets that block a card directly
([ADR 0012](../adr/0012-four-typed-directed-links-within-a-tenant.md) D6 as amended on
2026-10-03); a drag between the columns is a transition, offered only where the matrix allows it,
with the dialogs of T30; a card being dragged is not moved by an event. Missing: the e2e path.

## Required changes

1. The e2e path of T29 drags a card in both schemes
   ([ADR 0056](../adr/0056-end-to-end-playwright-against-the-built-containers-with-two-identities.md) D3).
