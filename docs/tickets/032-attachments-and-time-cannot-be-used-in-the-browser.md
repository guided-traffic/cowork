---
id: T32
title: attachments and time cannot be used in the browser, the tenant has no attachment quota and there is no time report view
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

On the branch of phase 3, the detail page lists, uploads and downloads attachments and books
and voids the person's own time ([`records-cards.ts`](../../frontend/src/app/features/ticket/records-cards.ts)),
and `/t/{slug}/time` shows the time report per project, ticket, person or tenant over a period
([`time-report.ts`](../../frontend/src/app/features/time/time-report.ts)). Time entries are not
published on the event stream ([ADR 0054](../adr/0054-server-sent-events-per-tenant-carry-keys-not-content-polling-is-the-fallback.md)
D4): the page that books reloads them. The per-tenant attachment quota of
[ADR 0016](../adr/0016-attachments-live-in-s3-compatible-storage-and-are-served-only-through-the-backend.md)
D6 is carried over from phase 2: limits per file and per ticket exist, a tenant quota does not.

## Required changes

1. An upload to a comment; the image preview of a raster attachment.
2. The per-tenant quota: the setting and its enforcement before bytes are stored, the usage in
   the tenant's administration (ADR 0016 D6); integration tests across two tenants.
3. Correcting a time entry with `If-Match` and its revisions
   ([ADR 0017](../adr/0017-effort-is-a-size-progress-is-a-five-step-percentage-and-time-is-booked-by-people.md)).
4. Unit tests (written for what exists); the README reference for the quota's variable and
   problem code.
