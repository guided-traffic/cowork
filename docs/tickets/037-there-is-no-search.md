---
id: T37
title: there is no search box — full text reaches title and body of one tenant's list only
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

The ticket lists take `q`, full text over the ticket's own title and body
([`tickets.go`](../../backend/internal/store/tickets.go)); comments, questions and attachments
carry their own `tsvector` columns that no route searches. The backlog of T28 sends `q`.
[ADR 0018](../adr/0018-the-views-of-the-first-release.md) D7 and
[ADR 0025](../adr/0025-search-is-postgresql-full-text-under-the-same-policy-as-the-data.md) decide
search over title, body, comments and question texts, within a tenant and across the person's
tenants (`/api/v1/me/search`, [ADR 0023](../adr/0023-the-tenant-is-in-the-path.md) D2).

## Required changes

1. The tenant search route and `/api/v1/me/search`: ranked hits with snippets (ADR 0025 D4, D5),
   keys and titles also by trigram (D3), under the visibility predicate.
2. The search box in the top bar (from any page, the person's tenants; inside a tenant, that
   tenant first) and a results page.
3. Integration tests that a hit never crosses a tenant, a restriction or the confidential rule.
