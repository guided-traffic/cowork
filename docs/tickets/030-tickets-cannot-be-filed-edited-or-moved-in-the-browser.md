---
id: T30
title: filing, editing and moving a ticket in the browser has no end-to-end path, and nobody has looked at the new editors in both schemes
state: in-progress
severity: medium
security: none
threat:
urgency: later        # rule 4: the fix is decided — the path of T29, and a look in the browser
effort: S
blocked-by: T29
filed-from: T26
opened: 2026-10-03
decided: 2026-10-03
done:
---

## Current state

The ticket page edits what a ticket is
([`frontend.md`](../developer/frontend.md#the-detail-page)): the title in place and the body as
Markdown, each over the version its editor began with and with the `412` asked about
([`ticket-title.ts`](../../frontend/src/app/features/ticket/ticket-title.ts),
[`ticket-body.ts`](../../frontend/src/app/features/ticket/ticket-body.ts)); the parent among
the project's open tickets, on filing and on the page
([`parent-picker.ts`](../../frontend/src/app/features/ticket/parent-picker.ts)); the horizon with
its optional reason, and the confidential flag for a tenant administrator
([`ticket-fields.ts`](../../frontend/src/app/features/ticket/ticket-fields.ts),
[`confidential-dialog.ts`](../../frontend/src/app/features/ticket/confidential-dialog.ts)). Every
editor and dialog of the page, the moves' included, belongs to the ticket it was opened on and
closes unwritten when the page turns to another; a unit test per editor holds that. The body is
shown as text (T33). Missing:

- **The end-to-end path** that files a ticket, assigns it, moves it and closes it through the
  browser, with a second identity watching — the tier is T29.

## Required changes

1. The e2e path of T29 files, assigns, edits the title and the body, moves and closes.

## Not verified

- The new editors — the title and body editors with their conflict note, the horizon select and
  its reason field, the parent picker, the confidential dialog — were built against unit tests and
  the existing page patterns, with the preset's tokens only; nobody has looked at them in a
  browser, in either scheme. `make dev` and a look at a ticket in dark and light settle it.

## Related

- T33 — renders the body this page edits
