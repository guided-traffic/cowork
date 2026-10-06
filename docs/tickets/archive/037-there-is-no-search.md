---
id: T37
title: the search is not walked end to end, and where "that tenant first" puts the other tenants is unconfirmed
state: done
severity: medium
security: none
threat:
urgency: later        # rule 4: decided fix
effort: S
blocked-by:
filed-from: T26
opened: 2026-10-03
decided: 2026-10-03
done: 2026-10-06
shipped: frontend/e2e/search.spec.ts, the top bar's search of a tenant and of every tenant of the person and a comment's hit opening its comment, with no content-security violation, in Chromium and WebKit and both colour schemes
---

## Current state

`GET /api/v1/tenants/{tenant}/search` and `GET /api/v1/me/search` answer ranked hits with snippets
under every ticket's visibility predicate
([ADR 0025](../adr/0025-search-is-postgresql-full-text-under-the-same-policy-as-the-data.md),
[search.md](../developer/search.md)). The top bar's box opens, inside a tenant the person works in,
that tenant's results, which offer *Search all your tenants*. [`search.spec.ts`](../../frontend/e2e/search.spec.ts)
types a word into the box inside a tenant: that tenant's hits show where each was found and the word
marked in its snippet, and not a second tenant's; *Search all your tenants* lists the second
tenant's hit beside them with its tenant; a hit in a comment opens the ticket scrolled to that
comment, which the plain address leaves below the fold; no content-security violation is reported —
in Chromium and WebKit, each in both schemes, in three local runs of the whole tier with two workers
on 2026-10-06. The ranking on real tickets and the time over a large tenant are
[search.md](../developer/search.md#tests)'s *Not verified*.

## Required changes

None.

## Related

- T33 — the rendered Markdown, whose path watches the same policy
