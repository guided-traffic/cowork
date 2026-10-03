---
id: T38
title: there are no saved filters
state: decided
severity: low
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

The list filters exist as query parameters ([ADR 0049](../adr/0049-filters-are-explicit-repeatable-query-parameters-no-query-language.md));
nothing stores a set of them. [ADR 0018](../adr/0018-the-views-of-the-first-release.md) D5: a
saved filter is a named parameter set, owned by a person, optionally shared with the tenant,
applicable to the backlog, the tenant list and the tenant board; it carries a version
([ADR 0050](../adr/0050-optimistic-concurrency-a-version-per-entity-if-match-where-a-write-overwrites.md) D1).

## Required changes

1. The table (tenant-bound, row-level security), the routes (create, list, edit with
   `If-Match`, share, delete), recorded acts.
2. The filter bar of the backlog, the tenant list and the tenant board can save, apply and share
   a filter; a shared one shows its owner.
3. Integration tests across two persons and two tenants.
