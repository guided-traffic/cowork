# ADR 0076: The Chat in the UI Runs Its Loop in the Backend as an Agent of the Person — the Shared Tool Catalogue In-Process, a Provider Only the Operator Names, a Tenant's Consent Before Its Data Leaves, and the Person's Decision on What a Person Owes a Reason For

## Status

**Accepted provisionally, 2026-10-04: built by the implementer on the owner's instruction to build
open questions to best knowledge and leave a gate open when in doubt; the owner's review is
pending.** Date: 2026-10-04. The four open questions of the chat — where the loop runs, who acts,
what may leave the installation, which writes the person confirms — had no answer when phase 5 was
built. The owner's instruction of 2026-10-03 was to build to best knowledge, to leave a gate open
where in doubt, and to collect what the owner should look at. This record is what was built; the
owner's answer to each question amends it in place. Two gates are left open and named as gaps: the
chat holds every capability, because no per-person choice is built
([chat.md H-40](../security/chat.md#h-40)), and a confidential ticket is not withheld from a
provider outside the installation ([chat.md H-37](../security/chat.md#h-37)). The first answer
departs from the recommendation the question carried — the loop in the browser — for the reason
under *Alternatives Considered*.

**Built** (phase 5, 2026-10-04): D1 — `GET` and `POST /api/v1/tenants/{tenant}/chat`
([`backend/api/chat.yaml`](../../backend/api/chat.yaml)), the turn in
[`internal/api/chat.go`](../../backend/internal/api/chat.go), the loop in
[`internal/chat`](../../backend/internal/chat/chat.go), the tool calls through
[`chat.Loopback`](../../backend/internal/chat/loopback.go); D2 — the agent header on a session
([ADR 0036](0036-a-token-acts-as-its-person-an-agent-flag-is-the-floor-the-agent-header-only-narrows.md)
D3 as amended), the policy of every tool in [`chat/confirm.go`](../../backend/internal/chat/confirm.go);
D3 — `COWORK_CHAT_*` in [`config/chat.go`](../../backend/internal/config/chat.go), the tenant's
consent (migration [24](../../backend/internal/store/migrations/000024_tenant_chat_consent.up.sql));
D4 — [`internal/llm`](../../backend/internal/llm/llm.go); D5 — the panel
([`layout/chat-panel.ts`](../../frontend/src/app/layout/chat-panel.ts),
[`core/chat.service.ts`](../../frontend/src/app/core/chat.service.ts),
[`core/chat-stream.ts`](../../frontend/src/app/core/chat-stream.ts)) and the consent switch in the
tenant's settings; D6 — [`frontend/nginx/default.conf.template`](../../frontend/nginx/default.conf.template),
`"inlineCritical": false` in [`angular.json`](../../frontend/angular.json); D7 — the limits; the
chart's `chat.*` values. The unit tests run the loop, the gateway and the configuration against
the stub provider of [`test/stubllm`](../../backend/test/stubllm/stubllm.go); the integration tier
runs turns through the whole server against it
([`api_chat_test.go`](../../backend/test/integration/api_chat_test.go)); the panel, the stream reader
and the service have their vitest specs.

**Verified on 2026-10-04:** against LM Studio serving `qwen/qwen3-30b-a3b-2507`, loaded with a context
of 32k tokens, the chat filed a ticket, set its urgency to `now` and moved it to `analysed`; the open
board showed the card at once, and every act was recorded as
`chat/qwen:qwen3-30b-a3b-2507/<conversation>`. LM Studio loads a model on first use with a context of
4096 tokens, which the instructions and the tools exceed
([docs/operations/chat.md](../operations/chat.md#lm-studio-on-the-operators-machine)). The production
bundle behind nginx with the policy of D6 was run in Chromium and WebKit, in both colour schemes,
with the API mocked: no violation of the policy, model text holding markup shown as text, a turn
streamed through nginx and stopped. **Not verified:** a provider other than LM Studio and the
stub — no call reached OpenAI's or Anthropic's API, Ollama or vLLM; a conversation in the
end-to-end tier, which does not exist
([ADR 0056](0056-end-to-end-playwright-against-the-built-containers-with-two-identities.md)).

## Context

The owner wants a chat panel at the right edge of every page through which an agent operates
cowork for the person — connected first to a local LM Studio, later to providers such as Anthropic
and OpenAI — and decided before the questions were put that every call to a model goes through the
backend: the browser never talks to a provider. The MCP server of
[ADR 0040](0040-rest-is-the-contract-mcp-is-the-ergonomic-surface-and-can-do-nothing-the-api-cannot.md)
had just been built, with its tools in a catalogue that knows no transport
([ADR 0042](0042-twelve-workflow-tools-and-one-escape-hatch.md)); the plan named one catalogue for
both.

What the design worked with, as researched on 2026-10-03 and not checked again here: LM Studio serves
OpenAI-compatible routes on `http://localhost:1234` — `/v1/chat/completions` with tool calling and
streaming among them — and an Anthropic-compatible `/v1/messages`; OpenAI Chat Completions with
tool calling is the format OpenAI, LM Studio, Ollama, vLLM, llama.cpp and OpenRouter speak, and
Anthropic speaks its Messages API, its OpenAI-compatible endpoint being documented for testing and
comparison; a page could not reach a model on the person's machine anyway — Safari refuses an
`http://localhost` request from an HTTPS page as mixed content, and Chromium asks for the Local
Network Access permission since version 142; and WebMCP, which lets a page register tools for an
agent built into the browser, runs as an origin trial in Chrome and nowhere else.

A chat changes what cowork is in three ways the earlier records did not have to weigh. It runs
what a model decides, and the model reads ticket bodies and comments that other people and agents
wrote — text that can steer it. It sends what it reads to whoever serves the model, which for a
hosted model is outside the installation and outside the client tenants' reach. And it renders a
model's output in the page, which until then sent no `Content-Security-Policy`
([trust-boundaries.md](../security/trust-boundaries.md#the-transport-in-front-of-the-pods)). cowork
still hosts no model: it calls one the operator names.

## Decision

**D1 — The loop runs in the backend, and its tools are the shared catalogue, run in-process through
the server's own handler.** `POST /api/v1/tenants/{tenant}/chat` runs one turn: the backend calls
the configured model with the conversation the request carries, runs the tools the model calls,
calls the model again with their answers, and streams the turn to the browser as server-sent events
— `text`, `tool_call`, `ui`, `tool_result`, `confirm`, `error`, and `done` last, which carries the
messages the turn added. The backend keeps nothing between turns: the browser holds the
conversation and sends it with each turn. The tools are the catalogue of
[`internal/tools`](../../backend/internal/tools/tools.go) that take everything as arguments — not
`session_start`, which reads a working directory, and not `api`: raw access to the API stays the MCP
server's, and a model that reads injected text gets none — and three of the chat's own,
`open_ticket`, `open_backlog` and `open_board`, which show the person a page once the API has said
the person may see it. A tool the chat does not classify (D2) is not offered. Every tool call is a
request of the API sent to the server's root handler in the same process
([`chat.Loopback`](../../backend/internal/chat/loopback.go) over `tools.HandlerDoer`): it passes the
request id, the request log, authentication, the CSRF check, the tenant boundary, validation,
authorization with the agent rules, the audit row and the publication to the event streams like
any request, under a request id of its own and with the person's client address. The loopback
sends nothing outside the turn's tenant, nothing to the chat itself or to the event stream, and no
path that is not clean.

**D2 — The chat acts as an agent of the person, and the person decides the acts a person owes a
reason, a note or a decision for.** Every tool call carries the person's session cookie and
`X-Cowork-Agent: chat/<model>/<conversation>` — the model's slashes written as colons, each part cut
to 64 characters — so the request is an agent's
([ADR 0036](0036-a-token-acts-as-its-person-an-agent-flag-is-the-floor-the-agent-header-only-narrows.md)
D3 as amended): every capability of
[ADR 0043](0043-agent-capabilities-are-chosen-per-token-the-default-is-everything-reversible-and-attributable.md)
D4, the hard-off list and every agent rule, and nothing only a session does. Its acts record the
person as the actor, the mark, the capability set and no token. A per-person choice of the chat's
capabilities is not built: the chat holds them all. Every tool has a policy
([`chat/confirm.go`](../../backend/internal/chat/confirm.go) `policies`):

| Policy | Tools | The call |
|---|---|---|
| read | `get_ticket`, `search`, `open_ticket`, `open_backlog`, `open_board` | runs at once |
| write | `file_ticket`, `record_state`, `open_question`, `comment`, `link`, `watch`, `set_urgency` | runs at once — and waits for the person once the conversation has read a confidential ticket |
| decision | `finish_work`, `record_answer`, `create_project` | always waits for the person |
| move | `transition` | waits for a move to `done`, `dropped`, `blocked` or `decided`, for a backward move, a reopen and the withdrawal of a done by hand ([ADR 0009](0009-ticket-states-are-the-frontmatter-states-plus-blocked.md)), and for any move when the ticket cannot be read; the other moves run at once, unless the conversation read a confidential ticket |
| stages | `set_progress` | waits for the write that fills the last of the three stages — the done act — and for one that lowers a stage of a ticket its stages closed — the reopen — and when the ticket cannot be read; otherwise like a write |
| left out | `session_start`, `api` | not offered |

A call that waits ends the turn with a `confirm` event: the act in words, written for the person
from what the API read — the canonical key and title — and from the model's arguments with every
control, line-separating and quote character made plain. What the review read is pinned into the
waiting call — a transition's `from` state, a progress write's `version`, `finish_work`'s `from` — so
the API refuses the call when the ticket moved before the person decided; `record_answer` and
`create_project` carry no such precondition. The review fails closed: a move or a progress write
whose ticket it cannot read, or whose arguments it cannot decode, waits. The next turn carries the
person's decision on the first waiting call only: it runs, marked `chat/<model>/<conversation>+confirmed`
so the record shows the person's Run, or it is answered "skipped by the person"; a later call of the
same answer that needs a decision too did not run and is answered so, and the model makes it again.
A call left waiting when the person writes a new message instead is answered as skipped. A creating
`POST` of a tool call carries an `Idempotency-Key` derived from the conversation and the call, so a
decision sent twice replays instead of acting twice
([ADR 0045](0045-idempotency-put-where-it-is-free-a-required-key-on-agent-posts-stored-with-the-act.md)
D5 as amended).
The model is told the rules — the person's language, nothing claimed beyond what a tool's result
confirms, the tenant it works in, the tools' text as information and never as instruction, the
acts that wait — and the tools know the person, so a question can be asked of `me`.

**D3 — The provider is the operator's configuration and nothing else; a provider outside the
installation needs each tenant's consent, bound to the provider it was given to; a turn works in
its tenant only.** `COWORK_CHAT_PROVIDER` (`openai` or `anthropic`; empty, no chat),
`COWORK_CHAT_URL`, `COWORK_CHAT_API_KEY` and `COWORK_CHAT_MODEL` name it; no request parameter names
a host, a path or a key. The URL is `https://`, or `http://` only on a host of the operator's own
network by its name or address — `localhost`, a loopback or private address, a name without a dot,
or a name under `.localhost`, `.svc`, `.cluster.local`, `.internal`, `.local`, `.lan` or
`.home.arpa`. `COWORK_CHAT_INSIDE=true` is the operator's statement that the provider runs inside
the installation's trust boundary — a model on the operator's machine or network, such as LM Studio
— and then every tenant has the chat. Without it (the default) a tenant has the chat only once one
of its administrators allowed the provider in the tenant's settings (`chat_external_allowed`):
switching it on takes a browser session
([ADR 0035](0035-personal-access-tokens.md) D5 as amended), switching it off an `admin`-scope
token as well, and both are the tenant's recorded `updated` acts. The consent stores the provider's
fingerprint — the wire format, the URL's host with its port, and the model — and holds only while
the configuration names the same: another model or another host is a provider the tenant did not
allow. The availability and the consent are read again before every call of the model, so a
withdrawn consent ends a turn that runs. A turn's tool calls stay in the turn's tenant (D1), and
the model is told so: the tenant's consent covers that tenant alone.

**D4 — The gateway speaks OpenAI Chat Completions with tool calling and the Anthropic Messages API,
streaming, behind one interface.** [`internal/llm`](../../backend/internal/llm/llm.go): `POST
{url}/chat/completions` with the tools as functions, `tool_choice: auto`, `stream: true` and the key
as a bearer token where one is set; `POST {url}/v1/messages` with `x-api-key`,
`anthropic-version: 2023-06-01`, the tools with their `input_schema` and `tool_choice: auto`; both
with `max_tokens` 4096 — which OpenAI's reasoning models refuse, wanting `max_completion_tokens`,
a known limit of the OpenAI adapter. A neutral message model sits between them — the instructions, the person's
messages, the model's text and tool calls, the tools' answers — and a tool call's arguments arrive
in fragments and are put together. An answer that is not streamed is read too, and a reasoning
model's `<think>…</think>` is taken out of the text. The client follows no redirect, gives up on a
provider that has not begun to answer within two minutes or stays silent for ninety seconds, and
reads a bounded answer. What a provider answers in an error never reaches the person: the turn's
`error` event says which kind of failure it was, and the log carries at most 300 characters of the
provider's message, the configured key taken out.

**D5 — The panel renders text only, and navigates only inside the tenant.** Whatever the model or a
tool wrote is shown as text with its line breaks — never as HTML and never as rendered Markdown,
whose sanitiser does not exist yet
([ADR 0011](0011-a-ticket-is-a-markdown-body-plus-first-class-open-questions.md) D6). A tool call is
a card with the tool's name, its arguments folded away, the start of its answer, and Run and Skip
where it waits. A `ui` event opens a page only when it is a ticket, a backlog or a board of the
turn's tenant; anything else is shown on the call's card as not opened. The conversation lives in
the browser per tenant and is gone when another tenant's pages open; the panel exists only while
the tenant's chat is available, and whether it is open is the person's preference in browser
storage.

**D6 — The shell has a `Content-Security-Policy`.** nginx sends `default-src 'self'; script-src 'self';
style-src 'self' 'unsafe-inline'; img-src 'self' data: blob:; font-src 'self' data:; connect-src
'self'; frame-ancestors 'none'; base-uri 'self'; form-action 'self'; object-src 'none'` with
`index.html`, the hashed bundles and the icons. `'unsafe-inline'` is for styles only, because
PrimeNG and Angular write `<style>` elements at run time; scripts are the bundle's files alone, and
the production build therefore inlines no critical CSS, whose loader is an inline `onload` handler
([ADR 0052](0052-primeng-with-the-angular-cdk-a-themes-preset-and-dark-mode-from-the-start.md) D4 as
amended). The policy is the second line behind D5 should text ever reach the page as markup.

**D7 — A turn is bounded by limits of its own.** `COWORK_CHAT_TURN_TIMEOUT` (`5m`) bounds a turn — the
model's calls and the tools' — and `COWORK_CHAT_MAX_STEPS` (`8`) the calls of the model in it;
`COWORK_CHAT_TURNS_PER_PERSON` (`2`) the turns one person runs at once on one replica, beyond
which a turn is `429 chat_busy`; each is disabled by `0`
([ADR 0039](0039-no-request-budgets-size-and-time-limits-instead-configurable-and-switchable.md) D2 as
amended). The turn is exempt from `COWORK_REQUEST_TIMEOUT`, which bounds reading its body, and its
body is held to `COWORK_MAX_JSON_BODY`. Fixed: at most 400 messages of at most 100,000 characters a
text, 32 tool calls a message and 32 decisions; a tool's answer clipped to 16,000 characters for the
model and 2,000 for the person; and the gateway's bounds of D4. A comment keeps a silent turn open
every ten seconds.

## Consequences

- One catalogue, one implementation: a tool means the same to Claude Code and to the chat, and a
  change to a tool reaches both. The chat can do nothing the API cannot — and nothing an agent
  holding every capability in the person's session cannot.
- The person's acts and the chat's stay apart in the record: an act of the chat names the person,
  the model and the conversation, and an act the person ran from a proposal says `+confirmed`.
- What the browser shows moves with the agent: a write of a tool call reaches the open pages
  through the event stream like any write, and a page tool moves the person's own page.
- The backend holds a conversation for the length of a turn only; a turn is an HTTP request that
  can last minutes, held open by its comments, and an Ingress in front must not buffer it
  ([docs/operations/chat.md](../operations/chat.md)).
- A provider outside the installation reads what the chat reads in a tenant that allowed it —
  confidential tickets included, H-37 — and every tenant's when the operator declares a provider
  inside. [ADR 0020](0020-notifications-are-an-in-app-inbox-per-person.md) D5 — nothing leaves cowork
  on its own — holds: the chat sends a tenant's text to a provider in a person's turn, never by
  itself.
- The records this one changes are amended in place, each saying so: ADR 0035 D5 and ADR 0031 D6
  (thirteen session-only operations and the consent field), ADR 0036 D3 and D7 (the header on a
  session), ADR 0039 D2 (the turn's limits), ADR 0040, ADR 0042 and ADR 0043 (the second host of the
  catalogue, `set_urgency`, what the chat may do), ADR 0045 D5 (the keys of a tool call), ADR 0046
  (the turn outside the generated server), ADR 0047 (the turn's error event and its codes), ADR 0052
  (the policy, the budget) and ADR 0058 D3 (the key's Secret).

## Alternatives Considered

- **The loop in the browser** — the recommendation the question carried: the browser runs the loop
  with the frontend's own actions as tools, and the backend only forwards each model call. "The UI
  operated by an agent" would have been literal. It lost on the plan's own condition, one catalogue
  for the MCP server and the chat: the tools exist in Go, and the browser would have needed a second
  implementation of every tool in TypeScript, with its descriptions and limits kept in step. As built
  the backend holds no conversation between turns either, and the page still moves with the agent
  through `ui` events and the event stream.
- **The person acting directly**, the tool calls carrying the session alone. Simple; an injected
  instruction would then act with the person's whole power, the acts every agent is refused — members,
  tokens, deletion, booking time — included, and the record could not tell the person's acts from
  the model's. Lost to D2.
- **Consent per installation only**, the operator's configuration deciding for every tenant. One
  switch; a client's data would leave for a hosted model without that client's administrators
  knowing, which crosses the line between tenants
  ([ADR 0005](0005-a-tenant-is-a-client-organisation-and-the-isolation-unit.md)). Lost to D3's
  per-tenant consent; `COWORK_CHAT_INSIDE` remains the operator's statement for a provider of their
  own.
- **No confirmations**, every act the agent's and the record the answer. Quick; the acts a person
  owes a reason, a verification note or a decision for would be the model's word. Lost to D2's
  policies. **Every write confirmed** lost the other way: filing, ranking and editing would wait for
  a click each.
- **Confidential tickets withheld from a provider outside**, as the option of per-tenant consent
  first described it. Not built tonight: the gate is left open and named
  ([chat.md H-37](../security/chat.md#h-37)); what is built holds the conversation's later writes
  once it has read one.
- **An agent built into the browser, through WebMCP.** No backend loop at all; an origin trial in one
  browser. Lost.
- **Angular's `security.autoCsp`** for D6, which hashes the inline loader into a policy the page
  carries. Not taken: the policy stays one header in nginx, and the loader goes.

## Residual risks

What each mechanism leaves open is [docs/security/chat.md](../security/chat.md#what-this-does-not-cover):
confidential tickets at a provider (H-37); the reversible acts the model can be steered into without
the person's decision (H-38); a mark the client could write itself (H-39); every capability, for
everyone (H-40); plain http allowed by a host's name (H-41); a log clip that knows only the
configured key (H-42); no budget for a paid provider, and a turn limit per replica (H-43); a
model's text that contradicts the tool results (H-44); a hold on writes that lives in the
conversation the browser keeps and misses confidential text that arrives without its ticket (H-45);
two proposals that carry no precondition (H-46); and the provider's host in the consent's audit row
(H-47). The decision itself is provisional: an answer of the owner can move any of D1–D7.

## References

- [ADR 0036](0036-a-token-acts-as-its-person-an-agent-flag-is-the-floor-the-agent-header-only-narrows.md) D3, D7 — the header on a session, amended for D2
- [ADR 0043](0043-agent-capabilities-are-chosen-per-token-the-default-is-everything-reversible-and-attributable.md) — the capabilities and the hard-off list the chat holds
- [ADR 0040](0040-rest-is-the-contract-mcp-is-the-ergonomic-surface-and-can-do-nothing-the-api-cannot.md), [ADR 0042](0042-twelve-workflow-tools-and-one-escape-hatch.md) — the catalogue's first host and its tools
- [ADR 0035](0035-personal-access-tokens.md) D5, [ADR 0031](0031-server-side-sessions-in-an-httponly-cookie.md) D6 — what only a session does
- [ADR 0039](0039-no-request-budgets-size-and-time-limits-instead-configurable-and-switchable.md) — the limits D7 adds to
- [ADR 0045](0045-idempotency-put-where-it-is-free-a-required-key-on-agent-posts-stored-with-the-act.md), [ADR 0050](0050-optimistic-concurrency-a-version-per-entity-if-match-where-a-write-overwrites.md) — the keys and the preconditions a decided call carries
- [ADR 0065](0065-a-confidential-flag-replaces-the-file-name-embargo-set-automatically-lifted-only-by-a-person.md) — the confidential flag the hold reads
- [ADR 0052](0052-primeng-with-the-angular-cdk-a-themes-preset-and-dark-mode-from-the-start.md) D4, [ADR 0011](0011-a-ticket-is-a-markdown-body-plus-first-class-open-questions.md) D6 — the policy and the sanitiser
- [docs/developer/chat.md](../developer/chat.md), [docs/operations/chat.md](../operations/chat.md), [docs/security/chat.md](../security/chat.md) — how it works, how it is run, what it leaves open
