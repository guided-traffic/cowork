---
id: T5
title: the API has no contract — no OpenAPI document, no request validation, no request id, no code catalogue — and the developer pages name functions that do not exist
state: done
severity: high
security: none
threat:
urgency: later        # rule 4: decided fix (the ADRs)
effort: L
filed-from: the phase-2 conversion (T2)
opened: 2026-10-02
decided: 2026-10-02
done: 2026-10-02
shipped: the OpenAPI 3.1 document split per family, bundled and generated, requests and responses validated, problem details with a generated code catalogue and request ids
---

## Current state

Decided by [ADR 0046](../adr/0046-spec-first-the-openapi-document-is-the-contract.md) D1, D2,
D4–D8, [ADR 0047](../adr/0047-errors-are-rfc-9457-problem-details-with-a-stable-code.md) D1–D7,
[ADR 0040](../adr/0040-rest-is-the-contract-mcp-is-the-ergonomic-surface-and-can-do-nothing-the-api-cannot.md)
D2, D5, D6 and [ADR 0001](../adr/0001-two-containers-a-go-backend-and-an-nginx-frontend-installed-by-one-helm-chart.md)
D4. On `main`:

- No `backend/api/`, no generator, no validator; routes are registered by hand, twice per path
  for the `405` (`handleGet` in [`server.go`](../../backend/internal/httpserver/server.go)).
- `writeProblem` writes the envelope without `request_id` and `errors[]`; the request log has no
  request id; no handler recovers a panic.
- `/readyz` puts the database ping error into `detail`; a pgx error can name the host and the
  user (trust-boundaries.md), against ADR 0047 D3.
- `GET /api/v1/version` answers `buildTime`, while every JSON member an ADR names is snake_case
  (`request_id`, `next_cursor`, `recorded_by_agent`); the frontend's `VersionInfo`
  ([`version.service.ts`](../../frontend/src/app/core/version.service.ts)) and its specs mirror
  `buildTime`.
- `statusRecorder` has neither `Flush` nor `Unwrap`, so no response can stream through the
  request log.
- ADR 0046 D8's lint is `spectral`, a Node tool, and the job that runs `make lint` has no
  working Node (T1); the check it asks for — every operation declares a security requirement —
  needs no second linter: a Go unit test over the bundled document makes it.
- Verified upstream: oapi-codegen v2.8.0 generates a strict `net/http` server interface and a
  client from OpenAPI 3.1; kin-openapi bundles a split document into one, with internalised
  references and clean component names, and validates requests — but has no option to refuse
  undeclared query parameters.
- Pages that say what the code does not: [package-map.md:12](../developer/package-map.md#L12)
  and [adding-things.md:31-33](../developer/adding-things.md#L31-L33) name `writeError` (it is
  `writeProblem`); [adding-things.md:37-38](../developer/adding-things.md#L37-L38) waits for
  "question Q-E1", which ADR 0046 decided; [testing.md:31](../developer/testing.md#L31) names
  `apiErrorOf` and "the `error` object" (the code has `problemOf` and problem details) and
  [testing.md:33](../developer/testing.md#L33) says `/readyz` reports its error verbatim; the
  README's API section says every response carries `application/json` (errors carry
  `application/problem+json`); runtime.md's "Log" calls metrics "an open question in the
  planning catalog" ([ADR 0060](../adr/0060-prometheus-metrics-on-a-second-listener-with-servicemonitor-and-prometheusrule.md)
  decided them).

## Required changes

1. **The document:** `backend/api/openapi.yaml` (OpenAPI 3.1) with `$ref`s into one file per
   path family — `me.yaml`, `tenants.yaml`, `tickets.yaml` (the key resolver) — and
   `components/`; `/api/v1/version` and `/api/v1/openapi.json` in a `meta.yaml` family, which
   ADR 0046 D1 gets in place (`auth.yaml` and `admin.yaml` come with the login).
2. **Generation:** kin-openapi bundles the document; oapi-codegen generates the strict server
   interface and the Go client into `*.gen.go` files of one package that a later
   `cmd/cowork-mcp` can import (ADR 0040 D2); a small package at `backend/api` embeds the
   document; `make generate` runs both beside sqlc (T4); `fmt`, `lint`, `cyclo` and `gosec`
   cover the new directories, the scans skipping generated files by name.
3. **The request pipeline:** recover → `500 internal` with nothing internal in `detail`; a
   request id generated per request, sent as `X-Request-Id`, carried in every problem body and
   in the request log line (an inbound `X-Request-Id` is not trusted); request validation by
   kin-openapi → `400 validation_failed` with `errors[]` (`/field`, `query:<name>`,
   `header:<name>`); undeclared query parameters refused by a check of our own against the
   routed operation (ADR 0049 D4); authentication before validation (T9), so an unauthenticated
   caller cannot probe the schema; responses validated in tests (an `httpserver.Options`
   switch) and skipped for streamed bodies; `statusRecorder` gains `Flush` and `Unwrap`; a path
   parameter that does not parse answers `404 not_found`.
4. **The problem-code catalogue:** one YAML file in the backend mapping each `code` to its type
   URI, status and title (ADR 0047 D4); `make generate` emits the Go constants, the document's
   `components/responses` and the README's problem-code table; each child adds its codes there,
   one line each.
5. `GET /api/v1/openapi.json`: unauthenticated, `info.version` equal to the backend version
   (ADR 0046 D5, ADR 0040 D6). `GET /api/v1/version` enters the document and answers
   `build_time`; the frontend's `VersionInfo` and its specs follow.
6. `/readyz` answers a generic `detail` and logs the error.
7. **The security check** (ADR 0046 D8 amended in place): a Go unit test over the bundled
   document fails an operation without a security requirement (or an explicit `security: []`),
   an `operationId` or examples.
8. The README's API route table is generated from the document between markers, so the README
   keeps the reference ADR 0002 D2 gives it and the document stays the source (ADR 0046 D1);
   `make generate-check` covers it.
9. **Tests:** `request_id` equals `X-Request-Id` on every response, errors and successes;
   `errors[]` pointers for body, query and header; a panic answers `500` with nothing internal;
   a request the document does not describe never reaches a handler; an undeclared query
   parameter is refused; `openapi.json` needs no credential and carries the version; a handler
   that drifts from the document fails a test; the security check fails on a document with an
   undeclared operation.
10. **Docs and records:** adding-things.md ("An API endpoint" spec-first, "A problem code");
    package-map.md; testing.md (`problemOf`, the generated client, `/readyz`); architecture.md
    (the request pipeline); build-test-lint.md; conventions.md (snake_case JSON members, UTC
    timestamps); new docs/developer/api.md and its row in the developer README; the README's API
    section; CLAUDE.md's request-log sentence; runtime.md's "Log" (`request_id`; metrics are
    ADR 0060's); ADR 0046 and ADR 0047 Status and index rows.

## Not verified

- Whether the generated router and the validator keep `%2F` inside one path segment, as the
  standard mux does; T13's link route takes a percent-encoded full key there.

## Related

- T4 — `make generate` and the drift step it starts.
- T6 — the limits and the list modes join this pipeline.
- T9 — authentication runs before validation.
