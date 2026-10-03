---
id: T40
title: the audit view, members, tokens and projects have no numbered pages, and the tenant has no administration pages
state: decided
severity: low
security: none
threat:
urgency: later        # rule 4: decided fix
effort: S
blocked-by: T27
filed-from: T26
opened: 2026-10-03
decided: 2026-10-03
done:
---

## Current state

The ticket lists take `page` and `per_page`; the audit view, the member list, the token list
and the project list page by cursor only. [ADR 0048](../adr/0048-cursor-pagination-on-every-list-numbered-pages-on-tables.md)
D2 (numbered pages on tables) is carried over from phase 2. The browser shows the members
(T28) and nothing else of the tenant's administration.

## Required changes

1. `page`/`per_page` with `total` on the audit view, members, tokens and projects.
2. The tenant's administration pages: settings (`updateTenant` with `If-Match`), projects
   (create, edit, archive, WIP limits), members with their roles and origin, the audit view with
   its filters, the tenant's tokens for administrators (metadata, revoke;
   [ADR 0035](../adr/0035-personal-access-tokens.md) D5).
3. Unit tests per page; integration tests for the paging.
