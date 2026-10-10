# Search

How a search finds tickets: the two routes, the one query that ranks and cuts snippets, the union
across a person's teams, the cursor, the search box and the results page — and how the search
relates to the ticket lists' `q` filter and the MCP tool `search`. The decisions are [ADR 0025]
(PostgreSQL full text under the same policy as the data), [ADR 0018] D7 (the search views),
[ADR 0023] D2 (`/me/search`) and [ADR 0021] D5 (the union one team at a time); what a search may
and may not reveal is [docs/security/tenancy.md](../security/tenancy.md#search-finds-only-what-its-reader-sees).
Read against the tree on 2026-10-05.

## The routes

| Route | Handler | Reads |
|---|---|---|
| `GET /api/v1/teams/{team}/search?q=` | `SearchTeam` in [`api/search.go`](../../backend/internal/api/search.go) | the team, after the boundary; a project-restricted token is admitted (`tenantWideForProjectTokens`) and narrowed to its project by `app.restricted_project_id` |
| `GET /api/v1/me/search?q=` | `SearchMyTeams` | every team of `personTenants` ([api.md](api.md#the-person-level-routes)), one `InTenant` each, the parts merged by rank in Go; `team=` narrows — or `tenant=`, its deprecated name, for one release |

Both take `q` (required; `searchQuery` refuses white space alone and more than
`COWORK_MAX_QUERY_LENGTH` characters with `400 validation_failed` at `query:q`), `limit` and
`cursor`, and answer `SearchHitList` — the API document, [`search.yaml`](../../backend/api/search.yaml),
is the reference for the fields.

## The query

`SearchTickets` in [`queries/read/search.sql`](../../backend/internal/store/queries/read/search.sql),
one statement per team:

| Part | Finds | Rank |
|---|---|---|
| the ticket's text | `tickets.search @@ plainto_tsquery('cowork_simple', q)` — the title (weight A) and the body (B) | `ts_rank`: 1.0 per hit in the title, 0.4 in the body (PostgreSQL's default weights) |
| a comment | `comments.search`, not a withdrawn comment | `ts_rank` of an unweighted vector, 0.1 |
| a question | `questions.search` — the question (A), the options, recommendation and answer (B) | `ts_rank` with every weight 0.1 |
| an attachment's name | `attachments.search`, the name and its words split at `.`, `-`, `_` (migration 31) | `ts_rank`, 0.1 |
| the key | `starts_with(<PROJECT>-<number>, key_prefix)`, the prefix `keyPrefix` takes from a query that is a key or the beginning of one (`COW-1`, `cow-12`, `acme/COW-12` in the team `acme` only) | 4 for the whole key, 2 for its beginning |
| a title by trigram | `q <% title` (pg_trgm's word similarity, threshold 0.6) | the similarity × 0.05 |

Every part reads `tickets` with `app_ticket_visible`, and so does the final read of the page's
tickets, which the lint of the query files checks ([data-access.md](data-access.md#visibility-in-sql)).
`best` keeps one row per ticket, its best match — on a tie the key, the ticket's own text, a question,
a comment, a file name, in that order. The page is cut after the cursor's place, ordered by rank and
ticket id descending, `LIMIT` one above the page; only then is the snippet taken, so `ts_headline`
runs over the page's rows alone: of the body for a hit in the ticket's text or its key, of the comment,
of the question with its options, recommendation and answer — at most two fragments of up to 18
words — and the file name as it is for an attachment, which the parser reads as one word. The bytes
`0x02` and `0x03` mark the words found; the text loses any it holds first, and `snippetParts` cuts
the snippet at them into `[{"text","match"}]` — text, never markup.

The key is not indexed: a short key is never stored ([ADR 0007] D3), and an index cannot join a
project's key to a ticket's number; the part reads the team's tickets with their projects, which
at the expected sizes is a few milliseconds. A query of no lexeme — punctuation alone — gives an empty
`tsquery`, which matches nothing and makes PostgreSQL send a notice; the key and the trigram still
apply.

## The cursor

A search's position is its last hit's rank and ticket id, `"<rank>/<id>"` (`searchPosition`), the rank
a `float32` written with as many digits as it needs, so it reads back to the same value the query
compares (`(rank, id) < (after_rank, after_id)`). The cursor is signed like every other
([api.md](api.md#paging)) and bound to the operation, the team — or the person and the narrowing —
and a hash of the query (`queryScope`), so a cursor of another query is `400 invalid_cursor`; the rank
is not sealed, because every text it was computed over is the reader's to see. Each team of the
union is read after the same position, at most one page and one more, and the merged hits are cut
again (`searchPage`); a hit that changes between two pages may move.

## The `q` filter and the MCP tool

The ticket lists' `q` (`TicketFilter.Query`, [data-access.md](data-access.md#the-ticket-list-builder))
is a **filter**: the tickets whose title and body hold every word, in the list's own order, combined
with the other filters, without a rank, a snippet, comments, questions or file names — the backlog's
search field uses it. The MCP tool `search` ([mcp.md](mcp.md)) is built on that filter too: it lists
the tickets of a project — in its rank —, a team — newest first — or every team — team after
team, each newest first — that match, at most 20, with the state and type filters the tool takes ([`tools/query.go`](../../backend/internal/tools/query.go)). Both stay as they are; the ranked search is the two routes above.

## In the browser

The top bar's search box ([`shell.html`](../../frontend/src/app/layout/shell.html), `search` in
[`shell.ts`](../../frontend/src/app/layout/shell.ts)): Enter opens `/t/<team>/search?q=` inside a
team the person works in — that team first — and `/me/search?q=` anywhere else and in a team a
global administrator only oversees. The box holds the words of the search the page shows
(`searchedFor`) and is empty elsewhere. The results page,
[`SearchResults`](../../frontend/src/app/features/search/search.ts), one component for both routes
(`data: { scope }`), reads its pages with `followPages`, fifty at a time, *Load more* for the next;
each hit shows — with its team on `/me/search` — the type, the short key, the title, the state,
where it was found (`foundIn`) and the snippet, each part by interpolation, the found ones in
`<mark>`. A hit links to the ticket's page, to `#comment-<id>` or `#question-<n>` for a hit there,
which the page scrolls to once that part has loaded (`linkedPart` in
[`ticket-detail.ts`](../../frontend/src/app/features/ticket/ticket-detail.ts)). The team's results
offer *Search all your teams* to a person with more than one. A search is a snapshot: the page does
not follow the event stream.

## Tests

| Test | Holds |
|---|---|
| `TestSearchFindsAndRanksWithSnippets` ([`api_search_test.go`](../../backend/test/integration/api_search_test.go)) | one hit per ticket, the title above the body above the rest; where each matched, the comment's id, the question's number, the snippets; a withdrawn comment not searched; a file name by its words; a key by its beginning, with and without the team; a title by trigram; the cursor walks the order and belongs to its query; white space, a long query, no `q` refused |
| `TestSearchNeverShowsWhatTheCallerCannotSee` | the team, the restricted project, the confidential ticket — in the body, a comment, a question, the options, a file name, the key and by trigram, for the team's search and the person's; the union across teams, its narrowing and its cursor; the restricted tokens; an assignee joining the circle |
| `TestKeyPrefix`, `TestSnippetParts`, `TestSearchPositionRoundTrips` ([`search_test.go`](../../backend/internal/api/search_test.go)) | the key's recognition, no `LIKE` wildcard passing; the snippet's parts; the rank read back exactly |
| [`search.spec.ts`](../../frontend/src/app/features/search/search.spec.ts), `shell.spec.ts` | the results page for both scopes, its links and fragments, the snippet as text, *Load more*, a failure; the box's target in and outside a team and under oversight, the words of the page shown |
| [`e2e/search.spec.ts`](../../frontend/e2e/search.spec.ts) (end-to-end) | in Chromium and WebKit, both schemes: the box inside a team lists that team's hits, where each was found and the word marked in its snippet; *Search all your teams* adds a second team's hit with its team; a comment's hit opens the ticket scrolled to the comment; the shell's content-security policy refuses nothing ([testing.md](testing.md#end-to-end-tests)) |

Not verified: how the ranking reads on real tickets, and how long a search of a team of thousands
of tickets takes — neither was measured.

[ADR 0007]: ../adr/0007-a-ticket-key-is-globally-unique-tenant-slash-project-dash-number.md
[ADR 0018]: ../adr/0018-the-views-of-the-first-release.md
[ADR 0021]: ../adr/0021-row-level-security-is-the-second-line-of-tenant-isolation.md
[ADR 0023]: ../adr/0023-the-tenant-is-in-the-path.md
[ADR 0025]: ../adr/0025-search-is-postgresql-full-text-under-the-same-policy-as-the-data.md
