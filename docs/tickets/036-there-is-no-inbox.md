---
id: T36
title: there is no inbox — no notifications, no unread count and no person-level events
state: decided
severity: high
security: none
threat:
urgency: next         # rule 3: severity high, live — the phase's verification goes through the inbox
effort: L
blocked-by:
filed-from: T26
opened: 2026-10-03
decided: 2026-10-03
done:
---

## Current state

No notification table, no inbox route. The event stream serves one tenant
([events.md](../developer/events.md)); `?me=true` and `inbox.changed` of
[ADR 0054](../adr/0054-server-sent-events-per-tenant-carry-keys-not-content-polling-is-the-fallback.md)
D1, D2 are carried over from phase 2.

## Required changes

1. Notifications as rows written in the act's transaction
   ([ADR 0020](../adr/0020-notifications-are-an-in-app-inbox-per-person.md) D1–D3, D6), for the
   events D2 names; read and unread; retention by the job.
2. `GET /api/v1/me/inbox` and marking read ([ADR 0023](../adr/0023-the-tenant-is-in-the-path.md) D2).
3. The person-level stream: `?me=true` with `inbox.changed {"unread": n}` and the person's own
   events across their tenants (ADR 0054 D1, D2); its integration tests assert what it must not
   carry.
4. The bell with the unread count in the top bar, the page `/me/inbox`, both live.
5. The e2e path: the second identity sees the assignment in its inbox within the stream's
   latency.

## Related

- T35 — its Q1 decides whether the person-level stream carries more than the person's own events
