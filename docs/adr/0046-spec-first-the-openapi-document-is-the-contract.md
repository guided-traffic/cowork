# ADR 0046: Spec-First — the OpenAPI Document Is the Contract, the Server Interface and Both Clients Are Generated From It, and Requests Are Validated Against It at Runtime

## Status

Accepted. Date: 2026-10-01. Decided by the owner as the answer to the catalog question
"spec-first or code-first?": spec-first with generated server interface and clients and
runtime validation, over code-first, over validation in tests only, and over hand-written
clients. The rules of D5–D8 were put to the owner with the question and not objected to.

**Not built.** No OpenAPI document; three hand-written routes.

## Context

The API has three consumers that must agree before a line of handler code exists: the
Angular UI with a generated TypeScript client, the MCP server and the tests with a generated
Go client ([ADR 0040](0040-rest-is-the-contract-mcp-is-the-ergonomic-surface-and-can-do-nothing-the-api-cannot.md)
D2), and a Claude session or script that loads the document from the installation (ADR 0040
D6). The shapes are already decided — path families ([ADR 0023](0023-the-tenant-is-in-the-path.md)),
`PUT` addresses, `from` preconditions and `If-Match` ([ADR 0045](0045-idempotency-put-where-it-is-free-a-required-key-on-agent-posts-stored-with-the-act.md)),
two exports ([ADR 0044](0044-two-endpoints-markdown-is-the-canonical-ticket-context-is-the-ticket-with-what-surrounds-it.md)).
A document written first is a diff a person can review before the implementation; a
document generated from code is a description of what was built. A document that is also
enforced at the boundary is a contract; one that is only published is documentation.

## Decision

**D1 — `backend/api/openapi.yaml` is the source of the API.** OpenAPI 3.1, one root document
with `$ref`s into one file per path family — `tenants.yaml`, `me.yaml`, `tickets.yaml`
(the key resolver), `auth.yaml`, `admin.yaml` — and `components/` for schemas, responses
and security schemes. A change to the API is a change to these files first.

**D2 — `oapi-codegen` generates the Go server interface and the Go client.** Handlers
implement the generated strict server interface; the MCP server and the integration tests use
the generated client. `make generate` runs it beside `sqlc` ([ADR 0027](0027-data-access-is-sqlc-over-pgx-behind-a-tenant-transaction-and-a-mutation-wrapper.md)
D1), and the same CI job fails on a diff.

**D3 — The Angular client is generated from the same document** into
`frontend/src/app/api/`, by `ng-openapi-gen` or an equivalent that emits typed services; it
is never edited by hand, and the frontend build regenerates it from the committed document.

**D4 — Requests and responses are validated against the document at runtime.** A middleware
(`kin-openapi`) validates every request's path, query, headers and body before the handler
and answers a schema violation with the API's error shape; responses are validated in the
test and development builds, so a handler that drifts from the document fails a test, not a
client.

**D5 — The document is served by the API** at `GET /api/v1/openapi.json`, unauthenticated,
with `info.version` equal to the backend version; the MCP server compares the major version
at start (ADR 0040 D5).

**D6 — Every operation has an `operationId`** (it becomes the Go method and the TypeScript
function), request and response examples, and its error responses declared through shared
`components/responses`. Security schemes `sessionCookie` and `bearerToken` are declared and
applied per operation, so the document says which routes a token may call.

**D7 — Versioning is in the path.** `/api/v1` is the first; a breaking change opens `/api/v2`
beside it and the old family stays until its clients are gone; there is no version header
and no content negotiation on versions.

**D8 — The document is part of the security documentation.** The security page of the API
reads auth schemes, scopes and error responses from the document, not from prose; a route
missing a security requirement is a lint failure (`spectral` with a ruleset that requires
it), run in `make lint`.

## Consequences

- One artefact is the contract for the UI, the MCP server, the tests, a script and a
  reviewer; a review of an API change reads YAML, not Go.
- Two generators in the build and a lint for the document; `make generate` and the
  drift-check job grow accordingly.
- D4 makes the document the boundary: a request the document does not describe never
  reaches a handler, and the error shape of the next record is what a violation returns.
- The document for sixty-odd routes is thousands of lines; D1's split keeps a path family
  reviewable on its own.
- `oapi-codegen`'s handling of `oneOf` and of `nullable` is a known awkwardness; the
  schemas are written to avoid `oneOf` where a discriminated object does, and the first
  route built proves the toolchain end to end before the rest are written.

## Alternatives Considered

- **Code-first** (`swag` annotations, or `huma` with its own router). One truth in the code;
  the document comes after the implementation, the Angular client changes with every handler,
  annotations drift silently, and `huma` replaces the stdlib mux of [ADR 0001](0001-two-containers-a-go-backend-and-an-nginx-frontend-installed-by-one-helm-chart.md).
  Lost.
- **Spec-first, validation in tests only.** Less runtime work for a check that costs
  microseconds; a request outside the schema reaches the handler until a test covers it.
  Lost.
- **Spec-first, hand-written clients.** One generator fewer; client drift becomes a review
  task for two clients. Lost.

## Residual risks

- The generators are dependencies with their own release cadence; Renovate moves them, and
  the drift-check job catches a generator that changes its output.
- D4's response validation in test builds only means a production handler can still emit a
  shape the document does not describe if no test reaches it; the integration tier covers
  every operation by D6's examples.

## References

- [ADR 0040](0040-rest-is-the-contract-mcp-is-the-ergonomic-surface-and-can-do-nothing-the-api-cannot.md) D2, D5, D6 — the clients and the served document
- [ADR 0023](0023-the-tenant-is-in-the-path.md), [ADR 0044](0044-two-endpoints-markdown-is-the-canonical-ticket-context-is-the-ticket-with-what-surrounds-it.md), [ADR 0045](0045-idempotency-put-where-it-is-free-a-required-key-on-agent-posts-stored-with-the-act.md) — the shapes the document encodes
- [ADR 0027](0027-data-access-is-sqlc-over-pgx-behind-a-tenant-transaction-and-a-mutation-wrapper.md) D1 — the sibling generator and the drift check
- [ADR 0003](0003-test-and-ci-policy.md) D1, D6 — `make` as the entry point, static analysis
