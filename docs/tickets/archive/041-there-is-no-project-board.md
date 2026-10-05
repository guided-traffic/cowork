---
id: T41
title: the project board has no end-to-end path
state: done
severity: medium
security: none
threat:
urgency: later        # rule 4: decided fix
effort: M
blocked-by:
filed-from: T26
opened: 2026-10-03
decided: 2026-10-03
done: 2026-10-04
shipped: frontend/e2e/board.spec.ts, a project opening on its board and a card dragged from Refinement to Ready as the transition to decided, in Chromium and WebKit and both colour schemes
---

## Current state

The board is built at `/t/{slug}/p/{KEY}/board`
([`board.ts`](../../frontend/src/app/features/project/board.ts),
[`board-card.ts`](../../frontend/src/app/features/project/board-card.ts)) as
[ADR 0018](../adr/0018-the-views-of-the-first-release.md) D1 has it, and a project's address opens
it. Its end-to-end path is [`board.spec.ts`](../../frontend/e2e/board.spec.ts) in the tier of T29
([ADR 0056](../adr/0056-end-to-end-playwright-against-the-built-containers-with-two-identities.md)
D3): the project's address and the navigation's link open the board; a card dragged from
Refinement to Ready is the transition `analysed → decided` — in its column, the column's count, the
live region and the API, and after a reload — in Chromium and WebKit, each in both schemes. The
board of the visual fixture tenant is the page of the dark-mode screenshot. Verified on 2026-10-04
by `make e2e`; the description lives in [testing.md](../developer/testing.md#end-to-end-tests).

## Required changes

None. Left: the move to [archive/](archive/).
