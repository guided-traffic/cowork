# ADR 0048: Opaque Cursor Pagination on Every List, Numbered Pages Additionally on Table-Like Lists — Streams Scroll, Tables Page

## Status

Accepted. Date: 2026-10-01. Decided by the owner as the answer to the catalog question
"pagination?", after the owner asked whether a UI without "page 2 of 5" would be readable:
cursor pagination on every list, and offset pagination with a total additionally on the lists
people read as tables — over cursor alone, over offset alone, and over keyset parameters
visible to the client. The rules of D5–D7 were put to the owner with the question and not
objected to.

**Not built.** No list route exists.

## Context

Ordering by id is ordering by time ([ADR 0022](0022-uuidv7-everywhere-sequences-only-for-ticket-numbers.md)
D5), the rank is a sortable string ([ADR 0014](0014-rank-is-the-decision-score-is-the-warning.md)
D2), every index leads with `tenant_id` ([ADR 0021](0021-row-level-security-is-the-second-line-of-tenant-isolation.md)
D4), and the page size is capped ([ADR 0039](0039-no-request-budgets-size-and-time-limits-instead-configurable-and-switchable.md)
D2). Two kinds of list exist in the views of [ADR 0018](0018-the-views-of-the-first-release.md):
streams a person reads from the top — activity, inbox, comments, search hits, the person-level
lists — where an insertion while reading must not shift anything and nobody expects a page
number; and tables a person scans and jumps around in — filtered ticket lists, the time
report, the audit view, members, tokens — where "page 2 of 5, 212 entries" is the right
form. A cursor serves the first exactly and cannot jump; an offset serves the second and
shifts rows in a stream. The sets behind the tables are bounded per tenant, so an offset
costs nothing noticeable there.

## Decision

**D1 — Every list route supports cursor pagination:** `?limit=` (default 50, clamped to
`COWORK_MAX_PAGE_SIZE`) and `?cursor=`; the response is `{items, next_cursor}` with
`next_cursor` null at the end. The cursor is opaque: base64url of the sort key(s) and the
last value — `id` for time-ordered lists, the rank string for the backlog, `(score, id)` for
score-ordered lists, `(rank, id)` for search — signed with HMAC under the server key
([ADR 0031](0031-server-side-sessions-in-an-httponly-cookie.md) D1) so it cannot be forged
or edited.

**D2 — Table-like lists additionally support numbered pages:** `?page=N&per_page=M`
(`per_page` from 25, 50, 100, clamped like `limit`), and the response then carries `total`
and `page`. The table-like lists are: ticket lists with filters (a project's backlog as a
list, the tenant-wide filtered list, saved filters), the time report, the audit view,
members, tokens, projects. The depth is capped: `page × per_page` above 10 000 answers
`400 page_too_deep` with the advice to filter.

**D3 — Streams are cursor-only:** activity, inbox, comments, search, `/me/next`,
`/me/assigned`, `/me/decisions`. They carry no `total` and no `page`.

**D4 — The UI follows the list's kind.** Tables show page numbers with jump and a `per_page`
choice; streams show "load more" or scroll. The generated clients
([ADR 0046](0046-spec-first-the-openapi-document-is-the-contract.md)) offer both helpers,
and the OpenAPI document marks per operation which modes it carries.

**D5 — A cursor is bound to its list and sort.** A cursor from one route presented to
another, or a tampered one, answers `400 invalid_cursor`. A cursor does not expire; it is a
position, not a session.

**D6 — Sort order is fixed per list;** `?order=asc|desc` exists only where both readings are
natural (comments, activity). There is no general `?sort=` in the first release.

**D7 — The list builder of [ADR 0027](0027-data-access-is-sqlc-over-pgx-behind-a-tenant-transaction-and-a-mutation-wrapper.md)
D4 implements both modes once;** each list declares its sort keys and whether it is a table,
and the builder renders the cursor or the offset form under the same tenant and project
predicates.

## Consequences

- Streams never shift under the reader; tables read like tables.
- Two modes in one builder; the sort keys a list declares for the cursor are the same ones
  the offset mode orders by.
- `total` is computed for tables only, where the filtered set is bounded; search and the
  person-level unions never count.
- The cap of D2 keeps a deep `OFFSET` from scanning a tenant's whole history; a person who
  needs row 20 000 of an audit view filters by period instead.

## Alternatives Considered

- **Cursor only, with previous/next and a counter but no jump** — the first recommendation.
  One mechanism; "page 2 of 5" and jumping would have been impossible where people expect
  them. Lost on the owner's question.
- **Offset only.** Page numbers everywhere; `OFFSET` scans grow with depth and rows shift
  between pages in exactly the streams people read most. Lost.
- **Keyset parameters visible to the client** (`after_id`, `after_rank`). No signature;
  every list a different parameter set, the client must know the sort, and an edited
  parameter yields surprising rather than foreign results. Lost.

## Residual risks

- D2's list of table-like routes is a judgement; a stream that turns out to be read as a
  table (the audit view may be both) is moved by amendment, not by a client guessing.
- The cursor's HMAC shares the server key with sessions; rotating it invalidates open
  cursors, which costs a client one refetch from the top.

## References

- [ADR 0022](0022-uuidv7-everywhere-sequences-only-for-ticket-numbers.md) D5, [ADR 0014](0014-rank-is-the-decision-score-is-the-warning.md) D2 — the sort keys
- [ADR 0018](0018-the-views-of-the-first-release.md) — which views are streams and which are tables
- [ADR 0027](0027-data-access-is-sqlc-over-pgx-behind-a-tenant-transaction-and-a-mutation-wrapper.md) D4 — the list builder
- [ADR 0039](0039-no-request-budgets-size-and-time-limits-instead-configurable-and-switchable.md) D2 — the page-size cap
- [ADR 0046](0046-spec-first-the-openapi-document-is-the-contract.md) — where the modes are declared
