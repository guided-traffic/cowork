---
id: T30
title: tickets cannot be filed, edited or moved through their states in the browser
state: in-progress
severity: high
security: none
threat:
urgency: next         # rule 3: severity high, live
effort: M
blocked-by:
filed-from: T26
opened: 2026-10-03
decided: 2026-10-03
done:
---

## Current state

On the branch of phase 3: filing from the backlog
([`new-ticket-dialog.ts`](../../frontend/src/app/features/ticket/new-ticket-dialog.ts)); the
detail page's fields with the cached `ETag` and the `412` prompt and the three progress stages,
whose slider asks for the verification note when it fills the last stage and for a reason when it
lowers a stage of a ticket done by them
([`ticket-fields.ts`](../../frontend/src/app/features/ticket/ticket-fields.ts)); the moves with
their reason, note, block and prerequisite override — `review`, done by hand from every open
state and its withdrawal — in one dialog
([`ticket-moves.ts`](../../frontend/src/app/features/ticket/ticket-moves.ts),
[`move-dialog.ts`](../../frontend/src/app/features/ticket/move-dialog.ts)), the matrix mirroring
`backend/internal/domain/transition.go` in
[`shared/transitions.ts`](../../frontend/src/app/shared/transitions.ts). The urgency override
and its withdrawal are set from the backlog and the board (a drag, the row menu, the Now button),
with a reason a person may leave out. Every write puts its answer into the cache. Missing: the
title edited in place, the parent as a picker, the override on the detail page, the
confidential flag.

## Required changes

1. The title edited in place, and the parent chosen from the project's tickets.
2. The urgency override and its withdrawal on the detail page, with `If-Match` and the optional
   reason; the confidential flag
   ([ADR 0065](../adr/0065-a-confidential-flag-replaces-the-file-name-embargo-set-automatically-lifted-only-by-a-person.md)).
3. Unit tests per form; the e2e path of T29 files, assigns, moves and closes.
