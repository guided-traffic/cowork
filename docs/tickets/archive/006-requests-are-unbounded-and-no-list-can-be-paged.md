---
id: T6
title: requests are unbounded, nginx answers a body above 1 MiB with its own HTML page, and no list can be paged because nothing can sign a cursor
state: done
severity: high
security: none
threat:
urgency: later        # rule 4: decided fix (the ADRs)
effort: M
blocked-by: T5
filed-from: the phase-2 conversion (T2)
opened: 2026-10-02
decided: 2026-10-02
done: 2026-10-02
shipped: the five request limits and the server key, signed cursors, numbered pages, the list builder and filters, nginx sized from the backend with its own problem bodies
---

## Current state

Decided by [ADR 0039](../adr/0039-no-request-budgets-size-and-time-limits-instead-configurable-and-switchable.md)
D1–D3, [ADR 0048](../adr/0048-cursor-pagination-on-every-list-numbered-pages-on-tables.md) D1–D7,
[ADR 0049](../adr/0049-filters-are-explicit-repeatable-query-parameters-no-query-language.md)
D1–D6, [ADR 0027](../adr/0027-data-access-is-sqlc-over-pgx-behind-a-tenant-transaction-and-a-mutation-wrapper.md)
D4, [ADR 0047](../adr/0047-errors-are-rfc-9457-problem-details-with-a-stable-code.md) D6 and
[ADR 0058](../adr/0058-postgresql-and-object-storage-are-external-the-chart-takes-references-with-configurable-keys.md)
D3. On `main`:

- [`config.go`](../../backend/internal/config/config.go) has none of ADR 0039 D2's five limits
  and no server key. ADR 0048 D1 signs every cursor "under the server key" and D5's cursor "does
  not expire", so a key made up per process will not do; the key ADR 0031 D1 names is
  `COWORK_SESSION_KEY`.
- The nginx template ([default.conf.template:32-42](../../frontend/nginx/default.conf.template#L32-L42))
  sets no `client_max_body_size` — nginx's 1 MiB default answers a larger body with its own HTML
  `413`, against ADR 0047 D6 — and a fixed `proxy_read_timeout 60s`; there is no `error_page`;
  the static-file regex ([:45-48](../../frontend/nginx/default.conf.template#L45-L48)) wins over
  the plain `/api/` prefix for an API path ending in `.png` or `.svg`.
- The image substitutes `BACKEND_URL` and `NGINX_LOCAL_RESOLVERS` "and no other"
  ([Containerfile:37](../../frontend/Containerfile#L37),
  [ADR 0001](../adr/0001-two-containers-a-go-backend-and-an-nginx-frontend-installed-by-one-helm-chart.md)
  D3), while ADR 0039 D3 has the chart size nginx from the backend's limits.
- ADR 0039 D2's request timeout would cut the hour-long event streams
  [ADR 0054](../adr/0054-server-sent-events-per-tenant-carry-keys-not-content-polling-is-the-fallback.md)
  D1, D6, D9 design.
- No list builder, no cursor, no filter parser.

## Required changes

1. **Configuration:** `COWORK_MAX_JSON_BODY` (`1MiB`), `COWORK_ATTACHMENT_MAX_BYTES` (`10MiB`),
   `COWORK_REQUEST_TIMEOUT` (`30s`), `COWORK_MAX_PAGE_SIZE` (`200`), `COWORK_MAX_QUERY_LENGTH`
   (`256`), each disabled by `0` (ADR 0039 D2); sizes in bytes or IEC units.
   `COWORK_SESSION_KEY` required: standard base64 of at least 32 bytes, its error naming the
   variable and never the value; the cursor key is derived from it under a fixed label, so the
   later session use of ADR 0031 D1 gets a key of its own. In place: ADR 0001 D7 (a further
   required variable), ADR 0048 D1 (the key is `COWORK_SESSION_KEY`, required from phase 2).
2. **Middleware:** a body limit per route class — `413`, a `Content-Length` above the limit
   refused before reading; the request timeout as a context deadline mapped to `504` with the
   transaction rolled back (not `http.TimeoutHandler`, which buffers and answers `503` text);
   an exemption the event stream uses (T20). ADR 0039 D2 amended in place: the event stream is
   exempt (ADR 0054 D1, D6, D9).
3. **Cursors and the list builder** (ADR 0027 D4, ADR 0048 D7): HMAC-signed cursors bound to the
   operation, its path parameters and its sort (`400 invalid_cursor` for a tampered or foreign
   one); `limit` clamped, never refused; table mode with `page`, `per_page` ∈ {25, 50, 100},
   `total`, and `400 page_too_deep` above 10 000 rows deep; `cursor` and `page` in one request →
   `400`; the modes each operation carries marked in the document (ADR 0048 D4); the predicates
   the ticket lists fill (tenant, project restriction, confidential, `deleted_at IS NULL`); the
   filter parser of ADR 0049 — repeatable, OR within a parameter, AND between, `!`, `me`, `none`,
   unknown parameters and out-of-vocabulary values refused.
4. **nginx:** two more substituted variables, for the body size and the read timeout, added to
   `NGINX_ENVSUBST_FILTER` (ADR 0001 D3 amended in place: four variables); `location ^~ /api/`;
   `error_page` with static problem bodies for `413`, `502`, `503`, `504` under `/api/` — no
   `instance`, no `request_id` — and never `proxy_intercept_errors`, which would replace the
   backend's own problems.
5. **Chart:** `backend.config.maxJsonBody`, `.attachmentMaxBytes`, `.requestTimeout`,
   `.maxPageSize`, `.maxQueryLength`; the two nginx variables set from them with headroom above
   the backend (ADR 0039 D3; a backend `0` becomes no limit for the body and a long finite
   timeout); `session.existingSecret` with `session.keys.key` — the only source ADR 0058 D3
   gives the session key, so the template fails without it, as it does without a database
   credential; every `ci/` values file sets it. `make run` generates a throw-away key when none
   is set.
6. **Tests:** configuration per variable (default, override, invalid, `0`, the key never
   echoed); body limit and timeout (the transaction rolled back); cursor sign, verify, tamper,
   another route, other path parameters; table-mode edges; the filter parser. nginx has no unit
   test, so both images run read-only together: a body above the backend's limit and below
   nginx's gets the backend's problem, one above nginx's gets nginx's problem body; a stopped
   backend gets the `502` problem; `/api/…/x.png` reaches the backend.
7. **Docs and records:** the README's configuration rows, the frontend's variables and the Helm
   values block; runtime.md (the limits, the `0` warning, what nginx answers); installation.md
   (the session key Secret; rotating it invalidates open cursors, ADR 0048's residual risk);
   adding-things.md ("A configuration variable" for a secret one, "A path nginx must treat
   differently": four substituted variables); trust-boundaries.md "The pods" (the substitution
   sentence); package-map.md's Containerfile row; architecture.md "Frontend container";
   CLAUDE.md's sentence on the substituted variables; ADR 0039 and ADR 0048 Status and index
   rows.

## Related

- T5 — the pipeline these middlewares join and the codes they answer with.
- T12 — the ticket lists fill the predicates and declare the filters.
- T19 — the upload size nginx must let through.
- T20 — the stream the timeout exempts.
