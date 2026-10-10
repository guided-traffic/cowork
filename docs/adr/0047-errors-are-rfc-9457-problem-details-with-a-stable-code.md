# ADR 0047: Errors Are RFC 9457 Problem Details With a Stable `code`, a `request_id` and Field Errors

## Status

Accepted. Date: 2026-10-01. Decided by the owner as the answer to the catalog question "error
shape?": RFC 9457 `application/problem+json` with the extensions `code`, `request_id` and
`errors[]`, over keeping the skeleton's own shape, over the bare standard without
extensions, and over a constant `type`. The rules of D5–D7 were put to the owner with the
question and not objected to.

**Built** (phase 2, 2026-10-02): every rule — the envelope with `request_id` and `errors[]` on
every route, the catalogue in [`internal/problem`](../../backend/internal/problem/problem.go)
that generates the document's enum and the README's table, the indistinguishable `404`, and
nginx's static problem bodies for what it answers itself ~~(`413`, `502`, `503`, `504`, without
`instance` and `request_id`; the code `backend_unreachable` is nginx's alone)~~ *(since
2026-10-04 the `404` for an API path that reaches the frontend, without `instance` and
`request_id`; `backend_unreachable` left the catalogue, D6)*.

Amended 2026-10-04 for the chat in the UI
([ADR 0076](0076-the-chat-in-the-ui-runs-its-loop-in-the-backend-as-an-agent-of-the-person.md)), and
built the same day (D1: an error after a stream's answer has begun; D4: three codes of the chat in
the catalogue — `chat_unavailable` `409`, the tenant has no chat ~~or its consent ended while a turn
ran~~ *(amended again 2026-10-04 with the owner's answers recorded in ADR 0076: there is no consent; the
installation configures no provider)*; `chat_busy` `429`, the person runs as many turns as `COWORK_CHAT_TURNS_PER_PERSON` allows;
`chat_provider_failed` `502`, the provider could not be reached, refused or answered what cowork
cannot read — and `not_ready` widened from "the backend cannot reach its database" to work the
backend cannot do now: no database, no event stream, or a turn of the chat its shutdown ends).

Amended 2026-10-04 by the owner's decision on the routing recorded in
[ADR 0001](0001-two-containers-a-go-backend-and-an-nginx-frontend-installed-by-one-helm-chart.md)
D3, and built the same day (D6: the Ingress routes `/api/` and `/auth/` to the backend, so nginx
answers only the `404` of a path that reaches the frontend by mistake, and what the Ingress
controller answers itself is the controller's page; D4: `backend_unreachable`, which nginx alone
answered, left the catalogue).

**Amended 2026-10-10 by the owner (not built):** the code `tenant_slug_taken` is renamed
`team_slug_taken` in the release that contracts the rename of a tenant to a team
([ADR 0005](0005-a-tenant-is-a-client-organisation-and-the-isolation-unit.md) D1); until then it keeps
its name, its title says "Team slug taken", and that release's own clients take both codes. A code
stays stable otherwise: this is the one rename, decided with the word it carries.

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
`detail` (for a person), `instance` (the request path). *(Amended 2026-10-04: a turn of the chat
answers `200` with a stream once it has begun, so what fails after that — the provider, the turn's
time ~~and the consent withdrawn~~ — is the stream's `error` event, whose data is the same problem body
with its `request_id` (`problem.BodyOf`); its `status` names the failure, not the response's. What
fails before the stream begins is a problem response like any. *(Amended again 2026-10-04, ADR 0076:
a turn the person stops is no failure — its stream ends with `done` and the reason `stopped`, and no
`error` event.)*)*

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

**D6 — nginx answers in the same shape for what it answers itself.** ~~The `502` while the
backend is down ([ADR 0039](0039-no-request-budgets-size-and-time-limits-instead-configurable-and-switchable.md)
D3) and the `413` above `client_max_body_size` carry a static `problem+json` body through
`error_page`, so a client never sees an HTML error page from `/api/`.~~ *(Amended 2026-10-04 by
the owner, ADR 0001 D3: the Ingress routes `/api/` and `/auth/` to the backend, and the frontend's
nginx never reaches it. What nginx answers itself is a request for `/api/` or `/auth/` that reaches
the frontend by mistake — an Ingress that sends every path there —, and that is a static
`404 not_found` problem whose `detail` names the cause, without `instance` and `request_id`, a body
above nginx's own limit included, never the UI shell. The code `backend_unreachable`, which only
nginx's proxy answered, is no longer answered by anything and left the catalogue; the UI keeps the
name for a request that reached no backend. What the Ingress controller answers itself — a `502`
or `503` without a ready backend pod, a `413` above its body limit, a `504` past its read timeout —
is the controller's own page, not a problem body: the chart does not know the controller, and its
limits are documented to sit above the backend's so that the backend answers its own `413` and
`504` ([ADR 0039](0039-no-request-budgets-size-and-time-limits-instead-configurable-and-switchable.md)
D3 as amended). A client reads such an answer by its status. Since the backend answers every error
with a problem body, the UI shows a `502`, `503` or `504` without one as the backend out of reach —
"The backend cannot be reached: The Ingress answered 503: no backend took the request. cowork tries
again on its own." —, as it shows a request that got no answer at all, and any other status without
one as an unexpected answer that names it
([`problem.service.ts`](../../frontend/src/app/core/problem.service.ts) `read`); `cowork-mcp` reports
an answer without a problem body with its status.)*

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
- ~~D6 depends on nginx's `error_page` for `/api/` only; a static HTML page is still what a
  browser gets for the UI's own `502`, which is intended.~~ *(Amended 2026-10-04:)* an HTML page,
  or whatever a controller writes, is what a client gets from `/api/` when the Ingress controller
  answers itself — no backend pod ready, a body above the controller's limit, an answer later than
  its timeout; D6 covers what cowork's own servers answer, and no more.

## References

- [`backend/internal/httpserver/server.go`](../../backend/internal/httpserver/server.go) — `writeProblem`, the three routes
- [ADR 0046](0046-spec-first-the-openapi-document-is-the-contract.md) D4, D6 — validation and `components/responses`
- [ADR 0023](0023-the-tenant-is-in-the-path.md) D5, [ADR 0039](0039-no-request-budgets-size-and-time-limits-instead-configurable-and-switchable.md) D3, [ADR 0045](0045-idempotency-put-where-it-is-free-a-required-key-on-agent-posts-stored-with-the-act.md) — codes this record gives a shape
