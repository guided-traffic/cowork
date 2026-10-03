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
([`new-ticket-dialog.ts`](../../frontend/src/app/features/ticket/new-ticket-dialog.ts)), the
detail page's fields with the cached `ETag` and the `412` prompt
([`ticket-fields.ts`](../../frontend/src/app/features/ticket/ticket-fields.ts)), the progress slider,
and the transitions with their reason, verification note, block and prerequisite override
([`ticket-moves.ts`](../../frontend/src/app/features/ticket/ticket-moves.ts), mirroring
`backend/internal/domain/transition.go`). Every write puts its answer into the cache. Missing:
the urgency override and its withdrawal, the confidential flag, the parent as a picker.

## Required changes

1. The title edited in place, and the parent chosen from the project's tickets.
2. The urgency override and its withdrawal with their reason, both with `If-Match`; the
   confidential flag ([ADR 0065](../adr/0065-a-confidential-flag-replaces-the-file-name-embargo-set-automatically-lifted-only-by-a-person.md)).
3. Unit tests per form and per transition rule (written for what exists); the e2e path of T29
   files, assigns, moves and closes.
