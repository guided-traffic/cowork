---
id: T38
title: there are no saved filters
state: in-progress
severity: low
security: none
threat:
urgency: later        # rule 4: decided fix; the view it applies to across a tenant waits for Q1
effort: M
blocked-by:
filed-from: T26
opened: 2026-10-03
decided:
done:
---

## Current state

Built: the table `saved_filters` (migration 36, tenant-bound, restrictive policies that hold a
person to their own filters and the shared ones), the routes `…/filters` and `…/filters/{filter}` —
list, create with a key, read, edit with `If-Match`, share and unshare by the edit, delete —, every
change a recorded act ([`api/filters.go`](../../backend/internal/api/filters.go)); the parameters
checked as the lists check them and checked again when read, a value that no longer holds a warning
([ADR 0049](../adr/0049-filters-are-explicit-repeatable-query-parameters-no-query-language.md) D7),
another member's filter that names what the reader cannot see withheld; the backlog's filter bar
applies, saves, shares and deletes filters, a shared one naming its owner
([`saved-filters.ts`](../../frontend/src/app/features/project/saved-filters.ts)); integration tests
across two persons and two tenants ([`api_filters_test.go`](../../backend/test/integration/api_filters_test.go)).

Outstanding: the tenant board does not exist yet (T42), so its filter bar cannot apply them; no page
lists a tenant's tickets (Q1); no end-to-end path walks a saved filter — the images could not be
built where the backlog's bar was made; a tenant administrator cannot remove a shared filter of a
person who left, which stays shared until its owner deletes it.

[ADR 0018](../adr/0018-the-views-of-the-first-release.md) D5 applies saved filters to "the tenant
list view" and ADR 0049 D6 serves "the tenant-wide list" with the same parameters, while the
tenant's routes in the browser have no list view ([`app.routes.ts`](../../frontend/src/app/app.routes.ts))
and ADR 0023 D4's UI routes name none; the API serves the list (`listTenantTickets`) and the
frontend reads it for the tenant's front page only.

## Required changes

### Independent of the open question

1. The tenant board's filter bar (T42) applies, saves and shares a filter as the backlog's does,
   through `SavedFilters` and `toBacklog`'s counterpart for the board.
2. An end-to-end spec: a member saves the backlog's filter shared, a second person applies it and
   sees its owner; `make docker-build e2e`.

### Depends on the answer

3. Saved filters on the tenant-wide list, as Q1 decides.

## Open questions

### Q1: Where across a tenant do saved filters apply, when no page lists the tenant's tickets?

ADR 0018 D5 and ADR 0049 D6 presuppose a tenant-wide list view; none exists and no ticket builds
one, while the API and the frontend's `TicketsService.tenantTickets` already serve the list.

- **(a) This ticket builds the page:** `/t/{slug}/tickets`, mirroring the API (ADR 0023 D4), a
  table over `listTenantTickets` with the filter bar and numbered pages
  ([ADR 0048](../adr/0048-cursor-pagination-on-every-list-numbered-pages-on-tables.md) D2); the
  records stand as written, and this ticket grows from M to L and from one subject to two.
- **(b) A ticket of its own for the page,** in the phase-3 family; this one applies saved filters
  to it once it exists. The page does not wait for saved filters — its filters are query
  parameters already — and saved filters do not own a view, as they own neither the backlog nor
  the boards.
- **(c) No tenant-wide list page:** saved filters apply to the project backlog and the tenant
  board; ADR 0018 D5 and ADR 0049 D6 are amended, and the tenant-wide list stays an API list.

Recommended: **(b)** — the records presuppose the view and the API and the client serve it
already, so it is cheap; a ticket of its own keeps this one to one subject, as the filing rule
asks and as the backlog and the boards are kept; (c) drops the only list across a tenant's
projects in the browser, which is the overview across many undertakings ADR 0018 was written
for.

**Answer:** _open_
