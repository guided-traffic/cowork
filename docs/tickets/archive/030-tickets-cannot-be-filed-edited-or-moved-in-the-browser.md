---
id: T30
title: filing, editing and moving a ticket in the browser has no end-to-end path, and nobody has looked at the new editors in both schemes
state: done
severity: medium
security: none
threat:
urgency: later        # rule 4: the fix is decided — the path of T29, and a look in the browser
effort: S
blocked-by:
filed-from: T26
opened: 2026-10-03
decided: 2026-10-03
done: 2026-10-06
shipped: frontend/e2e/assigned.spec.ts files, assigns, edits the title and the body, moves and closes a ticket while a second identity watches, in Chromium and WebKit and both colour schemes
---

## Current state

The ticket page edits what a ticket is
([`frontend.md`](../developer/frontend.md#the-detail-page)): the title in place and the body as
Markdown, each over the version its editor began with and with the `412` asked about; the parent,
the horizon with its optional reason and the confidential flag. The phase's path with two
identities, [`assigned.spec.ts`](../../frontend/e2e/assigned.spec.ts), files a ticket, assigns it,
and the second identity edits its title in place and its body as Markdown, moves it and closes it
while the administrator's open page shows the new title, the body as the server rendered it and the
ticket done — in Chromium and WebKit, each in both schemes, in three local runs of the whole tier
with two workers on 2026-10-06. The description lives in
[testing.md](../developer/testing.md#end-to-end-tests); what nobody has looked at on a screen is
[frontend.md](../developer/frontend.md#the-detail-page)'s *Not verified*.

## Required changes

None.

## Related

- T33 — renders the body this page edits
