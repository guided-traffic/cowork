---
id: T38
title: there are no saved filters
state: in-progress
severity: low
security: none
threat:
urgency: later        # rule 4: decided fix
effort: M
blocked-by:
filed-from: T26
opened: 2026-10-03
decided: 2026-10-05
done:
---

## Current state

Built: the table `saved_filters` (migration 33, tenant-bound, restrictive policies that hold a
person to their own filters and the shared ones), the routes `…/filters` and `…/filters/{filter}` —
list with a weak `ETag`, create with a key, read, edit with `If-Match`, share and unshare by the
edit, delete —, every change a recorded act ([`api/filters.go`](../../backend/internal/api/filters.go));
the parameters checked as the lists check them and checked again when read, a value that no longer
holds a warning ([ADR 0049](../adr/0049-filters-are-explicit-repeatable-query-parameters-no-query-language.md) D7),
another member's filter that names what the reader cannot see withheld; the backlog's filter bar
applies, saves — one key per content of the dialog — shares and deletes filters, a shared one
naming its owner ([`saved-filters.ts`](../../frontend/src/app/features/project/saved-filters.ts)),
and the tenant's ticket list (T53) applies them through the same bar, `project` included, a
condition a list leaves out named under the bar with why (`leftOut`); integration tests across two
persons and two tenants
([`api_filters_test.go`](../../backend/test/integration/api_filters_test.go)).

In the browser, [`filters.spec.ts`](../../frontend/e2e/filters.spec.ts) walks the backlog's bar
with two identities: a member saves the backlog's filter shared, and the administrator applies it
from the saved filters and sees its owner — in Chromium and WebKit, each in both schemes, in three
local runs of the whole tier with two workers on 2026-10-06.

Outstanding: the tenant board's filter bar does not apply them — the board carries the address's
`project` filter only; a tenant administrator cannot remove a shared filter of a person who left,
which stays shared until its owner deletes it.

## Required changes

1. The tenant board's filter bar applies, saves and shares a filter as the backlog's does, through
   `SavedFilters` — with what a board leaves out as its `leftOut` — and `toBacklog`'s counterpart
   for the board.
