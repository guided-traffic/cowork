# ADR 0047: Errors Are RFC 9457 Problem Details With a Stable `code`, a `request_id` and Field Errors

## Status

Accepted. Date: 2026-10-01. Decided by the owner as the answer to the catalog question "error
shape?": RFC 9457 `application/problem+json` with the extensions `code`, `request_id` and
`errors[]`, over keeping the skeleton's own shape, over the bare standard without
extensions, and over a constant `type`. The rules of D5–D7 were put to the owner with the
question and not objected to.

**Partly built.** Verified in the working tree on 2026-10-01: the backend's three routes
answer problem details with `type`, `title`, `status`, `detail`, `instance` and `code`
([`backend/internal/httpserver/server.go`](../../backend/internal/httpserver/server.go),
`writeProblem`), and the tests check the envelope. Not built: `request_id` (no request id
middleware yet), `errors[]` (no validation middleware yet), the code catalogue, nginx's
`error_page` bodies.

## Context

The skeleton answered `{"error":{"code","message"}}` and called it provisional. Earlier
records have already named error codes — `state_conflict`, `idempotency_mismatch`,
`agent_forbidden`, `csrf`, `not_found` — and [ADR 0046](0046-spec-first-the-openapi-document-is-the-contract.md)
D4 makes a validation middleware the producer of many more. One shape for every error, with
a media type a client can recognise, a status in the body for the proxy that loses it, a
machine identifier code switches on, and a place for field-level validation results — before
the first real route exists.

## Decision

**D1 — Every error response is `application/problem+json`** with the members of RFC 9457:
`type` (a URI under `https://cowork.dev/problems/<code-with-hyphens>`; an identifier, not a
link that must resolve), `title` (the code in words), `status` (equal to the HTTP status),
`detail` (for a person), `instance` (the request path).

**D2 — Three extension members.** `code`: a stable snake_case identifier clients switch on;
`request_id`: the id of the request, also sent as the `X-Request-Id` response header and
written into the request log line; `errors[]`: for validation failures, one entry per field
with `pointer` (a JSON pointer into the body, or `query:<name>` / `header:<name>`) and
`message`.

**D3 — `detail` is for people and carries no secret,** no stack trace, no SQL, no internal
path, no token or password. A `500` says "internal error" and the `request_id`; the log has
the rest.

**D4 — The code catalogue is one file in the backend,** mapping each `code` to its `type`
URI, default `status` and `title`; the OpenAPI document's `components/responses` and a
documentation page are generated from it, so the three cannot disagree.

**D5 — `404` does not distinguish.** An unknown tenant, a missing membership and a missing
ticket all answer `not_found` ([ADR 0023](0023-the-tenant-is-in-the-path.md) D5); nothing in
`detail` says which.

**D6 — nginx answers in the same shape for what it answers itself.** The `502` while the
backend is down ([ADR 0039](0039-no-request-budgets-size-and-time-limits-instead-configurable-and-switchable.md)
D3) and the `413` above `client_max_body_size` carry a static `problem+json` body through
`error_page`, so a client never sees an HTML error page from `/api/`.

**D7 — The validation middleware of ADR 0046 D4 produces `400 validation_failed`** with
`errors[]`; a handler never hand-rolls a validation error for what the document already
describes.

## Consequences

- Clients and tooling know the envelope; the UI reads `code` and `errors[]` for forms; the
  MCP server passes `code` and `detail` through as the tool's error ([ADR 0040](0040-rest-is-the-contract-mcp-is-the-ergonomic-surface-and-can-do-nothing-the-api-cannot.md)
  D3).
- Every error names its request; a report quotes a `request_id` and the log finds it.
- D4 adds a generator to `make generate`; a new code is one line, and the document and the
  page follow.
- The three skeleton routes changed shape in the change that wrote this record; the README's
  reference and the developer page were updated with it.

## Alternatives Considered

- **Keep the skeleton's `{"error":{"code","message"}}`,** extended with details. Nothing to
  change; a home-grown shape without a media type, without `status` in the body and without
  a standard for field errors. Lost.
- **Bare RFC 9457 without extensions.** Pure standard; clients would match on `type` URIs and
  field errors would have no place — the RFC provides for extensions. Lost.
- **`type: about:blank` everywhere, `code` alone.** Conformant; `type` carries nothing and
  standard clients that match on it see one value. The URI list is one file. Lost.

## Residual risks

- `type` URIs under `cowork.dev` are identifiers; if the domain ever hosts something, the
  paths should resolve to the generated page of D4. Not required.
- D6 depends on nginx's `error_page` for `/api/` only; a static HTML page is still what a
  browser gets for the UI's own `502`, which is intended.

## References

- [`backend/internal/httpserver/server.go`](../../backend/internal/httpserver/server.go) — `writeProblem`, the three routes
- [ADR 0046](0046-spec-first-the-openapi-document-is-the-contract.md) D4, D6 — validation and `components/responses`
- [ADR 0023](0023-the-tenant-is-in-the-path.md) D5, [ADR 0039](0039-no-request-budgets-size-and-time-limits-instead-configurable-and-switchable.md) D3, [ADR 0045](0045-idempotency-put-where-it-is-free-a-required-key-on-agent-posts-stored-with-the-act.md) — codes this record gives a shape
