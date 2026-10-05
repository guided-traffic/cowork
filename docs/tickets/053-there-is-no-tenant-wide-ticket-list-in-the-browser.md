---
id: T53
title: there is no tenant-wide ticket list in the browser
state: in-progress
severity: medium
security: none
threat:
urgency: later        # rule 4: what is left is a known run and a look
effort: M
blocked-by:
filed-from: T38
opened: 2026-10-05
decided: 2026-10-05
done:
---

## Current state

The tenant's ticket list is built at `/t/{slug}/tickets`, on the recommendation, the owner
reviewing the result ([`tenant-tickets.ts`](../../frontend/src/app/features/tenant/tenant-tickets.ts),
[`tenant-tickets-model.ts`](../../frontend/src/app/features/tenant/tenant-tickets-model.ts)), as
[ADR 0018](../adr/0018-the-views-of-the-first-release.md) D5 and
[ADR 0023](../adr/0023-the-tenant-is-in-the-path.md) D4 have it, and the navigation links it as
*Tickets* right after *Board*: a table over `listTenantTickets`, newest first, the project beside
each key; every filter of
[ADR 0049](../adr/0049-filters-are-explicit-repeatable-query-parameters-no-query-language.md) D1 a
query parameter of the page — the backlog's filters (state, type, severity, security, horizon,
effort, assignee, reporter, text) and the project as the bar's controls, the others named under the
bar —; numbered pages of 25, 50 or 100
([ADR 0048](../adr/0048-cursor-pagination-on-every-list-numbered-pages-on-tables.md) D2, D4); live
through the tickets service's reload on the tenant's events, a poll answered `304`; saved filters
applied, saved and shared through the backlog's filter bar, made one part for both lists
([`saved-filters.ts`](../../frontend/src/app/features/project/saved-filters.ts), `leftOut`), with
`project` applied here. No backend change: the route took numbered pages and every filter already,
and leaves deleted tickets out. The description lives in
[frontend.md](../developer/frontend.md#the-tenants-ticket-list); ADR 0018, 0023, 0048 and 0049 say
it in their Status.

**Verified** on 2026-10-05: `make frontend-test` (111 files, 4,036 tests, every one passing),
`make frontend-lint`, `make frontend-build` without a budget warning — the initial bundle
1,041,492 bytes of 1,048,576, 460 more than before for the route and the navigation's link; the
page is a lazy chunk of its own. The unit tests cover the address's model
([`tenant-tickets-model.spec.ts`](../../frontend/src/app/features/tenant/tenant-tickets-model.spec.ts))
and, in [`tenant-tickets.spec.ts`](../../frontend/src/app/features/tenant/tenant-tickets.spec.ts),
the request the address makes, the rows, the selects writing the address and keeping negated
values, the text's delay, the conditions beyond the bar, the failure that names a refused filter,
the numbered pages — the rows kept while the next page loads, the start at the first, the step to
the last page of a list that shrank —, a saved filter replacing the address's conditions, and, over
the real tickets service, the reload of the page shown on an event of the tenant and none on
another tenant's; taking out the step to the last page, the kept rows, the start at the first page
or the service's tenant check makes them fail. The saved-filter bar's spec covers `leftOut`, the
backlog's spec its note under the bar, and the routes' and the shell's specs the route and the link.

The end-to-end path, [`ticket-list.spec.ts`](../../frontend/e2e/ticket-list.spec.ts) — the list
over two projects through the address, narrowed by it and the same after a reload, a ticket filed
meanwhile at the top without a reload, a row opening its ticket, *Clear filters* —, passes
(run 37285901009 of commit `65337eb`). **Not verified:** the page has not been looked at in a browser — `make dev` was not run —, so the bar's ten controls on a narrow window,
both schemes, and a select's open overlay while its choice goes through the address are unseen.

## Required changes

1. The owner looks at the page in both schemes under `make dev` and reviews it: the link's place
   after *Board*, the bar, the columns and the page size of fifty to begin with.
