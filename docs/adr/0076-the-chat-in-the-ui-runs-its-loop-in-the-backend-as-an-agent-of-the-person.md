# ADR 0076: The Chat in the UI Runs Its Loop in the Backend as an Agent of the Person — the Shared Tool Catalogue In-Process, Providers Only the Operator Names, the Capabilities the Person Chooses, Nothing That Waits, and a Stop That Ends a Turn at Once

## Status

**Accepted, 2026-10-04, by the owner's answers to the chat's five open questions, amended in place
and built the same day.** Date: 2026-10-04. ~~Accepted provisionally, 2026-10-04: built by the
implementer on the owner's instruction to build open questions to best knowledge and leave a gate
open when in doubt; the owner's review is pending.~~ The four open questions of the chat — where the
loop runs, who acts, what may leave the installation, which writes the person confirms — had no
answer when phase 5 was built, and a fifth, whether every member may chat, came with them. The
owner's instruction of 2026-10-03 was to build to best knowledge, to leave a gate open where in
doubt, and to collect what the owner should look at; this record was what was built, and the owner
answered on 2026-10-04:

- **Where the loop runs:** in the backend, as built (D1).
- **Who acts:** an agent of the person, with the capabilities the person chooses from the nine of
  [ADR 0043](0043-agent-capabilities-are-chosen-per-token-the-default-is-everything-reversible-and-attributable.md)
  D4 — `decide`, `close` and `drop` off by default (D2). `record-answer` is off by default as well:
  with nothing waiting for the person, a text the model read could otherwise record an answer in the
  person's name.
- **What may leave the installation:** everything the person can read, confidential tickets
  included — the risk accepted by the owner. The providers are configured in the Helm chart and
  nowhere in the app; there is no tenant consent and no inside or outside; the chart holds a list of
  providers, and the person picks one in the panel (D3).
- **Which writes wait for the person:** none. Every tool call runs at once; the capabilities and
  the API's rules are the limit (D2).
- **Who may chat:** every member, viewers included, as built.

And, the same day: "a running agent must be stoppable at once" — a local model that makes mistakes
and does not stop by itself (D8). The record's title was the provisional decision's and is amended
with it. ~~Two gates are left open and named as gaps: the chat holds every capability, because no
per-person choice is built (chat.md H-40, closed), and a confidential ticket is
not withheld from a provider outside the installation ([chat.md H-37](../security/chat.md#h-37)).~~
The first answer departs from the recommendation the question carried — the loop in the browser —
for the reason under *Alternatives Considered*.

**Built** (phase 5, 2026-10-04, and the owner's answers the same day): D1 — `GET` and `POST
/api/v1/tenants/{tenant}/chat` ([`backend/api/chat.yaml`](../../backend/api/chat.yaml)), the turn in
[`internal/api/chat.go`](../../backend/internal/api/chat.go), the loop in
[`internal/chat`](../../backend/internal/chat/chat.go) with `offered`, the tool calls through
[`chat.Loopback`](../../backend/internal/chat/loopback.go); D2 — the agent header on a session
([ADR 0036](0036-a-token-acts-as-its-person-an-agent-flag-is-the-floor-the-agent-header-only-narrows.md)
D3 as amended) holding the person's chat capabilities (`authenticateSession` in
[`api/session.go`](../../backend/internal/api/session.go), `auth.DefaultChatCapabilities`), `GET` and
`PUT /api/v1/me/chat` ([`api/chat_capabilities.go`](../../backend/internal/api/chat_capabilities.go)),
the table `chat_capabilities`
([migration 24](../../backend/internal/store/migrations/000024_chat_capabilities.up.sql), which
replaced the unreleased migration of the tenant's consent under the same version); D3 —
`COWORK_CHAT_PROVIDERS` and the variables of each provider in
[`config/chat.go`](../../backend/internal/config/chat.go), the chart's `chat.providers`; D4 —
[`internal/llm`](../../backend/internal/llm/llm.go), one gateway per provider; D5 — the panel
([`layout/chat-panel.ts`](../../frontend/src/app/layout/chat-panel.ts),
[`core/chat.service.ts`](../../frontend/src/app/core/chat.service.ts),
[`core/chat-stream.ts`](../../frontend/src/app/core/chat-stream.ts)) with the provider's choice and
the capabilities' switches; D6 — [`frontend/nginx/default.conf`](../../frontend/nginx/default.conf),
`"inlineCritical": false` in [`angular.json`](../../frontend/angular.json); D7 — the limits; D8 —
`DELETE /api/v1/tenants/{tenant}/chat/turns` (`StopChatTurns`, `stopTurns` in `api/chat.go`) and the
panel's Stop. Removed with the answers: ~~the tenant's consent (`chat_external_allowed`, its
fingerprint, its audit rows), `COWORK_CHAT_INSIDE`, the availability reason `not_allowed_in_tenant`,
the policy of every tool (`chat/confirm.go`), the `confirm` event, the decisions of a turn, the
`+confirmed` mark, the hold after a confidential ticket~~. The unit tests run the loop, the gateway
and the configuration against the stub provider of [`test/stubllm`](../../backend/test/stubllm/stubllm.go);
the integration tier runs turns through the whole server against it
([`api_chat_test.go`](../../backend/test/integration/api_chat_test.go)) — among them a stop that ends
a slowly streaming turn within a second while the stub sees its request cancelled, a refused close
under the default capabilities and the same close once the person gave `close`, two providers and
the person's pick, and the policies of `chat_capabilities`; the panel, the stream reader and the
service have their vitest specs.

**Verified on 2026-10-04, before the owner's answers were built:** against LM Studio serving
`qwen/qwen3-30b-a3b-2507`, loaded with a context of 32k tokens, the chat filed a ticket, set its
urgency to `now` and moved it to `analysed`; the open board showed the card at once, and every act
was recorded as `chat/qwen:qwen3-30b-a3b-2507/<conversation>`. LM Studio loads a model on first use
with a context of 4096 tokens, which the instructions and the tools exceed
([docs/operations/chat.md](../operations/chat.md#lm-studio-on-the-operators-machine)). Aborting a
turn's request ended the backend's turn and closed the model's request: LM Studio logged that the
client disconnected and stopped generating, through nginx and through the development proxy. The
production bundle behind nginx with the policy of D6 was run in Chromium and WebKit, in both colour
schemes, with the API mocked: no violation of the policy, model text holding markup shown as text, a
turn streamed through nginx and stopped. **Not verified:** the build of the owner's answers against
LM Studio and in a browser — the provider's choice, the capabilities' switches, the stop route —,
which only the stub and the unit and component tests have run; a provider other than LM Studio and
the stub — no call reached OpenAI's or Anthropic's API, Ollama or vLLM; a conversation in the
end-to-end tier, which ~~does not exist~~ *(amended 2026-10-04: exists since that day and
configures no provider of the chat, so it holds none)*
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

The owner's answers weigh the first two differently than the provisional build did. The person, not
a confirmation per act, bounds what the model may do — by the capabilities given to the chat, which
the API enforces on every call. And the operator, not each tenant, decides where a tenant's text
goes — by the providers the chart lists. A model on the owner's machine that makes mistakes and does
not stop by itself made the stop a decision of its own.

## Decision

**D1 — The loop runs in the backend, and its tools are the shared catalogue, run in-process through
the server's own handler.** `POST /api/v1/tenants/{tenant}/chat` runs one turn: the backend calls
the model of the provider the turn names (D3) with the conversation the request carries, runs the
tools the model calls, calls the model again with their answers, and streams the turn to the
browser as server-sent events — `text`, `tool_call`, `ui`, `tool_result`, ~~`confirm`,~~ `error`, and
`done` last, which carries the messages the turn added and why it ended: `answered`, `step_limit`,
`stopped` (D8) or `error`. The backend keeps nothing between turns: the browser holds the
conversation and sends it with each turn; a call that a turn's end left without an answer is
answered to the model as not run on every later turn. The tools are the catalogue of
[`internal/tools`](../../backend/internal/tools/tools.go) that take everything as arguments — not
`session_start`, which reads a working directory, and not `api`: raw access to the API stays the MCP
server's, and a model that reads injected text gets none — and three of the chat's own,
`open_ticket`, `open_backlog` and `open_board`, which show the person a page once the API has said
the person may see it. A tool the chat does not ~~classify (D2)~~ name in `offered` — offered or left
out — is not offered, and a test fails while the shared catalogue holds one it does not name: a tool
joins the chat by a decision, not by being added to the catalogue. Every tool call is a request of
the API sent to the server's root handler in the same process
([`chat.Loopback`](../../backend/internal/chat/loopback.go) over `tools.HandlerDoer`): it passes the
request id, the request log, authentication, the CSRF check, the tenant boundary, validation,
authorization with the agent rules, the audit row and the publication to the event streams like
any request, under a request id of its own and with the person's client address. The loopback
sends nothing outside the turn's tenant, nothing to the chat's own routes — the turn and the stop —
or to the event stream, and no path that is not clean.

**D2 — The chat acts as an agent of the person, ~~and the person decides the acts a person owes a
reason, a note or a decision for~~ with the capabilities the person chose, and nothing waits for the
person.** Every tool call carries the person's session cookie and
`X-Cowork-Agent: chat/<model>/<conversation>` — the picked provider's model, its slashes written as
colons, each part cut to 64 characters — so the request is an agent's
([ADR 0036](0036-a-token-acts-as-its-person-an-agent-flag-is-the-floor-the-agent-header-only-narrows.md)
D3 as amended): ~~every capability of
[ADR 0043](0043-agent-capabilities-are-chosen-per-token-the-default-is-everything-reversible-and-attributable.md)
D4~~ the capabilities its person gave the chat, the hard-off list and every agent rule, and nothing
only a session does. Its acts record the person as the actor, the mark, the capability set and no
token. ~~A per-person choice of the chat's capabilities is not built: the chat holds them all.~~ ~~Every
tool has a policy (`chat/confirm.go` `policies`, removed)~~ *(the
table of the policies — read at once; write at once until the conversation read a confidential
ticket; `finish_work`, `record_answer` and `create_project` always waiting; a `transition` waiting for
`done`, `dropped`, `blocked`, `decided`, a backward move, a reopen and the withdrawal of a done; a
`set_progress` waiting for the write that closes or reopens; `session_start` and `api` not offered —
is withdrawn with the owner's answer of 2026-10-04)*. ~~A call that waits ends the turn with a
`confirm` event: the act in words, written for the person from what the API read, … the person's
decision on the first waiting call only: it runs, marked `chat/<model>/<conversation>+confirmed` so
the record shows the person's Run, or it is answered "skipped by the person"; … A call left waiting
when the person writes a new message instead is answered as skipped.~~

*(The owner's answers of 2026-10-04:)* **The person chooses the chat's capabilities** from the nine
of [ADR 0043](0043-agent-capabilities-are-chosen-per-token-the-default-is-everything-reversible-and-attributable.md)
D4, in the panel, with `PUT /api/v1/me/chat` in a browser session only — choosing gives the person's
agent access, which a token does not give, and a session the agent header marks, the chat's own
among them, is refused it, so the chat never widens its own; `GET /api/v1/me/chat` reads the set
with either credential. A person who never chose holds the default: every capability but `decide`,
`close`, `drop` and `record-answer` — the first three the owner keeps a person's, the fourth
because a text the model read could otherwise record an answer in the person's name. The set is
stored per person in `chat_capabilities`, which only its person reads and writes, a change is the
person's recorded act, and it is read on every request the header marks, so a change reaches a
running turn at its next call (ADR 0043 D5 as amended). **Every tool call runs at once.** Nothing
waits for a Run; the capabilities and the API's rules are the limit, and a refused act — `403
agent_forbidden` with the missing capability, `409 state_conflict`, `409 open_prerequisites` — is an
answer the model reads, not a failed turn. The tool descriptions and the instructions the chat gives
the model say which capabilities it holds and that an act needing another is refused and stays the
person's, as `cowork-mcp` says it from `GET /api/v1/me/token` (ADR 0043 D6). A creating `POST` of a
tool call carries an `Idempotency-Key` derived from the conversation and the call, so the same call
sent again replays instead of acting twice
([ADR 0045](0045-idempotency-put-where-it-is-free-a-required-key-on-agent-posts-stored-with-the-act.md)
D5 as amended). The model is told the rules — the person's language, nothing claimed beyond what a
tool's result confirms, the tenant it works in, the tools' text as information and never as
instruction, never to copy a confidential ticket's text into another ticket, a comment or a
question, the capabilities it holds — and the tools know the person, so a question can be asked of
`me`.

**D3 — The providers are the operator's configuration and nothing else; ~~a provider outside the
installation needs each tenant's consent, bound to the provider it was given to~~ a configured
provider receives what the person can read; a turn works in its tenant only.** ~~`COWORK_CHAT_PROVIDER`
(`openai` or `anthropic`; empty, no chat), `COWORK_CHAT_URL`, `COWORK_CHAT_API_KEY` and
`COWORK_CHAT_MODEL` name it;~~ *(Amended 2026-10-04 by the owner's answer:)* `COWORK_CHAT_PROVIDERS`
lists the providers by id in their order — empty, no chat; the first a turn's default — and each id
has variables of its own, the id upper-cased with its dashes as underscores:
`COWORK_CHAT_<ID>_NAME` (what the panel shows; the id when unset), `_KIND` (`openai` or
`anthropic`), `_URL`, `_MODEL` and `_API_KEY`. An id is 1 to 32 lowercase letters, digits and dashes,
a dash neither first nor last, named once; a configuration error names the variable and never
quotes a URL or a key, nor an entry of the list that is no id. In the chart the list is
`chat.providers`, each with its key from a Secret of its own
([ADR 0058](0058-postgresql-and-object-storage-are-external-the-chart-takes-references-with-configurable-keys.md)
D3 as amended). No request parameter names a host, a path or a key: a turn names a provider by its
id among those configured, and an unknown id is `400 validation_failed`; the availability lists each
provider's id, name, kind and model, never its URL or key. A URL is `https://`, or `http://` only on
a host of the operator's own network by its name or address — `localhost`, a loopback or private
address, a name without a dot, or a name under `.localhost`, `.svc`, `.cluster.local`, `.internal`,
`.local`, `.lan` or `.home.arpa`: the key and the data still travel. ~~`COWORK_CHAT_INSIDE=true` is
the operator's statement that the provider runs inside the installation's trust boundary … and then
every tenant has the chat. Without it (the default) a tenant has the chat only once one of its
administrators allowed the provider in the tenant's settings (`chat_external_allowed`) … The consent
stores the provider's fingerprint … The availability and the consent are read again before every
call of the model, so a withdrawn consent ends a turn that runs.~~ **Every tenant has the chat once a
provider is configured, and a configured provider may see everything the person can read in the
turn's tenant, confidential tickets included** — the risk the owner accepted; the operator's choice
of providers is the only gate. A turn's tool calls stay in the turn's tenant (D1), and the model is
told so.

**D4 — The gateway speaks OpenAI Chat Completions with tool calling and the Anthropic Messages API,
streaming, behind one interface,** one gateway per configured provider.
[`internal/llm`](../../backend/internal/llm/llm.go): `POST {url}/chat/completions` with the tools as
functions, `tool_choice: auto`, `stream: true` and the key as a bearer token where one is set; `POST
{url}/v1/messages` with `x-api-key`, `anthropic-version: 2023-06-01`, the tools with their
`input_schema` and `tool_choice: auto`; both with `max_tokens` 4096 — which OpenAI's reasoning models
refuse, wanting `max_completion_tokens`, a known limit of the OpenAI adapter. A neutral message model
sits between them — the instructions, the person's messages, the model's text and tool calls, the
tools' answers — and a tool call's arguments arrive in fragments and are put together. An answer
that is not streamed is read too, and a reasoning model's `<think>…</think>` is taken out of the
text. The client follows no redirect, gives up on a provider that has not begun to answer within two
minutes or stays silent for ninety seconds, and reads a bounded answer. What a provider answers in an
error never reaches the person: the turn's `error` event says which kind of failure it was, and the
log carries at most 300 characters of the provider's message, that provider's configured key taken
out.

**D5 — The panel renders text only, and navigates only inside the tenant.** Whatever the model or a
tool wrote is shown as text with its line breaks — never as HTML and never as rendered Markdown,
whose sanitiser does not exist yet
([ADR 0011](0011-a-ticket-is-a-markdown-body-plus-first-class-open-questions.md) D6). A tool call is
a card with the tool's name, its arguments folded away and the start of its answer ~~and Run and
Skip where it waits~~. A `ui` event opens a page only when it is a ticket, a backlog or a board of the
turn's tenant; anything else is shown on the call's card as not opened. The conversation lives in
the browser per tenant and is gone when another tenant's pages open; the panel exists only while
the tenant's chat is available, and whether it is open is the person's preference in browser
storage. *(Added 2026-10-04 with the owner's answers:)* Where the installation configures more than
one provider, the panel's header offers them by name, and the pick is the person's preference in
browser storage, `cowork.chat.provider.<person id>` — the first configured when the pick is gone. A
section of the panel, *What the assistant may do*, shows the nine capabilities with what each lets
the chat do, switched as the chat holds them, with the token page's *Full* and *Assisted* shortcuts;
a switch sends the whole set at once.

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
text, 32 tool calls a message ~~and 32 decisions~~; a tool's answer clipped to 16,000 characters for
the model and 2,000 for the person; and the gateway's bounds of D4. A comment keeps a silent turn open
every ten seconds.

**D8 — A running turn stops at once, from anywhere** *(added 2026-10-04 by the owner's answer: "a
running agent must be stoppable at once")*. Aborting a turn's request ends it: the turn's context is
the request's, so the model's request and a tool call in flight are cancelled with it.
`DELETE /api/v1/tenants/{tenant}/chat/turns` ends every running turn of the session's person in the
tenant on the replica that answers it, at once: each turn the replica runs is registered with the
cancel of its context while it runs — the registry the count of D7 reads — and the stop cancels it,
so the model's request and a tool call in flight are cancelled, and the turn's stream ends with
`done` and the reason `stopped`, without an `error` event. The answer is `204` once those turns have
ended, or after five seconds; nothing running is no error. A turn of another person, or of the person
in another tenant, runs on. The route takes a session only, any member's, and a session the agent
header marks is refused it, so the chat cannot stop itself or anyone. The panel's Stop aborts the
turn's request *and* calls the route, so a proxy that keeps the backend request open after the
browser aborted keeps no turn alive; the `chat_busy` notice — a turn of the person runs elsewhere —
offers a Stop that calls the route. What the stopped turn's calls did before has happened. The
registry is the replica's, as the count of D7 is: behind several backend replicas, the route stops
only the turns of the replica the request reaches.

## Consequences

- One catalogue, one implementation: a tool means the same to Claude Code and to the chat, and a
  change to a tool reaches both. The chat can do nothing the API cannot — and nothing an agent
  holding ~~every capability~~ the person's chosen capabilities in the person's session cannot.
- The person's acts and the chat's stay apart in the record: an act of the chat names the person,
  the model, the conversation and the capability set ~~and an act the person ran from a proposal
  says `+confirmed`~~.
- What the browser shows moves with the agent: a write of a tool call reaches the open pages
  through the event stream like any write, and a page tool moves the person's own page.
- The backend holds a conversation for the length of a turn only; a turn is an HTTP request that
  can last minutes, held open by its comments, and an Ingress in front must not buffer it
  ([docs/operations/chat.md](../operations/chat.md)).
- ~~A provider outside the installation reads what the chat reads in a tenant that allowed it —
  confidential tickets included, H-37 — and every tenant's when the operator declares a provider
  inside.~~ A configured provider reads what the chat reads in every tenant, confidential tickets
  included (H-37, accepted). [ADR 0020](0020-notifications-are-an-in-app-inbox-per-person.md) D5 —
  nothing leaves cowork on its own — holds: the chat sends a tenant's text to a provider in a
  person's turn, never by itself.
- A model steered by injected text does whatever the chosen capabilities and the API allow, at once
  (H-38); the default keeps deciding, closing, dropping and recording answers the person's.
- The records this one changes are amended in place, each saying so: ADR 0035 D5 and ADR 0031 D6
  (~~thirteen~~ sixteen session-only operations; ~~and the consent field~~), ADR 0036 D3 and D7 (the
  header on a session, the person's set), ADR 0039 D2 (the turn's limits, the stop), ADR 0040,
  ADR 0042 and ADR 0043 (the second host of the catalogue, `set_urgency`, what the chat may do — D5,
  the person's choice), ADR 0045 D5 (the keys of a tool call), ADR 0046 (the turn outside the
  generated server, the new routes), ADR 0047 (the turn's error event and its codes), ADR 0050 D4
  (the choice without `If-Match`), ADR 0021 D6 (`chat_capabilities`), ADR 0052 (the policy, the
  budget) and ADR 0058 D3 (one Secret per provider).

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
- **The chat holding every capability**, as provisionally built. Lost to the owner's answer: the
  person chooses, and the default leaves deciding, closing, dropping and recording answers to the
  person.
- **Consent per tenant**, as provisionally built: a tenant's administrators allow a provider outside
  the installation, bound to its kind, host and model, and `COWORK_CHAT_INSIDE` declares a provider
  inside. Lost to the owner's answer — everything the person can read, the providers chosen by the
  operator in the chart; the risk is accepted and named (H-37). ~~Lost to D3's per-tenant consent;
  `COWORK_CHAT_INSIDE` remains the operator's statement for a provider of their own.~~ **Consent per
  installation only** was the alternative that lost to the consent first; it is, in effect, what the
  owner chose, with the list of providers in place of one.
- **Confidential tickets withheld from a provider outside**, as the option of per-tenant consent
  first described it. Not built: the owner chose everything the person can read.
- **Only providers inside the installation.** Lost: the owner wants hosted providers too.
- **The acts a person owes a reason, a note or a decision for confirmed before they run**, as
  provisionally built, with every write once the conversation had read a confidential ticket. Lost to
  the owner's answer: nothing waits; the capabilities are the limit. **Every write confirmed** lost
  the other way: filing, ranking and editing would wait for a click each.
- **The chat's capabilities as a nullable column of `users`.** One table less; but the update policies
  of `users` are permissive and admit an administrator of the account, the start-up synchronisation
  and the identity provider, and a policy that let a person update their own row would admit every
  column the runtime role may update there — `global_admin` among them. A table of its own, which
  only its person reads and writes, keeps the second line as it was (ADR 0021 D6).
- **A stop that reaches every replica**, through the channel the event streams already listen on
  ([ADR 0054](0054-server-sent-events-per-tenant-carry-keys-not-content-polling-is-the-fallback.md)).
  Not built: a stop that arrives after the person sent a new turn would end that turn too unless it
  carried what to stop, by an id or a time the replicas share; the count of turns is per replica
  already, the chart runs one backend replica by default, and the abort of a turn's request reaches
  its replica through its own connection (H-48).
- **An agent built into the browser, through WebMCP.** No backend loop at all; an origin trial in one
  browser. Lost.
- **Angular's `security.autoCsp`** for D6, which hashes the inline loader into a policy the page
  carries. Not taken: the policy stays one header in nginx, and the loader goes.

## Residual risks

What each mechanism leaves open is [docs/security/chat.md](../security/chat.md#what-this-does-not-cover):
every configured provider receives what the chat reads, confidential tickets included — accepted by
the owner (H-37); the acts the chosen capabilities allow, which a model steered by injected text
takes at once (H-38); a mark the client could write itself (H-39); plain http allowed by a host's
name (H-41); a log clip that knows only the configured key (H-42); no budget for a paid provider,
and a turn limit per replica (H-43); a model's text that contradicts the tool results (H-44); and a
stop that reaches only the replica it lands on (H-48). ~~every capability, for everyone (H-40); … a
hold on writes that lives in the conversation the browser keeps … (H-45); two proposals that carry
no precondition (H-46); and the provider's host in the consent's audit row (H-47)~~ — closed with the
owner's answers; their numbers are not reused. ~~The decision itself is provisional: an answer of the
owner can move any of D1–D7.~~

## References

- [ADR 0036](0036-a-token-acts-as-its-person-an-agent-flag-is-the-floor-the-agent-header-only-narrows.md) D3, D7 — the header on a session, amended for D2
- [ADR 0043](0043-agent-capabilities-are-chosen-per-token-the-default-is-everything-reversible-and-attributable.md) — the capabilities, the person's choice for the chat (D5), and the hard-off list the chat holds
- [ADR 0040](0040-rest-is-the-contract-mcp-is-the-ergonomic-surface-and-can-do-nothing-the-api-cannot.md), [ADR 0042](0042-twelve-workflow-tools-and-one-escape-hatch.md) — the catalogue's first host and its tools
- [ADR 0035](0035-personal-access-tokens.md) D5, [ADR 0031](0031-server-side-sessions-in-an-httponly-cookie.md) D6 — what only a session does
- [ADR 0039](0039-no-request-budgets-size-and-time-limits-instead-configurable-and-switchable.md) — the limits D7 adds to
- [ADR 0045](0045-idempotency-put-where-it-is-free-a-required-key-on-agent-posts-stored-with-the-act.md), [ADR 0050](0050-optimistic-concurrency-a-version-per-entity-if-match-where-a-write-overwrites.md) — the keys a tool call carries, and the choice of the capabilities without `If-Match`
- [ADR 0021](0021-row-level-security-is-the-second-line-of-tenant-isolation.md) D6 — `chat_capabilities`, a named table
- [ADR 0058](0058-postgresql-and-object-storage-are-external-the-chart-takes-references-with-configurable-keys.md) D3 — one Secret per provider
- [ADR 0065](0065-a-confidential-flag-replaces-the-file-name-embargo-set-automatically-lifted-only-by-a-person.md) — the confidential flag, which no provider is held to
- [ADR 0052](0052-primeng-with-the-angular-cdk-a-themes-preset-and-dark-mode-from-the-start.md) D4, [ADR 0011](0011-a-ticket-is-a-markdown-body-plus-first-class-open-questions.md) D6 — the policy and the sanitiser
- [docs/developer/chat.md](../developer/chat.md), [docs/operations/chat.md](../operations/chat.md), [docs/security/chat.md](../security/chat.md) — how it works, how it is run, what it leaves open
