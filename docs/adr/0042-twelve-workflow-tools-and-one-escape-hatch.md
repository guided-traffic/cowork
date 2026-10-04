# ADR 0042: Twelve Workflow Tools and One Escape Hatch — the MCP Server Encodes the Method, Not the Resource Model

## Status

Accepted, amended 2026-10-01 by [ADR 0066](0066-repositories-are-bound-by-their-normalised-remote-identity-creation-proposed-by-the-agent-confirmed-by-the-person.md)
(D2: `record_answer` and `create_project` added; `session_start` binds by remote or
proposes). Decided by the owner as the answer to the catalog question "tool granularity?": a small set of workflow tools with a generic `api` escape hatch, over CRUD
tools generated from the resource model, over the escape hatch alone, and over both sets
side by side. The rules of D3–D6 were put to the owner with the question and not objected to.

**Built** (phase 5, 2026-10-04), the fifteen tools of D1 and D2 as made concrete and amended
there, in [`internal/tools`](../../backend/internal/tools/), served by `cowork-mcp`
([docs/developer/mcp.md](../developer/mcp.md)). D3 — every description names its limits, and
once the token is read which capabilities it holds and lacks. D4 — Markdown with the canonical
key; a refusal is the API's problem with its status and code, marked as the tool's error. D5 —
the time per installation and binding in the process and in a file under the user's cache
directory; the `SessionStart` hook writes it as well, a compaction does not. D6 in two tiers:
the unit tier asserts each tool's requests against a fake API, the integration tier runs every
tool through the MCP server against the real API and asserts its answers and the acts it
records, and a unit test holds every operation a tool calls to the document. The tools take a
surface: `session_start` reads the working directory and is for a terminal; a host inside the
backend runs the others in process — ~~which credential such a host presents is not decided~~.

Amended 2026-10-04 (D1: `place_ticket` in place of `set_urgency`, and `file_ticket` into a
horizon at a place — the owner's answer recorded in [ADR 0010](0010-the-frontmatter-vocabularies-become-ticket-columns.md)
D3), and built the same day: `place_ticket` and the horizon of `file_ticket` in
[`tool_tickets.go`](../../backend/internal/tools/tool_tickets.go), in `cowork-mcp` and in the chat.
Amended 2026-10-04 (D1: `set_urgency`, and the preconditions `from` and `version` a caller may pin),
with the chat in the UI provisionally
([ADR 0076](0076-the-chat-in-the-ui-runs-its-loop-in-the-backend-as-an-agent-of-the-person.md)), and
built the same day: sixteen tools. The host inside the backend is the chat: it presents the
person's session marked as its agent, `chat/<model>/<conversation>`, offers the tools that take
everything as arguments but `api`, with three of its own that open a page, ~~and holds each to a
policy of its own (ADR 0076 D2)~~ *(amended again 2026-10-04 by the owner's answers recorded in ADR
0076: and runs every call at once, its agent holding the capabilities the person chose)*. A tool call of `cowork-mcp` in a working directory without a
binding resolves the binding once before it runs (`Session.bindOnce` in
[`tools/session.go`](../../backend/internal/tools/session.go)), so a short key works without
`session_start` when the hook said the session is bound.

## Context

The MCP server is a thin client of the API that can do nothing the API cannot
([ADR 0040](0040-rest-is-the-contract-mcp-is-the-ergonomic-surface-and-can-do-nothing-the-api-cannot.md)
D3) and speaks `stdio` from the repository root ([ADR 0041](0041-the-mcp-server-speaks-stdio-and-ships-as-a-release-binary-per-platform.md)
D4). What it offers decides what a session spends its context on: a tool per resource puts
the method — analysis, then a question, then work, then verification — into prompts, where
it erodes; a tool per step of the working day puts it into parameters that cannot be
skipped. Every tool description rides in the context window on every turn, so fewer, richer
tools cost less than many thin ones. A session pulls at start ([ADR 0020](0020-notifications-are-an-in-app-inbox-per-person.md)
D5), a person answers questions ([ADR 0011](0011-a-ticket-is-a-markdown-body-plus-first-class-open-questions.md)
D2), and the agent limits hold server-side whatever tool is called (the next record).

## Decision

**D1 — The tool set of the first release:**

| Tool | Does | API behind it |
|---|---|---|
| `session_start()` | reads the binding, returns the active ticket of the bound project (assigned to the person and `in-progress`) or the top candidates by score, the person's unread inbox grouped by ticket, and the activity of the bound tickets since the last session, as one Markdown block; reports binding drift and missing configuration as results | `/tenants/{slug}/projects/{KEY}`, `/me/next`, `/me/inbox`, the ticket activity routes |
| `get_ticket(key)` | the Markdown export plus links, the prerequisite tree and the last comments | the ticket, `…/markdown`, `…/prerequisites`, `…/comments` |
| ~~`search(query, filters?)`~~ `search(query?, filters?)` *(amended 2026-10-04)* | full text within the bound project, the tenant, or the person's tenants; without a query, a project's tickets in rank order | `/tenants/{slug}/search`, `/me/search` |
| `file_ticket(type, title, body, severity, security, threat?, effort, parent?, links?)` | creates a ticket and returns its canonical key | `POST …/tickets` (+ links) |
| `record_state(key, body)` | replaces the body (the current state) | `PUT …/tickets/{n}/body` |
| `open_question(key, question, options, recommendation, asked_of?)` | creates a question entity | `POST …/questions` |
| `comment(key, text, explains_act?)` | writes a comment, optionally as the explanation of the agent's own act in the same request | `POST …/comments` |
| `transition(key, to, reason_or_note)` | performs a state transition; a refusal (the agent limits, an open prerequisite) comes back with the server's reason | `POST …/transitions` |
| `set_progress(key, percent)` | sets the five-step progress | `PATCH …/tickets/{n}` |
| `link(key, type, other_key)` | creates a typed link | `POST …/links` |
| `watch(key)` | registers `watch` interest | `PUT …/interest` |
| `finish_work(key, verification_note)` | writes the verification note as a comment, sets progress, and performs the furthest transition the agent may; reports what remains for a person | the comment, the ticket, the transition routes |
| `api(method, path, body?)` | any API call with the same token and the same limits; the escape hatch | everything |

Thirteen names; `api` is the hatch, the twelve are the method.

*(Made concrete 2026-10-04: the column "API behind it" named routes before the API had them;
each tool calls the routes that exist — `get_ticket` and the active ticket of `session_start`
`…/context`, `search` the ticket lists with `q`, `link` `PUT …/links/{type}/{other}` —, listed
per tool in its `Operations`. Optional arguments beside the table's: `get_ticket` `comments`
and `activity`; `search` `scope`, `project`, `state`, `type`, `assigned_to_me`,
`include_terminal` — and `query` itself is optional in one project, where a search without words lists
the project's tickets in rank order, and asks for words outside one *(amended 2026-10-04: on a
live run of the chat a model asked "which tickets are in WEB?" sent an empty query, which the
required query refused, and the question had no call)*; `file_ticket` `project`; `record_state` `comment`; `transition`
`block_kind`, `blocked_by` and `comment`; `api` `if_match`. `session_start` shows the
candidates in rank order while the score of [ADR 0014](0014-rank-is-the-decision-score-is-the-warning.md)
is not built, and no inbox while [ADR 0020](0020-notifications-are-an-in-app-inbox-per-person.md)
is not.)* *(Amended 2026-10-04: ~~`comment(key, text, explains_act?)`~~ `comment(key, text)` —
the explanation of an own act is the `comment` argument of `record_state`, `transition` and
`set_progress`, written in the act's own request as
[ADR 0015](0015-comments-are-a-thread-and-activity-is-a-separate-list.md) D2 requires;
~~`set_progress(key, percent)`~~ `set_progress(key, percent, stage?, note?, reason?, comment?)`
— one of the three stages of [ADR 0017](0017-effort-is-a-size-progress-is-a-five-step-percentage-and-time-is-booked-by-people.md)
D2 as amended, the implementation stage by default, with the note of the write that closes the
ticket and the reason of the one that reopens it.)* *(Amended 2026-10-04: a sixteenth name,
`set_urgency(key, urgency?, reason?, withdraw?)` — the urgency override of
[ADR 0010](0010-the-frontmatter-vocabularies-become-ticket-columns.md) D3 to `now`, `release`,
`next`, `later` or `icebox` with a reason, or its withdrawal, through `PUT` and `DELETE
…/urgency-override` with the version read; it needs the `override-urgency` capability and gives
"rank it to now" a tool. And three preconditions a caller may pin: `transition(…, from?)` and
`finish_work(…, from?)` act only while the ticket is in the state named, `set_progress(…, version?)`
only while the ticket has the version named ~~— the chat pins what it read before the person decides
(ADR 0076 D2)~~ *(the chat no longer pins: nothing of it waits for a decision, 2026-10-04; the
preconditions stay any caller's)*; left out, the state or version read at the call.)* *(Amended 2026-10-04, with the horizon of
ADR 0010 D3 as amended that day:)* ~~`set_urgency(key, urgency?, reason?, withdraw?)`~~
`place_ticket(key, horizon?, after?, before?, reason?)` — the horizon of a ticket, its place
directly after or before another ticket of that horizon, or both in one call: the horizon through
the override routes (`later`, the derived value of every ticket, withdraws), the place through
`PUT …/tickets/{number}/rank`; a horizon needs `override-urgency` and a reason, a place `rank`.
`file_ticket(…, horizon?, after?, before?)` files into a horizon at a place in one request,
`later` at the end without them. Both descriptions say what a horizon means and that it is not a
state: asked to file a ticket into `next`, an agent of the earlier catalogue sent the person to
the state `analysed`.

**D2 — ~~`answer_question` is not a tool and never will be;~~** *(amended 2026-10-01:
`record_answer(question, answer)` exists for tokens with the `record-answer` capability and
writes down the answer the person gave in chat, marked as recorded by the agent — the
decision stays the person's, [ADR 0066](0066-repositories-are-bound-by-their-normalised-remote-identity-creation-proposed-by-the-agent-confirmed-by-the-person.md)
D8; `create_project(tenant, key, name, remote)` exists for the `create-project` capability,
ADR 0066 D5)*. *(Made concrete 2026-10-04: `record_answer` takes the ticket's `key` beside the
question's number; `create_project` an optional `path`, the sub-directory of a monorepo.)* Nothing that deletes, overrides a prerequisite refusal or administers members,
tokens or tenants is a tool.

**D3 — Every tool description names the agent limits** it can run into, so the model knows
before calling that `done` and `decided` are a person's and that a refusal is not an error of
the tool.

**D4 — Every tool returns Markdown with the canonical key** ([ADR 0007](0007-a-ticket-key-is-globally-unique-tenant-slash-project-dash-number.md)
D2) of what it touched; `api` alone returns the API's JSON unchanged. A tool's failure is the
API's error with its code and message ([ADR 0040](0040-rest-is-the-contract-mcp-is-the-ergonomic-surface-and-can-do-nothing-the-api-cannot.md)
D3).

**D5 — `session_start` is the only tool with memory:** it keeps the time of the previous
call per binding in the process (and, between processes, in a file under the user's cache
directory named by installation and binding — the only file the server writes), so "since
the last session" has a meaning.

**D6 — Each tool is a tested procedure.** The integration tier runs the MCP server against
the API with the fixture identities ([ADR 0038](0038-no-development-login-switch-the-development-environment-is-the-real-login-path.md)
D3) and asserts, per tool, the calls it makes and the Markdown it returns; a test asserts
that every route a tool uses exists in the OpenAPI document.

## Consequences

- A session's context carries thirteen descriptions, not twenty-five; the working day is
  thirteen verbs.
- The method is enforced by parameters: `finish_work` without a verification note is a
  schema error, not a forgotten rule.
- A new API feature is reachable at once through `api` and becomes a tool when a session
  needs it often; the tool set is amended, not regenerated.
- `finish_work`'s "the furthest transition the agent may" depends on the agent limits
  record; this record fixes the shape, that one the destination.

## Alternatives Considered

- **CRUD tools, one per resource, generated from OpenAPI.** Complete and mechanical; five
  calls to start a session, twenty-five descriptions in every turn, and no method anywhere.
  Lost.
- **`api` and `openapi` only.** Minimal and never stale; pure prompt choreography, which is
  what the MCP server exists to remove. Lost.
- **Workflow tools plus a generated CRUD layer behind a flag.** Completeness on demand; two
  sets to document and test, and `api` already covers the rest. Lost.

## Residual risks

- Tool descriptions are prose the model reads; a vague description produces vague calls.
  They are reviewed like API documentation and tested by D6 for behaviour, not wording.
- D5's cache file is state outside the API; it holds timestamps only, and a missing file
  means "everything since the ticket was opened".

## References

- [ADR 0040](0040-rest-is-the-contract-mcp-is-the-ergonomic-surface-and-can-do-nothing-the-api-cannot.md), [ADR 0041](0041-the-mcp-server-speaks-stdio-and-ships-as-a-release-binary-per-platform.md) — the server these tools live in
- [ADR 0011](0011-a-ticket-is-a-markdown-body-plus-first-class-open-questions.md) D1, D2, D4 — body replacement, questions, the Markdown export
- [ADR 0012](0012-four-typed-directed-links-within-a-tenant.md) D6, [ADR 0015](0015-comments-are-a-thread-and-activity-is-a-separate-list.md) D2 — the prerequisite tree, the explaining comment
- [ADR 0020](0020-notifications-are-an-in-app-inbox-per-person.md) D5 — the pull at session start
- [docs/operations/claude-code.md](../operations/claude-code.md) — the daily loop these tools serve (the workflow plan once linked here is consumed, [ADR 0074](0074-the-question-catalog-is-consumed-phases-become-tickets-when-they-start-in-their-own-session.md) D3)
- [ADR 0076](0076-the-chat-in-the-ui-runs-its-loop-in-the-backend-as-an-agent-of-the-person.md) — the chat in the UI, which runs the catalogue in the backend
