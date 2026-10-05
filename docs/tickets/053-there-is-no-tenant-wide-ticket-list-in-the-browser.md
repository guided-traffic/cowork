---
id: T53
title: there is no tenant-wide ticket list in the browser
state: decided
severity: medium
security: none
threat:
urgency: next         # rule 3: severity medium, trigger live — the only list across a tenant's projects exists in the API alone
effort: M
blocked-by:
filed-from: T38
opened: 2026-10-05
decided: 2026-10-05
done:
---

## Current state

[ADR 0018](../adr/0018-the-views-of-the-first-release.md) D5 and
[ADR 0049](../adr/0049-filters-are-explicit-repeatable-query-parameters-no-query-language.md) D6
presuppose a list of a tenant's tickets across its projects, to which saved filters apply. The API
serves it (`GET /api/v1/tenants/{tenant}/tickets` with the filters of ADR 0049) and the frontend's
`TicketsService.tenantTickets` reads it; no page shows it. The owner chose, on 2026-10-05, a ticket
of its own for the page over building it with the saved filters and over dropping the list — built
on the recommendation, the owner reviewing the result.

## Required changes

1. The page `/t/{slug}/tickets`, mirroring the API ([ADR 0023](../adr/0023-the-tenant-is-in-the-path.md)
   D4): a table over `listTenantTickets` with the project beside each key, the filter bar of the
   backlog — state, type, severity, security, horizon, effort, assignee, reporter, text — as query
   parameters of the page, and numbered pages
   ([ADR 0048](../adr/0048-cursor-pagination-on-every-list-numbered-pages-on-tables.md) D2); in the
   tenant's navigation; live as the backlog is.
2. Saved filters apply to it, as they do to the backlog.
3. Unit tests for the page and its filters.
