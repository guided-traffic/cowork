# ADR 0049: Filters Are Explicit, Repeatable Query Parameters — AND Between Fields, OR Within a Field, Negation by Prefix, No Query Language

## Status

Accepted. Date: 2026-10-01. Decided by the owner as the answer to the catalog question
"filtering?": explicit query parameters, over a query mini-language, over a JSON filter body,
and over an `or=` group parameter. The rules of D4–D7 were put to the owner with the
question and not objected to.

Amended 2026-10-02 (D1: what `blocked` means beside `state=blocked`, and which values `!`
negates) and 2026-10-03 (D1: `done_after`, for the board's count of the tickets done in the
last fourteen days, [ADR 0018](0018-the-views-of-the-first-release.md) D1 as amended that
day). `blocked=true` as "the state is blocked" would repeat `state=blocked`; the reading
that adds something is the prerequisite one of [ADR 0012](0012-four-typed-directed-links-within-a-tenant.md)
D1.

**Built** (phase 2, 2026-10-02; `done_after` since 2026-10-03): D1–D6 on the project's and the tenant's ticket lists,
[`ticketlist.go`](../../backend/internal/api/ticketlist.go) parsing and
[`tickets.go`](../../backend/internal/store/tickets.go) rendering; ~~D6's board, dashboard and
saved filters and D7 arrive with their views~~. *(2026-10-05:)* D6's saved filters and D7: a saved
filter's parameters are a JSON object of the same names, refused as the lists refuse them
(`parseFilters`, the pointer `/parameters/<name>`), and checked again whenever the filter is read, a
value that no longer holds a warning beside it ([`api/filters.go`](../../backend/internal/api/filters.go)).
The backlog applies them, and *(2026-10-05)* so does the tenant-wide list in the browser,
`/t/{slug}/tickets`, whose address carries every parameter of D1 — D6's one set, `project`
included —, so that a saved filter applied there is a link
([`features/tenant/tenant-tickets.ts`](../../frontend/src/app/features/tenant/tenant-tickets.ts));
the board does not yet. *(Built 2026-10-05:)* D6 on the dashboard of [ADR 0018](0018-the-views-of-the-first-release.md)
D6 — `project` as the ticket lists take it, and as its period parameters the time lists' `from` and
`to`, days ([`dashboard.go`](../../backend/internal/api/dashboard.go) `parseDashboardQuery`).

## Context

Saved filters are named parameter sets ([ADR 0018](0018-the-views-of-the-first-release.md)
D5), so the parameters are a contract; the list builder renders them under the tenant and
project predicates ([ADR 0027](0027-data-access-is-sqlc-over-pgx-behind-a-tenant-transaction-and-a-mutation-wrapper.md)
D4, [ADR 0048](0048-cursor-pagination-on-every-list-numbered-pages-on-tables.md) D7); the
OpenAPI document validates every parameter at the boundary ([ADR 0046](0046-spec-first-the-openapi-document-is-the-contract.md)
D4). A query language would be expressive and unvalidatable, a thing Claude would have to
learn and a string a saved filter could not inspect; a JSON body would take filters out of
the URL. The views of the first release need conjunctions of per-field disjunctions and
nothing more.

## Decision

**D1 — A filter is a set of query parameters, each repeatable; values of one parameter are
OR-ed, parameters are AND-ed.** The parameters on ticket lists:

| Parameter | Values |
|---|---|
| `state` | the states of [ADR 0009](0009-ticket-states-are-the-frontmatter-states-plus-blocked.md) |
| `type` | the types of [ADR 0008](0008-five-ticket-types-and-an-optional-parent-in-the-same-project.md) |
| `severity`, `security`, `urgency`, `effort` | the vocabularies of [ADR 0010](0010-the-frontmatter-vocabularies-become-ticket-columns.md) |
| `assignee`, `reporter` | a person id, or `me`; `none` for unassigned |
| `interest` | `me` (the caller has any interest), `any` (anyone has) |
| `blocked` | `true`, `false` — *(made concrete 2026-10-02)* whether an open ticket the caller can see is a direct `blocks` source of the ticket |
| `project` | a project key; on tenant-wide lists |
| `parent` | a ticket key, or `none` for roots |
| `progress_min`, `progress_max` | 0–100 |
| `has_open_questions` | `true`, `false` |
| `opened_after`, `opened_before`, `updated_after`, `updated_before` | RFC 3339 timestamps |
| `done_after` | an RFC 3339 timestamp: done after it — the bound excluded, as in the four above *(added 2026-10-03)* |
| `q` | full text ([ADR 0025](0025-search-is-postgresql-full-text-under-the-same-policy-as-the-data.md)), length-capped |
| `include_terminal` | `true` to include `done` and `dropped`; the default hides them unless `state` names them |

**D2 — Negation is a `!` prefix on a value** (`state=!blocked`, `assignee=!me`); negated and
plain values of one parameter combine as "any of the plain, none of the negated". *(Made
concrete 2026-10-02: every repeatable parameter takes it — the vocabularies, `project`,
`assignee`, `reporter`, `parent` (`!none`: has a parent) and `interest` (`!any`: nobody holds
a stake); `assignee=!me` keeps the unassigned tickets. The booleans take `true` and `false`.)*

**D3 — There is no query language and no filter body.** No `q=state:open …` grammar beyond
full text in `q`, no `POST …/search` with a filter tree, no OR across different fields in the
first release.

**D4 — Unknown parameters and out-of-vocabulary values are refused** with
`400 validation_failed` and `errors[]` naming the parameter ([ADR 0047](0047-errors-are-rfc-9457-problem-details-with-a-stable-code.md)
D7), never ignored: a typo in a saved filter must be seen.

**D5 — `me` resolves to the person of the caller,** for a session and for a token alike — an
agent's `assignee=me` is its person.

**D6 — One parameter set serves every ticket view:** the project backlog as a list, the
tenant-wide list, the board (as a pre-filter), the dashboard (`project` and the period
parameters), and the saved filters.

**D7 — A saved filter is validated when it is loaded** against the current vocabularies and
shows a warning for a value that no longer exists, so an enum migration does not silently
empty a filter.

## Consequences

- Every filter is a readable, shareable, bookmarkable URL and a validated parameter in the
  document; the generated clients expose typed parameters.
- Claude filters without learning a grammar; a saved filter is a structure the UI can edit
  field by field.
- The builder renders one SQL fragment per parameter under the tenant and project predicates;
  OR within a field is an `IN` or an `= ANY`, negation a `NOT`.
- OR across fields ("blocked, or urgent") is not expressible; the first saved filter that
  needs it is the amendment that adds an `or=` group.

## Alternatives Considered

- **A query mini-language** (`state:open severity:>=high -blocked`). Expressive; a parser,
  its errors, its documentation, no boundary validation, a string instead of a structure in
  saved filters. Lost.
- **A JSON filter tree on `POST …/search`.** Arbitrary nesting; filters leave the URL and
  lose caching and sharing, for trees nobody needs. Lost.
- **Explicit parameters plus an `or=` group.** The one case D3 excludes; a mini-syntax
  through the back door. Deferred to an amendment on evidence.

## Residual risks

- D1's table grows with features (labels, if they come); each addition is a document change
  and a builder case.
- `updated_after` relies on an `updated_at` the ticket keeps for any change to itself, its
  body or its direct children; the data record defines which writes touch it.

## References

- [ADR 0018](0018-the-views-of-the-first-release.md) D5 — saved filters as parameter sets
- [ADR 0027](0027-data-access-is-sqlc-over-pgx-behind-a-tenant-transaction-and-a-mutation-wrapper.md) D4, [ADR 0048](0048-cursor-pagination-on-every-list-numbered-pages-on-tables.md) D7 — the builder
- [ADR 0046](0046-spec-first-the-openapi-document-is-the-contract.md) D4, [ADR 0047](0047-errors-are-rfc-9457-problem-details-with-a-stable-code.md) D7 — validation and its errors
- [ADR 0009](0009-ticket-states-are-the-frontmatter-states-plus-blocked.md), [ADR 0010](0010-the-frontmatter-vocabularies-become-ticket-columns.md), [ADR 0025](0025-search-is-postgresql-full-text-under-the-same-policy-as-the-data.md) — the vocabularies and the full text behind the parameters
