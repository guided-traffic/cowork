---
id: T39
title: tickets cannot be deleted, restored or purged
state: decided
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

`tickets` has no deletion column and no route deletes a ticket.
[ADR 0024](../adr/0024-deletion-tickets-are-soft-deleted-and-purged-projects-archived-people-deactivated-tenants-deleted-explicitly.md)
D1–D3, D7 — soft deletion by a tenant administrator, the purge after thirty days by a job or an
explicit act, the application filter on every list — is carried over from phase 2.

## Required changes

1. The column, the delete, restore and purge routes with D7's rules, the filter on every list
   and on the key resolver, the purge job ([ADR 0027](../adr/0027-data-access-is-sqlc-over-pgx-behind-a-tenant-transaction-and-a-mutation-wrapper.md) D5),
   recorded and published.
2. In the browser: delete with a confirmation, the administrator's view of deleted tickets with
   restore and purge.
3. Integration tests that a deleted ticket answers like a missing one everywhere but the
   administrator's view.
