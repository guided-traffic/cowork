# ADR 0046: Spec-First — the OpenAPI Document Is the Contract, the Server Interface and Both Clients Are Generated From It, and Requests Are Validated Against It at Runtime

## Status

Accepted. Date: 2026-10-01. Decided by the owner as the answer to the catalog question
"spec-first or code-first?": spec-first with generated server interface and clients and
runtime validation, over code-first, over validation in tests only, and over hand-written
clients. The rules of D5–D8 were put to the owner with the question and not objected to.

Amended 2026-10-02 (D1: the split files are bundled before generation; D2: the event stream
is served outside the generated server; D4: the validator leaves the security requirements to
the pipeline; D8: a Go test instead of `spectral`) and 2026-10-03 (D1: the families of the
login; D6, D8: the three forms of an operation's security requirement) and 2026-10-04 (D1: the
family `members.yaml` and the identity provider's paths; D6, D8: twelve session-only operations,
and the one operation that takes query parameters it does not declare), and again on 2026-10-04
for the chat in the UI (D1: the family `chat.yaml`; D2: a turn of the chat is served outside the
generated server like the event stream; D6, D8: thirteen session-only operations), and once more on
2026-10-04 for the global administrator's list of every tenant (D6, D8: fourteen session-only
operations), and by the owner's answers on the chat recorded in ADR 0076 (D1: `chat.yaml` gains
stopping the person's turns, `me.yaml` the chat's capabilities; D6, D8: sixteen session-only
operations), and on 2026-10-05 by the decision on the purge of a deleted ticket, built on the recommendation
([ADR 0024](0024-deletion-tickets-are-soft-deleted-and-purged-projects-archived-people-deactivated-tenants-deleted-explicitly.md)
D7; D6, D8: seventeen session-only operations). `oapi-codegen`
does not resolve references into other files, so the split document is bundled first; a stream
is not a response a strict handler returns; and the rule D8 wants checked is three assertions
over the loaded document, which a unit test makes without a Node toolchain in the backend's
lint.

**Partly built** (phase 2, 2026-10-02): D1, D2, D4 (responses validated in the test tier), D5,
D7 and D8; D3 since phase 3 (2026-10-03). D6's request and response examples
exist on a few operations only. The `sessionCookie` scheme is built since phase 3
(2026-10-03): the sessions of [ADR 0031](0031-server-side-sessions-in-an-httponly-cookie.md) exist,
and every operation declares which credential it takes.

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
and security schemes. A change to the API is a change to these files first. *(Amended
2026-10-02: the families built are `meta.yaml`, `me.yaml`, `tenants.yaml`, `tickets.yaml`,
`questions.yaml`, `comments.yaml`, `time.yaml`, `attachments.yaml` and `events.yaml`;
`make generate` bundles them into `backend/api/openapi.gen.json`, which the generator reads
and the server embeds and serves.)* *(Added 2026-10-03: `auth.yaml` and `accounts.yaml`; the
paths of the login — `/auth/options`, `/auth/local`, `/auth/logout` — are outside `/api/v1`, as
[ADR 0037](0037-csrf-origin-check-and-a-custom-header-on-unsafe-cookie-requests-no-cors.md) D5
names them.)* *(Added 2026-10-04: `members.yaml` — the members and their grants, the group
mappings, a project's restriction and access list — and, in `auth.yaml`, the identity provider's
start and callback, `/auth/oidc/login` and `/auth/callback`.)* *(Added 2026-10-04: `chat.yaml` —
the chat's availability and a turn, `/tenants/{tenant}/chat`, [ADR 0076](0076-the-chat-in-the-ui-runs-its-loop-in-the-backend-as-an-agent-of-the-person.md).)*
*(Added again 2026-10-04: in `chat.yaml` stopping the person's turns, `/tenants/{tenant}/chat/turns`;
in `me.yaml` the chat's capabilities, `/me/chat`.)*

**D2 — `oapi-codegen` generates the Go server interface and the Go client.** Handlers
implement the generated strict server interface; the MCP server and the integration tests use
the generated client. `make generate` runs it beside `sqlc` ([ADR 0027](0027-data-access-is-sqlc-over-pgx-behind-a-tenant-transaction-and-a-mutation-wrapper.md)
D1), and the same CI job fails on a diff. *(Added 2026-10-02: the event stream of
[ADR 0054](0054-server-sent-events-per-tenant-carry-keys-not-content-polling-is-the-fallback.md)
is documented but excluded from the generated server and served by a handler of its own,
after the same authentication, tenant boundary and request validation.)* *(Added 2026-10-04: so
is a turn of the chat, `runChatTurn`, which answers a stream as well — after the same steps and the
body limit; `exclude-operation-ids` names both in
[`api/oapi-codegen.yaml`](../../backend/api/oapi-codegen.yaml), and `skip-prune` keeps the models
of the turn's body and its events, which only the document's components name.)*

**D3 — The Angular client is generated from the same document** into
`frontend/src/app/api/`, by `ng-openapi-gen` or an equivalent that emits typed services; it
is never edited by hand, and the frontend build regenerates it from the committed document.
*(Amended 2026-10-03: `ng-openapi-gen` 1.x writes one function per operation and an `Api`
service that returns Promises, from `backend/api/openapi.gen.json`, configured by
[`ng-openapi-gen.json`](../../frontend/ng-openapi-gen.json). The output is committed like the
Go code and checked by `make frontend-generate-check` in the frontend job, instead of being
regenerated by the build: the image builds from `frontend/` alone and cannot read the backend's
document. Its root URL is the empty string, because the generated paths begin with `/api/v1`.)*

**D4 — Requests and responses are validated against the document at runtime.** A middleware
(`kin-openapi`) validates every request's path, query, headers and body before the handler
and answers a schema violation with the API's error shape; responses are validated in the
test and development builds, so a handler that drifts from the document fails a test, not a
client. *(Amended 2026-10-02: the validator does not check the security requirements — the
pipeline has authenticated the caller before it validates — because its own check reads every
body into memory first; a multipart body is left to the handler.)*

**D5 — The document is served by the API** at `GET /api/v1/openapi.json`, unauthenticated,
with `info.version` equal to the backend version; the MCP server compares the major version
at start (ADR 0040 D5).

**D6 — Every operation has an `operationId`** (it becomes the Go method and the TypeScript
function), request and response examples, and its error responses declared through shared
`components/responses`. Security schemes `sessionCookie` and `bearerToken` are declared and
applied per operation, so the document says which routes a token may call. *(Built 2026-10-03:
an operation has one of three forms — both schemes, which is the default; `sessionCookie` alone,
for the ~~six~~ routes a token must not call *(amended 2026-10-04: ~~twelve~~ ~~thirteen~~ ~~fourteen~~
~~sixteen~~, a turn of the chat, stopping one, choosing the chat's capabilities and the list of every
tenant among them — [ADR 0035](0035-personal-access-tokens.md) D5; amended 2026-10-05: seventeen,
the purge of a deleted ticket the seventeenth)*; or none, for the public ones — and the pipeline reads
the credentials an operation takes from its own requirement. A public write carries the
extension `x-cowork-origin-check: true`, which makes the pipeline hold it to the origin check of
[ADR 0037](0037-csrf-origin-check-and-a-custom-header-on-unsafe-cookie-requests-no-cors.md) D5.)*
*(Added 2026-10-04: the extension `x-cowork-open-query: true` lets an operation take query
parameters the document does not declare; only the identity provider's callback carries it, for
the parameters an issuer adds of its own, such as `iss` and `session_state`.)*

**D7 — Versioning is in the path.** `/api/v1` is the first; a breaking change opens `/api/v2`
beside it and the old family stays until its clients are gone; there is no version header
and no content negotiation on versions.

**D8 — The document is part of the security documentation.** The security page of the API
reads auth schemes, scopes and error responses from the document, not from prose; a route
missing a security requirement is a ~~lint failure (`spectral` with a ruleset that requires
it), run in `make lint`~~ *(amended 2026-10-02)* test failure: a unit test over the bundled
document ([`backend/api/document_test.go`](../../backend/api/document_test.go)) requires an
`operationId`, the bearer requirement (or an explicit empty one on the public operations), the
problem response and a tag on every operation. *(Amended 2026-10-03: the requirement is either
credential, the session cookie alone for exactly the ~~six~~ session-only operations *(amended
2026-10-04: ~~twelve~~ ~~thirteen~~ ~~fourteen~~ ~~sixteen~~; amended 2026-10-05: seventeen)*, or an explicit empty one on the public operations, which as writes also carry
`x-cowork-origin-check`.)* *(Added 2026-10-04: the same test holds `x-cowork-open-query` to the
callback alone.)*

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
