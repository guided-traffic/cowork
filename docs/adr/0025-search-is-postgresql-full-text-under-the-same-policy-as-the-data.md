# ADR 0025: Search Is PostgreSQL Full Text Under the Same Policy as the Data — Tenant-Led Indexes, the `simple` Dictionary, Trigram for Keys and Titles

## Status

Accepted. Date: 2026-10-01. Decided by the owner as the answer to the catalog question
"search?": PostgreSQL only, over an external engine and over "PostgreSQL now, engine later".
The rules of D5 were put to the owner with the question and not objected to.

**Not built.** No searchable table exists.

## Context

[ADR 0018](0018-the-views-of-the-first-release.md) D7 asks for full-text search over titles,
bodies, comments and question texts inside a tenant and as a union over a person's tenants.
[ADR 0021](0021-row-level-security-is-the-second-line-of-tenant-isolation.md) D4 asks that
indexes lead with `tenant_id`, and its whole point is that a tenant's data is filtered by
the engine, not by application discipline — an external search index would be the one place
where isolation becomes a filter again. [ADR 0011](0011-a-ticket-is-a-markdown-body-plus-first-class-open-questions.md)
D1 replaces the body on every update, so the index has to follow in the same transaction.

## Decision

**D1 — Search runs in PostgreSQL, in the same transaction context as every other query.**
No external engine, no synchronisation, no second index of tenant data.

**D2 — Every searchable table carries a stored, generated `tsvector` column** built from its
text fields with the `simple` dictionary after `unaccent`, and a GIN index led by
`tenant_id` (`btree_gin`). Tickets: title and body; comments: text; questions: question,
options, recommendation, answer. The column is generated, so a body replacement updates it in
the same statement.

**D3 — Keys and titles are additionally indexed with `pg_trgm`,** led by `tenant_id`, so a
partial key (`VKO-1`) and a misspelt title still find their ticket.

**D4 — Ranking is `ts_rank` with title weighted above body above comments;** the snippet is
`ts_headline`. Across a person's tenants the union of per-tenant results is merged by rank
([ADR 0021](0021-row-level-security-is-the-second-line-of-tenant-isolation.md) D5).

**D5 — What a hit shows, and what is not searched.** A hit shows the tenant (in `/me`), the
key, the title, the snippet, the type and the state; a hit in a comment or a question shows
the ticket with that comment or question linked. Withdrawn comments
([ADR 0015](0015-comments-are-a-thread-and-activity-is-a-separate-list.md) D3) and soft-deleted
tickets ([ADR 0024](0024-deletion-tickets-are-soft-deleted-and-purged-projects-archived-people-deactivated-tenants-deleted-explicitly.md)
D1) are excluded by the query, not by the index. Attachment contents are not searched; the
file name is part of the ticket's searchable text.

**D6 — The dictionary is `simple`, deliberately.** The owner's tickets mix German, English
and code identifiers; a language stemmer would fold `Deployment` and `deploy`, or miss
`readyReplicas` entirely. `simple` with `unaccent` is predictable; a language-aware
configuration is an amendment when a tenant asks.

## Consequences

- No second stateful component; the chart already carries the database and, by
  [ADR 0016](0016-attachments-live-in-s3-compatible-storage-and-are-served-only-through-the-backend.md),
  an object store.
- Three extensions in the first migration that needs them: `unaccent`, `pg_trgm`,
  `btree_gin`. All three ship with PostgreSQL; the integration tier asserts they are
  available.
- Search results are isolated by the same policy as everything else, and consistent with
  the transaction that changed the data.
- No typo tolerance beyond trigram, no facets server-side; the saved filters of ADR 0018 D5
  are the facets.
- The GIN index grows with comments; at the expected sizes that is megabytes.

## Alternatives Considered

- **An external engine** (Meilisearch, Typesense, OpenSearch). Better relevance and facets;
  a second stateful set, an outbox or triggers to keep it in step, tenant isolation rebuilt
  as a filter, a second backup. Lost.
- **PostgreSQL now, an engine as a later record.** The door is always open; saying so adds
  nothing. Lost as a distinct option.
- **A language dictionary (`german`, `english`).** Stemming that surprises on mixed text and
  code. Lost to D6.

## Residual risks

- Ranking quality is what `ts_rank` gives; if it disappoints, weights and a `websearch_to_tsquery`
  grammar are the first amendments.
- Generated `tsvector` columns recompute on every body write; a very large body makes a
  write slightly slower. Bodies are analyses, not books.

## References

- [ADR 0018](0018-the-views-of-the-first-release.md) D7 — the search views
- [ADR 0021](0021-row-level-security-is-the-second-line-of-tenant-isolation.md) D4, D5 — tenant-led indexes and the union
- [ADR 0011](0011-a-ticket-is-a-markdown-body-plus-first-class-open-questions.md) D1 — the body replacement the index follows
