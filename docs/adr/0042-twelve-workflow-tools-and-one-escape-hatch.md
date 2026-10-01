# ADR 0042: Twelve Workflow Tools and One Escape Hatch — the MCP Server Encodes the Method, Not the Resource Model

## Status

Accepted, amended 2026-10-01 by [ADR 0066](0066-repositories-are-bound-by-their-normalised-remote-identity-creation-proposed-by-the-agent-confirmed-by-the-person.md)
(D2: `record_answer` and `create_project` added; `session_start` binds by remote or
proposes). Decided by the owner as the answer to the catalog question "tool granularity?": a small set of workflow tools with a generic `api` escape hatch, over CRUD
tools generated from the resource model, over the escape hatch alone, and over both sets
side by side. The rules of D3–D6 were put to the owner with the question and not objected to.

**Not built.** No `cowork-mcp`.

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
| `search(query, filters?)` | full text within the bound project, the tenant, or the person's tenants | `/tenants/{slug}/search`, `/me/search` |
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

**D2 — ~~`answer_question` is not a tool and never will be;~~** *(amended 2026-10-01:
`record_answer(question, answer)` exists for tokens with the `record-answer` capability and
writes down the answer the person gave in chat, marked as recorded by the agent — the
decision stays the person's, [ADR 0066](0066-repositories-are-bound-by-their-normalised-remote-identity-creation-proposed-by-the-agent-confirmed-by-the-person.md)
D8; `create_project(tenant, key, name, remote)` exists for the `create-project` capability,
ADR 0066 D5)*. Nothing that deletes, overrides a prerequisite refusal or administers members,
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
- [docs/planning/vscode-workflow.md](../planning/vscode-workflow.md) — the daily loop these tools serve
