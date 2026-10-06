---
id: T43
title: the dashboard is built, but no end-to-end path walks it and the owner has not reviewed it
state: in-progress
severity: low
security: none
threat:
urgency: later        # rule 4: decided fix
effort: S
blocked-by:
filed-from: T26
opened: 2026-10-03
decided: 2026-10-03
done:
---

## Current state

The tenant's dashboard of [ADR 0018](../adr/0018-the-views-of-the-first-release.md) D6, made
concrete in its D6 on 2026-10-05, is built and is the tenant's front page `/t/{slug}`, *Overview*
in the navigation, before *Board*:

- `GET /api/v1/tenants/{tenant}/dashboard`
  ([`dashboard.go`](../../backend/internal/api/dashboard.go),
  [`dashboard.sql`](../../backend/internal/store/queries/read/dashboard.sql)) answers the nine
  tiles and the open tickets updated last in one transaction, every query under the visibility
  predicate and the deletion filter; each tile's definition is its field's description in
  [`components/schemas.yaml`](../../backend/api/components/schemas.yaml) `Dashboard`.
- One integration test per tile with a hidden-ticket case, and one that a deleted ticket counts in
  no tile and a restored one counts again
  ([`api_dashboard_test.go`](../../backend/test/integration/api_dashboard_test.go)); one unit test
  per tile ([`dashboard_test.go`](../../backend/internal/api/dashboard_test.go)).
- The page ([`features/tenant/dashboard.ts`](../../frontend/src/app/features/tenant/dashboard.ts))
  with its filters in the address, bars in CSS over the preset's tokens, live through
  [`dashboard.service.ts`](../../frontend/src/app/core/dashboard.service.ts) at most once a second
  on the events of its own tenant only, the person-level stream carrying every tenant of the
  person; its unit tests per tile beside it. *Overview* stays marked while the address carries the
  filters.
- In the browser, [`dashboard.spec.ts`](../../frontend/e2e/dashboard.spec.ts) walks the page with
  two identities in a tenant of the test's own: the member's tiles leave out a confidential ticket
  and a project restricted away from the member, the administrator's count them; a filter chosen in
  the page lands in the address and survives a reload; a ticket the member moves moves the
  administrator's tile within five seconds; a ticket the administrator deletes leaves both.
  [`visual.spec.ts`](../../frontend/e2e/visual.spec.ts) compares the page of the visual fixture
  tenant in both schemes beside the board's dark picture. Both in Chromium and WebKit, each in both
  schemes, in three local runs of the whole tier with two workers on 2026-10-06.

What is not done: **the owner has not reviewed the page.** Besides the pictures above, it was looked
at only as the production build against a mocked API in Chromium, both schemes at 1440 px and the
dark one at 1000 px; not on a phone's width.

## Required changes

1. The owner reviews the page under `make dev` in both schemes; what the review changes goes into
   the page and its unit tests.

## Open questions

### Q1: Should a time booking reach the dashboard live?

The time tile is the one tile no event moves: ADR 0054 D4 keeps time entries off the stream,
because time follows its own visibility (ADR 0034 D5) and the stream's filter judges only a
project and the confidential flag.

- **A — Leave it** (recommended): the tile follows at the next reload another event of the tenant
  causes, at a `resync` or the fallback's poll, or when the page is opened. A booking is mostly
  made on a ticket's page, not beside an open dashboard; the cost is a sum that can lag until the
  next act on a ticket.
- **B — Publish a booking** as an event of its ticket: the dashboard reloads on it. The hub would
  need the rule of time's visibility as well, or a member who may not see others' time learns that
  somebody booked on a ticket — the existence of an entry, against ADR 0034 D5. A change of ADR
  0054 D4.
- **C — Reload the open dashboard on a timer**, every minute, besides the events: a `304` costs a
  hash; every open dashboard asks once a minute whether anything changed.

A is recommended: the lag is bounded by the work going on in the tenant, B opens a visibility
rule in the hub for one tile, and C spends a request a minute per open page on a number that
changes a few times a day.

**Answer:** (A) — a booking stays off the stream, and the time tile follows at the next reload.
Settled on the recommendation, the owner reviewing the result (2026-10-05); nothing to build.
Recorded in ADR 0018 D6 and ADR 0054 D4 as made concrete 2026-10-05.

## Not verified

How long the ten queries take over a large tenant: their plans were not read and no tenant of
realistic size was measured. `EXPLAIN ANALYZE` of each query over a seeded tenant would settle it.
