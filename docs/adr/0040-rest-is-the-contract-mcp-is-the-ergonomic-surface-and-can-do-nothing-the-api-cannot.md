# ADR 0040: REST Is the Contract, MCP Is the Ergonomic Surface — and the MCP Server Can Do Nothing the API Cannot

## Status

Accepted. Date: 2026-10-01. Decided by the owner as the answer to the catalog question "REST
only, MCP only, or both?": both, with REST as the single interface to the data and an MCP
server as a thin client of it, over REST alone, over MCP alone, and over an additional
GraphQL surface. The rules of D4–D7 were put to the owner with the question and not objected
to.

**Built** (phase 2, 2026-10-02; D2–D5 since 2026-10-04): D1, D6 and D7 — the REST API with its
document, served at `/api/v1/openapi.json`. D2–D5 — `backend/cmd/cowork-mcp` over
`internal/mcpcli`, `internal/mcpserver` and `internal/tools`, on the generated client of
`internal/api/apigen`; a unit test holds that the binary depends on no store, no database
driver, no S3 client and no API handler. D3 as built: a tool's failure is the API's problem
with its status and code; a failed connection is retried twice for a `GET`, `PUT`, `DELETE`
or keyed `POST`, which cannot act twice, and an answer of the API is never retried. D4 reads
`CLAUDE_PROJECT_DIR` beside the two variables, for the working directory.

Amended 2026-10-04 for the chat in the UI
([ADR 0076](0076-the-chat-in-the-ui-runs-its-loop-in-the-backend-as-an-agent-of-the-person.md) D1),
provisionally with that record (D1, D3: the catalogue's second host is inside the backend and goes
through the API all the same), and built the same day.

## Context

An LLM is a first-class user of cowork ([ADR 0004](0004-cowork-is-a-team-product.md) D1); its
token, its marking and its limits are decided ([ADR 0035](0035-personal-access-tokens.md),
[ADR 0036](0036-a-token-acts-as-its-person-an-agent-flag-is-the-floor-the-agent-header-only-narrows.md));
it pulls, nothing pushes ([ADR 0020](0020-notifications-are-an-in-app-inbox-per-person.md)
D5). What remained was the shape of the surface it talks to. A REST API alone puts the
multi-step choreography of a session — read the binding, fetch the ticket, fetch the inbox —
into prompts, where it fails quietly. An MCP server that talks to the database itself would
be a second implementation of tenancy, roles and agent limits beside the API. Claude Code has
a native place for an MCP server and none for "a REST server".

## Decision

**D1 — The REST API with its OpenAPI document is the one interface to the data.** Every
client — the Angular UI, the MCP server, the importer, a script — goes through it. There is
no second path to the database. *(Amended 2026-10-04: the chat in the UI is a client too, though it
runs inside the backend: its tool calls are requests of this API, sent to the server's own handler
in the same process, through the whole pipeline — authentication, the CSRF check, the tenant
boundary, validation, the agent rules, the audit, the events — and never past it to the store.)*

**D2 — The MCP server is a thin client of the API.** It lives in this repository as
`backend/cmd/cowork-mcp`, shares the module and the generated API client with the backend,
and is versioned with it. It holds a personal access token, sends `X-Cowork-Agent` on every
request (ADR 0036 D3), and exposes the workflow tools the next records define.

**D3 — The MCP server can do nothing the API cannot, and nothing the token cannot.** No
tool exists without an API route behind it; every authorization decision is the API's; a
tool's failure is the API's error, surfaced with its code, never swallowed or retried into
success. *(Amended 2026-10-04: the same holds for the catalogue's second host, the chat in the UI,
with the person's session marked as the chat's agent in place of the token — it can do nothing the
API and that agent cannot; a tool's failure is an answer its model reads.)*

**D4 — Configuration of the MCP server is environment only:** `COWORK_URL` and
`COWORK_TOKEN`, passed through by the Claude Code MCP configuration; no configuration file,
no stored credential. It keeps a session id and an in-memory cache and writes nothing to
disk *(amended 2026-10-04: but the file of session-start times of
[ADR 0042](0042-twelve-workflow-tools-and-one-escape-hatch.md) D5, which that record names
the only file the server writes; the two records disagreed, and the later, more specific one
holds)*.

**D5 — The MCP server checks compatibility at start** against `GET /api/v1/version` and the
OpenAPI document's version, and refuses to serve tools against an API it does not know.
*(Made concrete 2026-10-04: an API it does not know is another major version than the
binary's — a binary without a semantic version, a development build, compares none — or a
served document that lacks an operation one of its tools calls. The refusal is the answer
to every tool call; an installation that cannot be reached is asked again at the next call.)*

**D6 — The API serves its own OpenAPI document** at `GET /api/v1/openapi.json`, so a session
without the MCP server — a script, a one-off `curl` — can load the contract from the
installation it talks to.

**D7 — No GraphQL, no second query language.** The views of [ADR 0018](0018-the-views-of-the-first-release.md)
are fixed; the list filters of the API record serve them.

## Consequences

- One authorization, one audit, two surfaces. A security review reads the API; the MCP server
  is a client.
- A tool call is a tested multi-step procedure in Go, not prompt choreography; the session
  start of the workflow plan becomes one tool.
- The MCP server is a local binary in the first release (its transport is the next record);
  it is built and tested with the backend and released with it, but it is not a container.
- The generated API client (from the OpenAPI document) is shared by the MCP server and the
  tests; the frontend has its own generated client in TypeScript.
- D3 makes "the agent can only…" a property of the API, not of the MCP server: a person who
  bypasses the MCP server with `curl` and the same token can do exactly the same things.

## Alternatives Considered

- **REST and OpenAPI alone.** One surface; the choreography moves into prompts, and Claude
  Code has no native slot for it. Lost.
- **MCP alone, talking to the database.** Short paths; a second implementation of tenancy,
  roles and agent limits, and no interface for the UI, the importer or scripts. Lost.
- **REST, MCP and GraphQL for the UI.** Flexible queries for fixed views, with field-level
  authorization traps. Lost.

## Residual risks

- Tool definitions and API routes can drift; D5's compatibility check and a test that every
  tool's routes exist in the OpenAPI document are the guards.
- D4 passes the token through the environment of the MCP process; the Claude Code
  configuration file that sets it is the place a token leaks from, and the token's expiry and
  scope ([ADR 0035](0035-personal-access-tokens.md) D3, D4) bound that.

## References

- [ADR 0035](0035-personal-access-tokens.md), [ADR 0036](0036-a-token-acts-as-its-person-an-agent-flag-is-the-floor-the-agent-header-only-narrows.md) — the token and the marking the MCP server carries
- [ADR 0023](0023-the-tenant-is-in-the-path.md) — the path families the client is generated from
- [ADR 0020](0020-notifications-are-an-in-app-inbox-per-person.md) D5 — the pull the session-start tool performs
- [docs/operations/claude-code.md](../operations/claude-code.md) — where the MCP server sits in the daily loop (the workflow plan once linked here is consumed, [ADR 0074](0074-the-question-catalog-is-consumed-phases-become-tickets-when-they-start-in-their-own-session.md) D3)
- [ADR 0076](0076-the-chat-in-the-ui-runs-its-loop-in-the-backend-as-an-agent-of-the-person.md) — the chat in the UI, the catalogue's second host
