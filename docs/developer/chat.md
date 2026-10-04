# The chat in the UI

How the assistant at the right edge of the UI is built: the turn's route, the loop in
`internal/chat`, the loopback that sends its tool calls through the whole server, the policy of
every tool and the person's decisions, the gateway in `internal/llm`, the stream, the panel, and the
shell's content-security policy. The decision is
[ADR 0076](../adr/0076-the-chat-in-the-ui-runs-its-loop-in-the-backend-as-an-agent-of-the-person.md),
provisional; running it is [docs/operations/chat.md](../operations/chat.md); what it leaves open,
[docs/security/chat.md](../security/chat.md). The tool catalogue it runs is [mcp.md](mcp.md). Read
against the tree on 2026-10-04.

```
ChatPanel ─ ChatService ─ fetch POST /api/v1/tenants/{tenant}/chat ─► nginx /api/ ─► httpserver ─► api pipeline
                                                                                  (session, CSRF, boundary,
                                                                                   body limit, validation)
   ◄──── text/event-stream: text, tool_call, ui, tool_result, confirm, error, done ────┐   │
                                                                                      │   ▼
                                                api/chat.go serveChat: availability, Check, chat_busy, streamTurn
                                                                                      │   ▼
                                       chat.Run: instructions, catalogue, loop ──► llm.Provider.Complete ──► provider
                                                  │
                                                  └─► tools.Tool.Call ─► apigen client ─► chat.Loopback
                                                          ─► the server's root handler again: request id, log,
                                                             session + X-Cowork-Agent, CSRF, boundary, handler, audit
```

## Where it lives

| Package, file | Responsibility |
|---|---|
| [`api/chat.yaml`](../../backend/api/chat.yaml), the `Chat…` schemas of [`components/schemas.yaml`](../../backend/api/components/schemas.yaml) | `getChatAvailability` and `runChatTurn`; the turn's body (`ChatTurn`: the conversation's id, the messages, the page, the decisions) and the data of every event; the contract of the stream in prose |
| [`internal/api/chat.go`](../../backend/internal/api/chat.go) | `ChatOptions`; `GetChatAvailability` and `chatAvailability` — inside, or the tenant's consent to the configured fingerprint; `serveChat` — the turn after the pipeline; `startTurn`, the count of a person's turns; `streamTurn` — the headers, the keep-alive, the availability asked again before every call of the model (`chat.Options.Allowed`), the shutdown, `error` and `done`; `chatTurn` — the session cookie, the page, the loopback, the mark, the person; `turnProblem`; `eventWriter` |
| [`internal/api/api.go`](../../backend/internal/api/api.go) `serveOperation` | sends `runChatTurn`, after the timeout bounded reading its body, the body limit and the validation, to `serveChat` with a context the request timeout does not cut |
| [`internal/api/tenants.go`](../../backend/internal/api/tenants.go) | the consent: `chat_external_allowed` with the fingerprint it was given to (`consented`, `consentRules`); switching it on takes a session |
| [`internal/chat/chat.go`](../../backend/internal/chat/chat.go) | `Run` and the loop (`runner`): `split` — the conversation into what the model reads and the calls that wait —, `decide`, `runCall` with the derived keys (`keys`), `answer`, `calls` — the model's calls as the conversation keeps them —, `tainted`, `clipText` |
| [`internal/chat/confirm.go`](../../backend/internal/chat/confirm.go) | `policies`, the policy of every tool; `review`, which decides whether a call waits and pins what it read (`pin`); the proposals' words (`reviewMove`, `reviewStages`, `reviewDecision`, `describeWrite`) made plain (`plain`, `quote`) |
| [`internal/chat/check.go`](../../backend/internal/chat/check.go) | `Check`: the conversation and the decisions a turn may send |
| [`internal/chat/loopback.go`](../../backend/internal/chat/loopback.go) | `Loopback` — the tool calls to the server's own handler, in the turn's tenant only, the confidential flag noticed —, `Editor` (the cookie, the CSRF pair, the mark, `+confirmed`), `Mark`, `NewSession` |
| [`internal/chat/prompt.go`](../../backend/internal/chat/prompt.go), [`ui.go`](../../backend/internal/chat/ui.go) | the instructions of a turn; `open_ticket`, `open_backlog`, `open_board` |
| [`internal/llm/llm.go`](../../backend/internal/llm/llm.go) | `Provider`, the neutral `Request`, `Message`, `ToolCall`, `Tool`, `Response`; `Config`, `New`; `Error` with its kinds |
| [`internal/llm/client.go`](../../backend/internal/llm/client.go), [`openai.go`](../../backend/internal/llm/openai.go), [`anthropic.go`](../../backend/internal/llm/anthropic.go), [`think.go`](../../backend/internal/llm/think.go) | the HTTP client and the bounds, the two wire formats, the filter of a reasoning model's `<think>` |
| [`internal/config/chat.go`](../../backend/internal/config/chat.go) | the `COWORK_CHAT_*` variables, the rule of the URL (`checkChatURL`, `ownNetwork`), `Chat.Fingerprint` |
| [`cmd/cowork/main.go`](../../backend/cmd/cowork/main.go) `chatOf` | the gateway, the start's log line, `api.ChatOptions` with the signal context as `Shutdown` and the root handler, built after the API, as `Loopback` |
| [`test/stubllm`](../../backend/test/stubllm/stubllm.go) | the provider of the tests |
| [`frontend/src/app/core/chat.service.ts`](../../frontend/src/app/core/chat.service.ts) | `ChatService` — the availability, the conversation per tenant, one turn at a time —, `TurnRecord`, `pageContext`, `navigable`, `CHAT_FETCH` |
| [`frontend/src/app/core/chat-stream.ts`](../../frontend/src/app/core/chat-stream.ts) | `EventStreamParser`, `chatEvent`, `chatEvents`: the stream of a `fetch` read as it arrives |
| [`frontend/src/app/layout/chat-panel.ts`](../../frontend/src/app/layout/chat-panel.ts), `.html`, `.scss` | `ChatPanel`; the toggle, the hint for an administrator and the deferred panel in [`shell.html`](../../frontend/src/app/layout/shell.html) |
| [`frontend/src/app/features/tenant/tenant-settings.ts`](../../frontend/src/app/features/tenant/tenant-settings.ts) | the consent switch, shown to the tenant's administrators for a provider outside |
| [`frontend/nginx/default.conf.template`](../../frontend/nginx/default.conf.template), [`frontend/angular.json`](../../frontend/angular.json) | the shell's policy; `"inlineCritical": false` |

## A turn

1. **The pipeline** treats `POST …/chat` as a session-only write: authentication by the cookie (a
   token is `403 session_required`), the session rules — the CSRF check, the agent header refused
   on what only a session does, a temporary password —, the tenant boundary, the request timeout
   while the body is read, `COWORK_MAX_JSON_BODY`, validation against `ChatTurn`
   ([api.md](api.md#the-pipeline)). Then `serveOperation` hands the request to `serveChat` with the
   context from before the request timeout.
2. **`serveChat`** authorizes the reader role, asks the availability (`409 chat_unavailable`),
   decodes the body, holds the conversation to `Check` — it begins with the person's message; the
   model's messages carry text or calls with ids of their own; a tool's message answers a call of
   the model's message before it; it ends with the person's message, a tool's answer, or calls that
   wait; a decision names a call that waits, once (`400 validation_failed` naming the message) —,
   builds the turn (`chatTurn`) and counts the person's turn (`429 chat_busy` beyond
   `COWORK_CHAT_TURNS_PER_PERSON` on this replica). Everything before this point is a problem
   answer.
3. **`streamTurn`** answers `200 text/event-stream` with `X-Accel-Buffering: no`, starts the
   keep-alive (`: keep-alive` after ten seconds without a write; a goroutine of its own, under the
   writer's lock), bounds the turn by `COWORK_CHAT_TURN_TIMEOUT` and by the shutdown, and runs
   `chat.Run`. Whatever ends the turn, `done` is the last event, after an `error` event when it
   failed — `turnProblem` maps the provider's error to `chat_provider_failed` with the gateway's
   sentence and logs the clip, the timeout to `timeout`, the shutdown to `not_ready`, a withdrawn
   consent to `chat_unavailable`, anything else to `internal`.
4. **`chat.Run`** writes the instructions (`system`), builds the catalogue — the shared tools of
   `tools.Catalogue(tools.Anywhere)` and the three page tools, each offered only when `policies`
   names it and does not leave it out — and splits the conversation: a call that waited when the
   person wrote a new message instead is answered as skipped where it stands, and the calls of the
   model's last message without an answer wait.
5. **`decide`** answers the waiting calls: the first runs when the turn's decision says so and is
   answered "skipped by the person" otherwise; each later one runs unless the review holds it, and
   then it is answered that it did not run — the person decides one call at a time.
6. **The loop**, up to `COWORK_CHAT_MAX_STEPS` calls of the model: ask `Allowed`, call
   `Provider.Complete` with the instructions, the history and the tools — the text streams out as
   `text` events —, keep the answer (`calls` gives every call an id of its own, drops more than 32,
   and keeps arguments that are no object or too long apart, answered with why), and for each call
   `review` it: a call that waits pins what the review read into its arguments, is sent as
   `tool_call` and `confirm`, and ends the turn as `confirm`; any other is sent as `tool_call`, run
   (`runCall` → `Tool.Call`) and answered as `tool_result`. An answer without calls ends the turn as
   `answered`.
7. **`done`** carries the messages the turn added, in the neutral model — the browser appends them
   as they are and sends the whole conversation with the next turn. The backend keeps nothing.

A tool's refusal is an answer the model reads, not a failed turn. A tool's answer reaches the model
clipped to 16,000 characters and the person to 2,000 (`clipText` says how much was left out).

## The loopback

A tool call is a request of the API, built by the generated client (`apigen.ClientWithResponses`
over `chat.Loopback` as its `HttpRequestDoer`) and served by the server's own root handler in the
same process (`tools.HandlerDoer`) — the handler `httpserver.New` builds, so the call gets a request
id of its own, a line in the request log and the panic recovery before the API pipeline runs it.
`runServe` builds that handler after the API it wraps, so `ChatOptions.Loopback` is a function read
at the first turn; without it — in a test that builds the API alone — the calls go to the API's
handler.

- **The request** carries the person's session cookie, `Origin: COWORK_BASE_URL`,
  `X-Requested-With: cowork`, `X-Cowork-Agent: chat/<model>/<conversation>` — `+confirmed` after the
  conversation for a call the person decided — and `User-Agent: cowork-chat` (`Editor`); the
  `Idempotency-Key` of a creating `POST` is the tool's, which `runCall` makes derive from the
  conversation's id, the call's id and a counter (`keys`). Its client address is the turn's: the
  loopback copies the turn request's peer and `X-Forwarded-For`, so the source hash of the acts is
  the person's.
- **The context** ends when the turn's does and carries its deadline, so a turn that runs out of time
  reads as a timeout in the call too; it carries none of the turn request's values — the pipeline
  authenticates and admits the call anew (`detached`).
- **What it refuses** before anything is served (`allowed`): a path that is not clean, the chat route
  itself (a turn never starts another), the event stream (a recorder would buffer it forever), and
  any path outside `/api/v1/tenants/<the turn's tenant>` and `/api/v1/tickets/<the turn's tenant>/`
  — the person's other tenants and `/api/v1/me` included. `NewSession` binds the tools to the page's
  project or to the tenant, confines a search of every tenant to the turn's (`Session.Tenants`),
  names the person (`Session.Person`, so `open_question` asks `me` without `/api/v1/me`) and assumes
  the token — an agent with every capability — so no tool reads `/api/v1/me/token`.
- **What it notices**: an answer whose body says `"confidential": true` sets the turn's flag, which
  `runCall` turns into the note at the start of the tool's answer — the hold of
  [The policies](#the-policies-and-the-decisions).

`HandlerDoer` keeps the whole answer in memory before the tool reads it; the bounds are the API's —
the page size, the body of a ticket — and the clip of what the model reads.

## The policies and the decisions

`policies` in [`confirm.go`](../../backend/internal/chat/confirm.go) names every tool the chat may
meet, and a tool it does not name is not offered (`TestEveryToolIsClassified` fails while the shared
catalogue holds one it does not name):

| Policy | Tools | `review` |
|---|---|---|
| `read` | `get_ticket`, `search`, `open_ticket`, `open_backlog`, `open_board` | runs at once |
| `write` | `file_ticket`, `record_state`, `open_question`, `comment`, `link`, `watch`, `set_urgency` | waits once the conversation is tainted, with `describeWrite`'s words |
| `decision` | `finish_work`, `record_answer`, `create_project` | always waits; `finish_work` pins `from` |
| `move` | `transition` | waits for `done`, `dropped`, `blocked`, `decided` without a read; reads the ticket and waits for a backward move, a reopen, a withdrawal of a done, a ticket it cannot read, and for any move once tainted; pins `from` |
| `stages` | `set_progress` | reads the ticket; waits for the write that closes or reopens, for a ticket it cannot read, and once tainted; pins `version` |
| `left` | `session_start`, `api` | not offered |

A call the tool would refuse before it acts — an unknown tool, arguments its schema refuses — is not
held: it runs and is refused. The words of a proposal quote the model and other people only through
`plain`, which makes every control, format and line-separating character a space and every quote
mark an apostrophe, and cuts at the length given.

### Adding a tool to the chat

1. A shared tool comes first, as [mcp.md](mcp.md#adding-a-tool) says — the chat runs what the MCP
   server runs. A tool that only makes sense in the browser is the chat's own, in
   [`ui.go`](../../backend/internal/chat/ui.go) through `tools.Define`.
2. Give it a policy in `policies`. A read is `read`. A write that is reversible and needs no person's
   reason is `write`, and gets its words in `describeWrite` for the hold. An act a person owes a
   reason, a verification note or a decision for is `decision` — or a review of its own, like
   `reviewMove`, that waits for exactly those calls, fails closed where it cannot read what decides,
   and pins what it read as the tool's precondition (`from`, `version`), which the shared tool then
   takes as an optional argument. A tool a model reading injected text must not have is `left`.
3. A page tool sends a `ui` event only after the API answered that the person may see the page, and
   only for a path the frontend's `navigable` accepts — extend `navigable` with it, or the panel
   shows the call as not opened.
4. When the policy changes what waits, the instructions in [`prompt.go`](../../backend/internal/chat/prompt.go)
   name it, so the model makes the call and leaves the rest to the person.
5. Tests: a case of `TestProposals` or a test of its own in
   [`chat_test.go`](../../backend/internal/chat/chat_test.go) for what waits and what it pins, and a
   turn through the whole server in [`api_chat_test.go`](../../backend/test/integration/api_chat_test.go)
   when it writes.
6. The table of ADR 0076 D2 and of [docs/security/chat.md](../security/chat.md#what-waits-for-the-person).

## The gateway

`llm.Provider` has one method, `Complete(ctx, Request, onText)`: the instructions, the messages of
the neutral model, the tools with their JSON Schemas; `onText` receives the answer's text piece by
piece as it streams, and the `Response` is the whole answer — its text, its tool calls put together
from their fragments, why it ended, what the provider counted. `New` builds the adapter of the
configured wire format:

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
the provider's message with the key taken out, for the log only. `thinkFilter` takes
`<think>…</think>` out of the text in whatever cut the pieces arrive.

**Adding a wire format** is a type implementing `Provider` in `internal/llm`, a case in `New`, the
value in `config/chat.go` and in the chart's `cowork.chatEnabled`, the format in `test/stubllm`, and
the tests of [Testing](#tests) for it.

## The stream and the panel

The backend writes each event as `event: <name>` and `data: <json>` and flushes it; the data are the
generated types `ChatTextEvent`, `ChatToolCall`, `ChatUiEvent`, `ChatToolResultEvent`,
`ChatConfirmEvent`, `Problem` and `ChatDoneEvent`. The browser reads it with `fetch`: the answer of
a `POST` is a stream, which neither the `HttpClient` (it waits for the whole body) nor `EventSource`
(`GET` only) reads as it arrives. [`chat-stream.ts`](../../frontend/src/app/core/chat-stream.ts) cuts
the bytes into events by the HTML standard's event-stream format — a character split between two
pieces waits for its rest, comments are read past — and `chatEvent` holds each event's data to the
shape the document gives it, reading past one it does not know.

[`ChatService`](../../frontend/src/app/core/chat.service.ts) holds the tenant's availability (a
`resource` over `GET …/chat`), one conversation per tenant — cleared when another tenant's pages
open, never written to storage —, and one turn at a time. `send` and `decide` post the whole
conversation with the page (`pageContext`: the path, the project's key, the ticket's short key, each
in the shape the document allows), the conversation's id and the decision, with
`X-Requested-With: cowork` and the session's cookie. `done` appends its messages; a turn that ends
without `done` — Stop, a cut connection — is written down by `TurnRecord` from the events it saw, so
the model learns next time what happened: the calls that reported a result with their summary, a
proposal left waiting, a call without a result left out. A `ui` event navigates only when `navigable`
accepts its path. Whether the panel is open is `cowork.chat.<person id>` in `localStorage`, every
access in `try`.

[`ChatPanel`](../../frontend/src/app/layout/chat-panel.ts) shows the conversation as text — Angular's
interpolation, never `innerHTML`, no Markdown —, a call as a card with its name, its arguments as
folded JSON text, the start of its answer and its state, and Run and Skip on a proposal, and beneath
the answer of a turn that ended `answered` without calling, proposing or answering any tool a note
that nothing in it was looked up (`noTools`, from `TurnRecord.called`); Stop aborts the `fetch`. The shell shows the toggle while the tenant's chat is available — an administrator of a
tenant that has not allowed a provider outside sees a hint that leads to the settings instead — and
loads the panel's code with `@defer` once it is. The panel takes 24rem beside the content and lies
over it on a window narrower than 64rem (`overlayQuery` in [`shell.ts`](../../frontend/src/app/layout/shell.ts)),
where Escape and the focus moving into the page beneath close it.

## The content-security policy

The template keeps the policy in one nginx variable, `$ui_csp`, and adds it with `always` in each of
the three locations that serve the UI — the icons, the hashed bundles, `index.html` — because an
`add_header` inside a location replaces the server's
([trust-boundaries.md](../security/trust-boundaries.md#the-shells-content-security-policy)). Under
`script-src 'self'` the production build inlines no critical CSS (`"inlineCritical": false` in
`angular.json`): the inliner's loader is an inline `onload` handler. A change to the template or to
the build is checked by running the built image in a browser and watching the console for a
violation; nginx has no unit test ([build-test-lint.md](build-test-lint.md#run-the-images-together)).

## Tests

- **Unit, the loop** ([`chat_test.go`](../../backend/internal/chat/chat_test.go)), against a scripted
  model (`model`, an `llm.Provider`) and a fake API behind the real loopback (`api`): the tool rounds (`TestATurnRunsTheToolsTheModelCalls`),
  the step limit, a confirmation and a skip, the calls after a proposal, the page tools, what a model
  gets wrong, a failed turn, the loopback's refusals, the mark, `Check`, the clip, the proposals and
  their words (`TestProposals`, `TestADescriptionIsPlain`), the review failing closed, every tool
  classified, a question asked of `me`, the hold after a confidential ticket
  (`TestAConfidentialTicketTaintsTheConversation`), a proposal pinned to what it read, a decided call
  marked and replayed by its keys, the consent asked before every call, arguments too long.
- **Unit, the gateway** ([`llm_test.go`](../../backend/internal/llm/llm_test.go)), against
  [`test/stubllm`](../../backend/test/stubllm/stubllm.go) on `httptest`'s loopback listener: both
  formats streamed and not, no key no header, a refusal that never echoes the provider, no redirect,
  an answer it cannot read, a silent or absent provider, the think filter, the bounds of an answer
  and `max_tokens` in every request, an answer of white space, `Head` and `Cut`.
- **Unit, the rest**: the variables ([`config/chat_test.go`](../../backend/internal/config/chat_test.go)),
  the turn held to the document and the availability's shape, the consent's fingerprint, the turns
  per person ([`api/chat_test.go`](../../backend/internal/api/chat_test.go)), the agent header on a
  session ([`api/session_test.go`](../../backend/internal/api/session_test.go)), and the shared
  tools' new arguments in [`tools/`](../../backend/internal/tools/) (`TestSetUrgency`,
  `TestPreconditionsTheCallerRead`, `TestAHostThatKnowsThePerson`).
- **Integration** ([`api_chat_test.go`](../../backend/test/integration/api_chat_test.go)), through the
  whole server against [`test/stubllm`](../../backend/test/stubllm/stubllm.go) — no real model:
  the availability inside and outside and the consent in a session; a turn that files a ticket and
  ranks it to `now`, its acts the person's with the chat's mark and a key; a confirmation round over
  the Anthropic format; a token, a CSRF failure and an agent-marked session refused; a turn that stays
  in its tenant; the turn's time, its keep-alive and a failing provider; the agent header on a
  session; the turn limit and the shutdown; a confidential ticket that holds the writes and a Run that
  replays; a question asked of the person.
- **Frontend** (vitest on jsdom): [`chat-stream.spec.ts`](../../frontend/src/app/core/chat-stream.spec.ts),
  [`chat.service.spec.ts`](../../frontend/src/app/core/chat.service.spec.ts),
  [`chat-panel.spec.ts`](../../frontend/src/app/layout/chat-panel.spec.ts), and the chat's parts of
  `shell.spec.ts` and `tenant-settings.spec.ts`.

Response validation does not reach a turn: its answer is a stream, not a response of the generated
server ([testing.md](testing.md#response-validation)). A real model is checked by hand
([docs/operations/chat.md](../operations/chat.md#lm-studio-on-the-operators-machine)); the end-to-end
tier, which would hold a conversation against the stub, does not exist.
