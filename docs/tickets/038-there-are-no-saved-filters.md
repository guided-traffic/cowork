---
id: T38
title: there are no saved filters
state: analysed
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

The list filters exist as query parameters ([ADR 0049](../adr/0049-filters-are-explicit-repeatable-query-parameters-no-query-language.md));
nothing stores a set of them. [ADR 0018](../adr/0018-the-views-of-the-first-release.md) D5: a
saved filter is a named parameter set, owned by a person, optionally shared with the tenant,
applicable to the backlog, the tenant list view and the tenant board; it carries a version
([ADR 0050](../adr/0050-optimistic-concurrency-a-version-per-entity-if-match-where-a-write-overwrites.md) D1).

No page lists a tenant's tickets, and no ticket builds one. The API serves the tenant-wide list
with every filter and numbered pages (`listTenantTickets`,
[`tenants.yaml`](../../backend/api/tenants.yaml#L332-L388)), and the frontend reads it for the
tenant's front page only ([`overview.ts`](../../frontend/src/app/features/tenant/overview.ts#L74));
the tenant's routes have no list view ([`app.routes.ts`](../../frontend/src/app/app.routes.ts#L29-L80)),
and ADR 0023 D4's UI routes name none — while ADR 0018 D5 applies saved filters to "the tenant
list view" and ADR 0049 D6 serves "the tenant-wide list" with the same parameters.

## Required changes

### Independent of the open question

1. The table (tenant-bound, row-level security), the routes (create, list, edit with
   `If-Match`, share, delete), recorded acts.
2. The filter bar of the backlog and of the tenant board (T42) can save, apply and share a
   filter; a shared one shows its owner.
3. Integration tests across two persons and two tenants.

### Depends on the answer

4. Saved filters on the tenant-wide list, as Q1 decides.

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
