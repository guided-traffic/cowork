---
id: T37
title: the search is not walked end to end, and where "that tenant first" puts the other tenants is unconfirmed
state: in-progress
severity: medium
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

`GET /api/v1/tenants/{tenant}/search` and `GET /api/v1/me/search` answer ranked hits with snippets
over titles, bodies, comments, questions, file names, keys by their beginning and titles by trigram,
every text under its ticket's visibility predicate
([ADR 0025](../adr/0025-search-is-postgresql-full-text-under-the-same-policy-as-the-data.md),
[`api/search.go`](../../backend/internal/api/search.go),
[`queries/read/search.sql`](../../backend/internal/store/queries/read/search.sql)); the integration
tier holds every hit and snippet to the tenant, the restriction and the confidential rule
([`api_search_test.go`](../../backend/test/integration/api_search_test.go)). The top bar's search box
opens, inside a tenant the person works in, that tenant's results, which offer *Search all your
tenants* as a link to `/me/search`; anywhere else the person's tenants
([`features/search/search.ts`](../../frontend/src/app/features/search/search.ts),
[`shell.ts`](../../frontend/src/app/layout/shell.ts) `search`). A hit in a comment or a question links
to `#comment-<id>` or `#question-<n>`, which the ticket's page scrolls to once that part has loaded.
The frontend's tests run on jsdom: neither the box, the results nor the scroll to a linked part has
run in a browser.

## Required changes

1. An end-to-end test ([`frontend/e2e/`](../../frontend/e2e/), ADR 0056) on the built images: a word
   typed into the box inside a tenant lists that tenant's hits with their snippets, *Search all your
   tenants* lists a second tenant's beside them for the person in both, a hit in a comment opens the
   ticket scrolled to that comment, and no content-security violation is reported, in Chromium and
   WebKit.

## Not verified

- How the ranking reads on real tickets, and how long a search of a tenant of thousands of tickets
  takes: neither was measured.

## Related

- T33 — the rendered Markdown, which has no end-to-end check either
