# The chat in the UI

How the assistant at the right edge of the UI is built: the turn's route and the stop's, the loop in
`internal/chat`, the loopback that sends its tool calls through the whole server, the capabilities the
person gives the chat, the providers and the gateway in `internal/llm`, the stream, the panel, and
the shell's content-security policy. The decision is
[ADR 0076](../adr/0076-the-chat-in-the-ui-runs-its-loop-in-the-backend-as-an-agent-of-the-person.md);
running it is [docs/operations/chat.md](../operations/chat.md); what it leaves open,
[docs/security/chat.md](../security/chat.md). The tool catalogue it runs is [mcp.md](mcp.md). Read
against the tree on 2026-10-05.

```
ChatPanel ─ ChatService ─ fetch POST /api/v1/tenants/{tenant}/chat ─► Ingress ─────► httpserver ─► api pipeline
   │                                                                                (session, CSRF, boundary,
   │                                                                                 body limit, validation)
   ◄──── text/event-stream: text, tool_call, ui, tool_result, error, done ─────────┐   │
   │                                                                                │   ▼
   │                                   api/chat.go serveChat: provider, Check, the person's capabilities,
   │                                                          chat_busy + registry, streamTurn
   │                                                                                │   ▼
   │                                   chat.Run: instructions, catalogue, loop ──► llm.Provider.Complete ──► the picked provider
   │                                              │
   │                                              └─► tools.Tool.Call ─► apigen client ─► chat.Loopback
   │                                                      ─► the server's root handler again: request id, log,
   │                                                         session + X-Cowork-Agent (the person's chat set), CSRF,
   │                                                         boundary, handler, audit
   └─ Stop: abort the fetch + DELETE …/chat/turns ─► StopChatTurns ─► stopTurns: cancel the registered turns
```

## Where it lives

| Package, file | Responsibility |
|---|---|
| [`api/chat.yaml`](../../backend/api/chat.yaml), the `Chat…` schemas of [`components/schemas.yaml`](../../backend/api/components/schemas.yaml) | `getChatAvailability`, `runChatTurn` and `stopChatTurns`; the turn's body (`ChatTurn`: the conversation's id, the provider's id, the messages, the page) and the data of every event; the contract of the stream in prose |
| [`api/me.yaml`](../../backend/api/me.yaml) `chat` | `getMyChat` and `setMyChat`, the person's chat capabilities (`ChatCapabilities`, `ChatCapabilitiesUpdate`) |
| [`internal/api/chat.go`](../../backend/internal/api/chat.go) | `ChatOptions`, `ChatProvider`; `GetChatAvailability` and `chatAvailability` — the providers' ids, names, kinds and models; `serveChat` — the turn after the pipeline; `chatProvider`, the provider a turn names; `startTurn` — the count and the registry of a person's turns (`runningTurn`); `stopTurns`, `StopChatTurns`; `streamTurn` — the headers, the keep-alive, the shutdown, the stop's cause, `error` and `done`; `chatTurn` — the session cookie, the page, the loopback, the mark, the person, the capabilities; `turnProblem`; `eventWriter` |
| [`internal/api/chat_capabilities.go`](../../backend/internal/api/chat_capabilities.go) | `GetMyChat`, `SetMyChat` — the set in the catalogue's order, the person's recorded act — `ordered` |
| [`internal/api/session.go`](../../backend/internal/api/session.go) | `authenticateSession`: a request the agent header marks holds `chatCapabilities` — the person's set, or `auth.DefaultChatCapabilities` |
| [`internal/api/api.go`](../../backend/internal/api/api.go) `serveOperation` | sends `runChatTurn`, after the timeout bounded reading its body, the body limit and the validation, to `serveChat` with a context the request timeout does not cut |
| [`internal/store/chat.go`](../../backend/internal/store/chat.go), [`queries/*/chat.sql`](../../backend/internal/store/queries/read/chat.sql), [migration 24](../../backend/internal/store/migrations/000024_chat_capabilities.up.sql) | `DB.ChatCapabilities`, read as the person in a transaction of its own; `GetChatCapabilitiesForUpdate`, `SetChatCapabilities`; the table `chat_capabilities` and its policies |
| [`internal/chat/chat.go`](../../backend/internal/chat/chat.go) | `Run` and the loop (`runner`): `offered`, the tools the chat names; `catalogue`; `split` — the conversation into what the model reads, a call without an answer answered as not run —; `runCall` with the derived keys (`keys`), `answer`, `calls` — the model's calls as the conversation keeps them —, `clipText` |
| [`internal/chat/check.go`](../../backend/internal/chat/check.go) | `Check`: the conversation a turn may send |
| [`internal/chat/loopback.go`](../../backend/internal/chat/loopback.go) | `Loopback` — the tool calls to the server's own handler, in the turn's tenant only —, `Editor` (the cookie, the CSRF pair, the mark), `Mark`, `NewSession` (the capabilities the session assumes) |
| [`internal/chat/prompt.go`](../../backend/internal/chat/prompt.go), [`ui.go`](../../backend/internal/chat/ui.go) | the instructions of a turn and `holds`, the capabilities in words; `open_ticket`, `open_backlog`, `open_board` |
| [`internal/llm/llm.go`](../../backend/internal/llm/llm.go) | `Provider`, the neutral `Request`, `Message`, `ToolCall`, `Tool`, `Response`; `Config`, `New`; `Error` with its kinds |
| [`internal/llm/client.go`](../../backend/internal/llm/client.go), [`openai.go`](../../backend/internal/llm/openai.go), [`anthropic.go`](../../backend/internal/llm/anthropic.go), [`think.go`](../../backend/internal/llm/think.go) | the HTTP client and the bounds, the two wire formats, the filter of a reasoning model's `<think>` |
| [`internal/config/chat.go`](../../backend/internal/config/chat.go) | `COWORK_CHAT_PROVIDERS` and each provider's `COWORK_CHAT_<ID>_*` (`ChatEnv`), the limits, the rule of the URL (`checkChatURL`, `ownNetwork`) |
| [`internal/auth/principal.go`](../../backend/internal/auth/principal.go) | `DefaultChatCapabilities` |
| [`cmd/cowork/main.go`](../../backend/cmd/cowork/main.go) `chatOf` | a gateway per provider, the start's log lines, `api.ChatOptions` with the signal context as `Shutdown` and the root handler, built after the API, as `Loopback` |
| [`test/stubllm`](../../backend/test/stubllm/stubllm.go) | the provider of the tests; `Cancelled` counts the requests a client ended while the stub held its answer back |
| [`frontend/src/app/core/chat.service.ts`](../../frontend/src/app/core/chat.service.ts) | `ChatService` — the availability, the providers and the person's pick, the capabilities and the notice after a change of them that offers a new conversation (`noteChange`), the conversation per tenant, one turn at a time, Stop —, `TurnRecord`, `pageContext`, `navigable`, `CHAT_FETCH` |
| [`frontend/src/app/core/chat-stream.ts`](../../frontend/src/app/core/chat-stream.ts) | `EventStreamParser`, `chatEvent`, `chatEvents`: the stream of a `fetch` read as it arrives |
| [`frontend/src/app/layout/chat-panel.ts`](../../frontend/src/app/layout/chat-panel.ts), `.html`, `.scss` | `ChatPanel`; the toggle and the deferred panel in [`shell.html`](../../frontend/src/app/layout/shell.html) |
| [`frontend/src/app/shared/capabilities.ts`](../../frontend/src/app/shared/capabilities.ts) | `capabilityMeanings`, `assisted` — shared with the token page |
| [`frontend/nginx/default.conf`](../../frontend/nginx/default.conf), [`frontend/angular.json`](../../frontend/angular.json) | the shell's policy; `"inlineCritical": false` |

## A turn

1. **The pipeline** treats `POST …/chat` as a session-only write: authentication by the cookie (a
   token is `403 session_required`), the session rules — the CSRF check, the agent header refused
   on what only a session does, a temporary password —, the tenant boundary, the request timeout
   while the body is read, `COWORK_MAX_JSON_BODY`, validation against `ChatTurn`
   ([api.md](api.md#the-pipeline)). Then `serveOperation` hands the request to `serveChat` with the
   context from before the request timeout.
2. **`serveChat`** authorizes the reader role, asks the availability (`409 chat_unavailable` without
   a provider), decodes the body, picks the provider (`chatProvider`: the id the turn names, or the
   first; an id not configured is `400 validation_failed` at `/provider`), holds the conversation to
   `Check` — it begins and ends with the person's message; the model's messages carry text or calls
   with ids of their own; a tool's message answers a call of the model's message before it
   (`400 validation_failed` naming the message) —, reads the person's chat capabilities, builds the
   turn (`chatTurn`), and registers it (`startTurn`: the turn's tenant and the cancel of its context;
   `429 chat_busy` beyond `COWORK_CHAT_TURNS_PER_PERSON` on this replica). Everything before this
   point is a problem answer.
3. **`streamTurn`** answers `200 text/event-stream` with `X-Accel-Buffering: no`, starts the
   keep-alive (`: keep-alive` after ten seconds without a write; a goroutine of its own, under the
   writer's lock), bounds the turn by `COWORK_CHAT_TURN_TIMEOUT` and by the shutdown, and runs
   `chat.Run` with the picked provider. Whatever ends the turn, `done` is the last event: a turn the
   stop cancelled (the cause `errStopped`) ends with the reason `stopped` and no `error`; any other
   failure comes after an `error` event — `turnProblem` maps the provider's error to
   `chat_provider_failed` with the gateway's sentence and logs the clip, the timeout to `timeout`, the
   shutdown to `not_ready`, anything else to `internal`.
4. **`chat.Run`** writes the instructions (`system`, with the capabilities the session holds, which replace whatever earlier messages say the chat may do),
   builds the catalogue — the shared tools of `tools.Catalogue(tools.Anywhere)` and the three page
   tools that `offered` names as offered, each described with the capabilities it can run into the
   chat holds or lacks (`Tool.Describe`) — and turns the conversation into what the model reads: a
   call of the model without an answer, which a turn's end left, is answered as not run where it
   stands.
5. **The loop**, up to `COWORK_CHAT_MAX_STEPS` calls of the model: call `Provider.Complete` with the
   instructions, the history and the tools — the text streams out as `text` events —, keep the answer
   (`calls` gives every call an id of its own, drops more than 32, and keeps arguments that are no
   object or too long apart, answered with why), and run each call at once: send it as `tool_call`,
   run it (`runCall` → `Tool.Call`), answer it as `tool_result`. The context ending between calls ends
   the turn. An answer without calls ends the turn as `answered`.
6. **`done`** carries the messages the turn added, in the neutral model — the browser appends them
   as they are and sends the whole conversation with the next turn. The backend keeps nothing.

A tool's refusal is an answer the model reads, not a failed turn — an act that needs a capability
the person did not give is `403 agent_forbidden` naming it. A tool's answer reaches the model clipped
to 16,000 characters and the person to 2,000 (`clipText` says how much was left out).

## Stop

The turn's context is the request's (from before the request timeout), so the browser's abort of the
`fetch` ends it: the model's request is cancelled with it, and so is a tool call in flight, whose
context the loopback ties to the turn's. `DELETE /api/v1/tenants/{tenant}/chat/turns`
(`StopChatTurns`) is the second path: `startTurn` registers every turn this replica runs — its
tenant, the `context.CancelCauseFunc` of its context, and a channel closed when it has ended — in the
same map `COWORK_CHAT_TURNS_PER_PERSON` counts (`handler.turns`); `stopTurns` cancels the person's
turns in the tenant with the cause `errStopped`, and the route waits until they have ended, at most
`stopWait` (five seconds), so a turn sent right after finds their places free. The route is
session-only in the document, so a token is `403 session_required` and an agent-marked session `403
agent_forbidden`; the loopback refuses the chat's own routes besides. The registry is the replica's:
a turn of another replica is not reached
([docs/security/chat.md H-48](../security/chat.md#h-48)).

## The loopback

A tool call is a request of the API, built by the generated client (`apigen.ClientWithResponses`
over `chat.Loopback` as its `HttpRequestDoer`) and served by the server's own root handler in the
same process (`tools.HandlerDoer`) — the handler `httpserver.New` builds, so the call gets a request
id of its own, a line in the request log and the panic recovery before the API pipeline runs it.
`runServe` builds that handler after the API it wraps, so `ChatOptions.Loopback` is a function read
at the first turn; without it — in a test that builds the API alone — the calls go to the API's
handler.

- **The request** carries the person's session cookie, `Origin: COWORK_BASE_URL`,
  `X-Requested-With: cowork`, `X-Cowork-Agent: chat/<model>/<conversation>` — the picked provider's
  model — and `User-Agent: cowork-chat` (`Editor`); the `Idempotency-Key` of a creating `POST` is the
  tool's, which `runCall` makes derive from the conversation's id, the call's id and a counter
  (`keys`). Its client address is the turn's: the loopback copies the turn request's peer and
  `X-Forwarded-For`, so the source hash of the acts is the person's.
- **The capabilities** are not the loopback's: the session's resolver reads the person's set on every
  request the header marks (`authenticateSession`), so a change of the set reaches a running turn at
  its next call. `NewSession` makes the tools assume a token — an agent with the capabilities read at
  the turn's start — so no tool reads `/api/v1/me/token`, and the descriptions name them.
- **The context** ends when the turn's does and carries its deadline, so a turn that runs out of time
  reads as a timeout in the call too; it carries none of the turn request's values — the pipeline
  authenticates and admits the call anew (`detached`).
- **What it refuses** before anything is served (`allowed`): a path that is not clean, the chat's own
  routes (`/chat` and below: a turn never starts or stops another), the event stream (a recorder would
  buffer it forever), and any path outside `/api/v1/tenants/<the turn's tenant>` and
  `/api/v1/tickets/<the turn's tenant>/` — the person's other tenants and `/api/v1/me` included.
  `NewSession` binds the tools to the page's project or to the tenant, confines a search of every
  tenant to the turn's (`Session.Tenants`), and names the person (`Session.Person`, so
  `open_question` asks `me` without `/api/v1/me`).

`HandlerDoer` keeps the whole answer in memory before the tool reads it; the bounds are the API's —
the page size, the body of a ticket — and the clip of what the model reads.

## The capabilities

`offered` in [`chat.go`](../../backend/internal/chat/chat.go) names every tool the chat may meet and
whether the model gets it; a tool it does not name is not offered (`TestEveryToolIsNamed` fails while
the shared catalogue holds one it does not name). `session_start` and `api` are named and left out.
Every other tool runs at once; what bounds it is the agent's capabilities and the API's rules:

| Where | What |
|---|---|
| `auth.DefaultChatCapabilities` | `rank`, `set-horizon`, `interest`, `upload`, `create-project` — the set of a person who never chose |
| `chat_capabilities` | one row per person, `capabilities text[]` within the nine — and `override-urgency`, the name `set-horizon` had before, which the check still takes because release 0.5 writes it after an image rollback, and `auth.Canonical` drops on read (migration 38 rewrote the rows that held it) —; the policies admit the person's own row only; no delete grant |
| `PUT /api/v1/me/chat` (`setMyChat`) | session only; the whole set, in any order, unique; stored in the catalogue's order; an installation-level `updated` act on the person with `chat_capabilities` before and after; the same set again records nothing |
| `GET /api/v1/me/chat` (`getMyChat`) | either credential; `{capabilities, chosen}` |
| `authenticateSession` | a request with `X-Cowork-Agent` on a session holds the set (`chatCapabilities`) — the chat's, and any person's who sends the header themselves |
| `auth.Authorize` | refuses an act whose capability the request lacks: `403 agent_forbidden`, `missing capability: …` |
| `holds` in [`prompt.go`](../../backend/internal/chat/prompt.go), `Tool.Describe` | tell the model which capabilities it holds |

### Adding a tool to the chat

1. A shared tool comes first, as [mcp.md](mcp.md#adding-a-tool) says — the chat runs what the MCP
   server runs. A tool that only makes sense in the browser is the chat's own, in
   [`ui.go`](../../backend/internal/chat/ui.go) through `tools.Define`.
2. Name it in `offered`: `true` to give it to the model, `false` for a tool a model reading injected
   text must not have. Nothing of it waits for the person: an act the person should keep needs a
   capability the API checks ([ADR 0043](../adr/0043-agent-capabilities-are-chosen-per-token-the-default-is-everything-reversible-and-attributable.md)),
   and the tool's `limitsOf` names it, so the description says whether the chat holds it.
3. A page tool sends a `ui` event only after the API answered that the person may see the page, and
   only for a path the frontend's `navigable` accepts — extend `navigable` with it, or the panel
   shows the call as not opened.
4. Tests: a turn that runs it in [`chat_test.go`](../../backend/internal/chat/chat_test.go), and a
   turn through the whole server in [`api_chat_test.go`](../../backend/test/integration/api_chat_test.go)
   when it writes — under the default capabilities when it needs one.

## The providers and the gateway

`config.Chat.Providers` is the list of `COWORK_CHAT_PROVIDERS`, each with its id, name, kind, URL, key
and model (`ChatEnv(id, suffix)` names a variable); `chatOf` builds one `llm.Provider` per entry and
hands them to `api.ChatOptions.Providers` in order. `llm.Provider` has one method,
`Complete(ctx, Request, onText)`: the instructions, the messages of the neutral model, the tools with
their JSON Schemas; `onText` receives the answer's text piece by piece as it streams, and the
`Response` is the whole answer — its text, its tool calls put together from their fragments, why it
ended, what the provider counted. `New` builds the adapter of the configured wire format:

| | OpenAI Chat Completions | Anthropic Messages |
|---|---|---|
| Request | `POST {url}/chat/completions`; the instructions as the `system` message; a tool's answer as a `tool` message; the tools as `function`s, `tool_choice: auto`; `Authorization: Bearer` where a key is set | `POST {url}/v1/messages`; `system`; a tool's answer as a `tool_result` block in the next user message, messages of one role in a row merged; the tools with `input_schema`, `tool_choice: {type: auto}`; `x-api-key`, `anthropic-version: 2023-06-01` |
| Both | `stream: true`, `max_tokens: 4096` | the same |
| The stream | `data:` chunks with `choices[0].delta` — the text, the calls' `arguments` in fragments by `index` — and `[DONE]` | `message_start`, `content_block_start` (`text` or `tool_use`), `content_block_delta` (`text_delta`, `input_json_delta`), `message_delta` with the stop reason, `message_stop`; a thinking block is read past |
| Not streamed | one JSON body, read as well | the same |

`client.go` holds what both share: `Client` follows no redirect and gives up on a provider that has
not begun to answer in two minutes; `readEvents` gives up after ninety seconds without a line, on a
line over 1 MiB and on a stream over 32 MiB; an answer that is not streamed is read up to 8 MiB; an
answer's text is kept up to 256 KiB, a call's arguments up to 64 KiB (`TooLong` beyond), and 64 calls
an answer. An error is an `*llm.Error`: `Detail`, a sentence of the gateway's for the person — the
status and what it usually means, never the provider's words — and `Clip`, at most 300 characters of
the provider's message with that provider's key taken out, for the log only. `thinkFilter` takes
`<think>…</think>` out of the text in whatever cut the pieces arrive.

**Adding a wire format** is a type implementing `Provider` in `internal/llm`, a case in `New`, the
value in `config/chat.go` and in the chart's `cowork.chatEnabled`, the format in `test/stubllm`, and
the tests of [Tests](#tests) for it.

## The stream and the panel

The backend writes each event as `event: <name>` and `data: <json>` and flushes it; the data are the
generated types `ChatTextEvent`, `ChatToolCall`, `ChatUiEvent`, `ChatToolResultEvent`, `Problem` and
`ChatDoneEvent`. The browser reads it with `fetch`: the answer of a `POST` is a stream, which neither
the `HttpClient` (it waits for the whole body) nor `EventSource` (`GET` only) reads as it arrives.
[`chat-stream.ts`](../../frontend/src/app/core/chat-stream.ts) cuts the bytes into events by the HTML
standard's event-stream format — a character split between two pieces waits for its rest, comments
are read past — and `chatEvent` holds each event's data to the shape the document gives it, reading
past one it does not know.

[`ChatService`](../../frontend/src/app/core/chat.service.ts) holds the tenant's availability (a
`resource` over `GET …/chat`), the providers and the person's pick (`provider`: the stored id while it
is configured, else the first; `setProvider` keeps it under `cowork.chat.provider.<person id>` in
`localStorage`, every access in `try`), the capabilities (a `resource` over `GET /api/v1/me/chat`,
read while the panel is open; `setCapabilities` puts the whole set), one conversation per tenant —
cleared when another tenant's pages open, never written to storage —, and one turn at a time. `send`
posts the whole conversation with the page (`pageContext`: the path, the project's key, the ticket's
short key, each in the shape the document allows), the conversation's id and the picked provider's
id, with `X-Requested-With: cowork` and the session's cookie. `done` appends its messages; a turn that
ends without `done` — Stop, a cut connection — is written down by `TurnRecord` from the events it saw,
so the model learns next time what happened: the calls that reported a result with their summary, a
call without a result left out. A `ui` event navigates only when `navigable` accepts its path.
`stop` aborts the `fetch` and calls `DELETE …/chat/turns` for the turn's tenant, once; a refused stop
is reported as a problem. A `chat_busy` refusal is a notice whose Stop is `stopElsewhere`, the same
route; a `done` with the reason `stopped` — stopped from elsewhere — shows as stopped. Whether the
panel is open is `cowork.chat.<person id>` in `localStorage`.

[`ChatPanel`](../../frontend/src/app/layout/chat-panel.ts) shows the conversation as text — Angular's
interpolation, never `innerHTML`, no Markdown —, a call as a card with its name, its arguments as
folded JSON text, the start of its answer and its state, and beneath the answer of a turn that ended
`answered` without calling any tool a note that nothing in it was looked up (`noTools`, from
`TurnRecord.called`); Stop calls `ChatService.stop`. The header holds the provider's choice — a PrimeNG
select, only where more than one provider is configured, off while a turn runs — and the toggle of
*What the assistant may do*: a section with the nine capabilities as switches, each with its meaning
from [`shared/capabilities.ts`](../../frontend/src/app/shared/capabilities.ts), and the *Full* and
*Assisted* shortcuts; a switch or a shortcut sends the whole set, and the switches are off while it is
on its way. The shell shows the toggle while the tenant's chat is available and loads the panel's
code with `@defer` once it is. The panel takes 24rem beside the content and lies over it on a window
narrower than 64rem (`overlayQuery` in [`shell.ts`](../../frontend/src/app/layout/shell.ts)), where
Escape and the focus moving into the page beneath close it.

## The content-security policy

The frontend's nginx configuration keeps the policy in one nginx variable, `$ui_csp`, and adds it with `always` in each of
the three locations that serve the UI — the icons, the hashed bundles, `index.html` — because an
`add_header` inside a location replaces the server's
([trust-boundaries.md](../security/trust-boundaries.md#the-shells-content-security-policy)). Under
`script-src 'self'` the production build inlines no critical CSS (`"inlineCritical": false` in
`angular.json`): the inliner's loader is an inline `onload` handler. A change to the configuration
or to the build is checked by running the built image behind the Ingress stand-in in a browser and
watching the console for a violation; nginx has no unit test
([build-test-lint.md](build-test-lint.md#run-the-images-together)).

## Tests

- **Unit, the loop** ([`chat_test.go`](../../backend/internal/chat/chat_test.go)), against a scripted
  model (`model`, an `llm.Provider`) and a fake API behind the real loopback (`api`; `turn` and
  `turnHolding` give the session the default or a chosen set): the tool rounds
  (`TestATurnRunsTheToolsTheModelCalls`), the step limit, every call at once and a refusal the model
  reads (`TestEveryCallRunsAtOnce`), what the model is told it holds
  (`TestTheModelIsToldWhatTheChatHolds`), a call a turn left (`TestACallItsTurnLeftIsNotRun`), a turn
  stopped while a call runs (`TestAStoppedTurnEndsAtOnce`), the page tools, what a model gets wrong, a
  failed turn, the loopback's refusals, the mark, `Check`, the clip, every tool named, a question asked
  of `me`, arguments too long.
- **Unit, the gateway** ([`llm_test.go`](../../backend/internal/llm/llm_test.go)), against
  [`test/stubllm`](../../backend/test/stubllm/stubllm.go) on `httptest`'s loopback listener: both
  formats streamed and not, no key no header, a refusal that never echoes the provider, no redirect,
  an answer it cannot read, a silent or absent provider, the think filter, the bounds of an answer
  and `max_tokens` in every request, an answer of white space, `Head` and `Cut`.
- **Unit, the rest**: the variables ([`config/chat_test.go`](../../backend/internal/config/chat_test.go)),
  the turn held to the document and the availability's shape, the provider a turn picks, the turns per
  person and the stop's registry ([`api/chat_test.go`](../../backend/internal/api/chat_test.go)), the
  agent header on a session ([`api/session_test.go`](../../backend/internal/api/session_test.go)), the
  named table's policy in the migration set ([`store/policy_test.go`](../../backend/internal/store/policy_test.go)),
  the session-only operations ([`api/document_test.go`](../../backend/api/document_test.go)), and the
  shared tools' arguments in [`tools/`](../../backend/internal/tools/) (`TestPlaceTicket`,
  `TestPreconditionsTheCallerRead`, `TestAHostThatKnowsThePerson`).
- **Integration** ([`api_chat_test.go`](../../backend/test/integration/api_chat_test.go)), through the
  whole server against [`test/stubllm`](../../backend/test/stubllm/stubllm.go) — no real model: the
  availability with two providers; the person's pick and the default; a turn that files a ticket and
  ranks it to `now`, its acts the person's with the chat's mark, the default set and a key; the
  person's capabilities — a close refused, chosen in a session only, recorded, then run at once; a
  token, a CSRF failure and an agent-marked session refused; a turn that stays in its tenant; the
  turn's time, its keep-alive and a failing provider; the agent header on a session; the turn limit
  and the shutdown; the stop of a slowly streaming turn within a second, the stub seeing its request
  cancelled, another person's turn and the person's turn in another tenant untouched; a question
  asked of the person; the policies of `chat_capabilities`.
- **Frontend** (vitest on jsdom): [`chat-stream.spec.ts`](../../frontend/src/app/core/chat-stream.spec.ts),
  [`chat.service.spec.ts`](../../frontend/src/app/core/chat.service.spec.ts),
  [`chat-panel.spec.ts`](../../frontend/src/app/layout/chat-panel.spec.ts), and the chat's parts of
  `shell.spec.ts`.

Response validation does not reach a turn: its answer is a stream, not a response of the generated
server ([testing.md](testing.md#response-validation)). A real model is checked by hand
([docs/operations/chat.md](../operations/chat.md#lm-studio-on-the-operators-machine)); the end-to-end
tier ([testing.md](testing.md#end-to-end-tests)) configures no provider of the chat and holds no
conversation yet.
