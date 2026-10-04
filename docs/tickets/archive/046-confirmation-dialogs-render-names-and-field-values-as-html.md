---
id: T46
title: confirmation dialogs render a person's name and other members' field values as HTML
state: done
severity: medium
security: boundary
threat: a member of a tenant, or anyone who controls their name at the identity provider, puts a link or an image into a confirmation dialog another member or an administrator opens — a link that looks like cowork's own ("session expired, sign in again") or an image that calls out when the dialog opens
urgency: next         # rule 3: severity medium, trigger live since 0.2.0, fixed in 0.3.0
effort: XS
blocked-by:
filed-from: the phase-4 frontend review of 2026-10-04
opened: 2026-10-04
decided: 2026-10-04
done: 2026-10-04
shipped: 0.3.0 — every confirmation dialog shows its message as text through app-confirm-dialog, no page uses p-confirmdialog itself
---

## Current state

PrimeNG 22's `p-confirmdialog` renders its `message` through `[innerHTML]`
(`primeng-confirmdialog.mjs`); Angular's sanitiser removes scripts but keeps `<a href>` and
`<img src>`. Messages built from data others control:
[`ticket-fields.ts`](../../frontend/src/app/features/ticket/ticket-fields.ts) (the edit-conflict
prompt quotes the other member's field values — released in 0.2.0), the phase-4 members page
(`members.ts`, the person's display name — for a provider person the provider's `name` claim)
and the group-mappings page (group names). Harmless names break too: `Smith <Jones>` shows as
`Smith `.

## Required changes

1. Every `p-confirmdialog` renders its message as text: a `<ng-template #message let-c>` with
   `{{ c.message }}` (one shared wrapper if it fits), on every page that has one (tickets, members,
   group mappings, accounts, tokens, project settings).
2. A test per page that a message with markup shows the markup as text.
3. On the merge of the fixing pull request: `state: done`, `shipped:`, then the file is renamed
   without the `local_` prefix and archived.
