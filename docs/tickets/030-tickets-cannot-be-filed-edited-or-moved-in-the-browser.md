---
id: T30
title: the ticket page cannot edit the title, the body, the parent, the urgency override or the confidential flag, and an editor left open writes to the ticket the page turns to
state: in-progress
severity: high
security: none
threat:
urgency: next         # rule 3: severity high, live — a ticket's body, its current state, cannot be changed in the browser
effort: M
blocked-by:
filed-from: T26
opened: 2026-10-03
decided: 2026-10-03
done:
---

## Current state

Released: filing from the backlog, with a body but without a parent
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
with a reason a person may leave out. Every write puts its answer into the cache. Missing:

- **The body cannot be changed after filing.** The page shows it as text
  ([`ticket-detail.html`](../../frontend/src/app/features/ticket/ticket-detail.html#L51-L52));
  `replaceTicketBody` — `PUT …/tickets/{number}/body` with `If-Match`
  ([`tenants.yaml`](../../backend/api/tenants.yaml#L552-L581)) — has no caller in the frontend.
  The body is the ticket's current state, replaced as a whole
  ([ADR 0011](../adr/0011-a-ticket-is-a-markdown-body-plus-first-class-open-questions.md) D1): in
  the browser it is written once, at filing.
- **The title** is a heading, not an editor
  ([`ticket-detail.html`](../../frontend/src/app/features/ticket/ticket-detail.html#L30)); **the
  parent** is shown, never chosen, neither on filing nor on the page
  ([`ticket-fields.html`](../../frontend/src/app/features/ticket/ticket-fields.html#L151-L153)).
- **The urgency override** and its withdrawal are not on the detail page.
- **The confidential flag** shows as a badge
  ([`ticket-detail.html`](../../frontend/src/app/features/ticket/ticket-detail.html#L20-L24)), and
  nothing sets or lifts it: `setConfidential` is a tenant administrator's act with `admin` scope,
  never an agent's, the lift with a reason
  ([`tenants.yaml`](../../backend/api/tenants.yaml#L636-L648),
  [ADR 0065](../adr/0065-a-confidential-flag-replaces-the-file-name-embargo-set-automatically-lifted-only-by-a-person.md)).
- **An editor left open survives a turn to another ticket.** The page is reused when the path
  names another ticket, and its edit-conflict confirmation closes then
  ([`ticket-detail.ts`](../../frontend/src/app/features/ticket/ticket-detail.ts#L125-L132)); the
  threat editor, the stage dialog and the move dialog do not — each is a plain signal
  ([`ticket-fields.ts`](../../frontend/src/app/features/ticket/ticket-fields.ts#L93) and line 115,
  [`ticket-moves.ts`](../../frontend/src/app/features/ticket/ticket-moves.ts#L49)). Confirmed
  after the turn, each writes to the ticket shown then: the move dialog sends to the page's
  current ticket ([`move-dialog.ts`](../../frontend/src/app/features/ticket/move-dialog.ts#L203-L210))
  with that ticket's state as `from`
  ([`ticket-actions.service.ts`](../../frontend/src/app/core/ticket-actions.service.ts#L148-L152)),
  so a move meant for one ticket lands on the other where its state allows the move; the stage
  dialog and the threat editor write their patch with the other ticket's `ETag`.

## Required changes

1. The body edited on the page as Markdown and written with `replaceTicketBody` and `If-Match`,
   with the `412` prompt the fields have; the title edited in place; the parent chosen from the
   project's tickets, on filing and on the page.
2. The urgency override and its withdrawal on the detail page, with `If-Match` and the optional
   reason; the confidential flag for a tenant administrator, the lift with its reason.
3. The threat editor, the stage dialog and the move dialog close when the page turns to another
   ticket, as the confirmation does; a unit test per editor that a turn closes it and writes
   nothing.
4. Unit tests per form; the e2e path of T29 files, assigns, moves and closes.

## Related

- T33 — renders the body this page edits
