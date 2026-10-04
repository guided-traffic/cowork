# The chat in the UI

How an installation gives its people the assistant at the right edge of the UI: the backend calls a
model the operator names, and the model works in the tenant through the API as the person's agent
([ADR 0076](../adr/0076-the-chat-in-the-ui-runs-its-loop-in-the-backend-as-an-agent-of-the-person.md),
provisional: the owner's review is pending). Without a provider there is no chat and nothing changes.
The variables are [README.md, Configuration](../../README.md#configuration), the chart's values
[README.md, Helm chart values](../../README.md#helm-chart-values); what the chat may do and what it
leaves open is [docs/security/chat.md](../security/chat.md).

```
browser ── POST /api/v1/tenants/<slug>/chat (the session) ──► frontend nginx ──► backend ──► the provider
        ◄── text/event-stream: text, tool calls, results ───────────────────────┘   │        (COWORK_CHAT_URL)
                                                                                     └─► the tools: the API
                                                                                         in the same process
```

## What the chat needs

- **A provider** that speaks one of two wire formats — `openai`, OpenAI Chat Completions, which
  LM Studio, OpenAI, Ollama and vLLM serve, or `anthropic`, the Anthropic Messages API — and a model
  of it that calls tools.
- **The backend's pods reach it.** The browser never talks to the provider; the backend calls it
  for every step of a turn.
- **A browser session.** A turn is a person's in a session: a login and `COWORK_BASE_URL`
  ([installation.md](installation.md#the-local-administrator)).
- **A decision about inside or outside** — the next section.

## Inside or outside the installation

`COWORK_CHAT_INSIDE` (`chat.inside`) is the operator's statement about where the model runs.

- **`true` — inside.** Every tenant has the chat at once, and no tenant is asked. Set it only for a
  model on machines the installation's operators run — LM Studio on the operator's machine, a model
  server in the cluster. Nothing checks the statement: a hosted provider declared inside receives
  what the chat reads in every tenant.
- **`false` — outside, the default.** A tenant has the chat only once one of its administrators
  allows the provider in the tenant's **Settings** — the switch names the model and says that the
  tenant's text then leaves the installation. Switching it on takes a browser session; it is recorded
  as the tenant's `updated` act; an `admin`-scope token may switch it off. The consent is given to the
  provider as configured — its wire format, the host and port of its URL, and the model — and a
  change of any of the three in the configuration leaves every tenant without the chat until its
  administrators allow the new provider. Until then an administrator sees an assistant icon in the
  top bar that says so and leads to the settings; the other members see no assistant.

Members see on the assistant's empty page which model it uses and, for a provider outside, that what
the assistant reads is sent to it. A confidential ticket is sent too
([H-37](../security/chat.md#h-37)).

## LM Studio, on the operator's machine

LM Studio serves the OpenAI format on `http://localhost:1234/v1`.

```bash
COWORK_CHAT_PROVIDER=openai
COWORK_CHAT_URL=http://localhost:1234/v1          # the gateway appends /chat/completions
COWORK_CHAT_MODEL=qwen/qwen3-30b-a3b-2507          # example: the id GET /v1/models lists
COWORK_CHAT_INSIDE=true                             # the operator's machine: every tenant has the chat
# COWORK_CHAT_API_KEY=…                             # only when LM Studio is set to require an API token
```

**Load the model with a large context.** LM Studio loads a model on its first use with a context of
4096 tokens, and the chat's instructions and tool descriptions alone take more: the turn ends with
`chat_provider_failed`, and the backend's log line `the chat's provider failed` carries LM Studio's
message, *The number of tokens to keep from the initial prompt is greater than the context length*.
Load the model before the first turn with a context of 16k tokens or more — the check below ran with
32k — or set that context as the model's default in LM Studio. The context is shared among the
predictions the model runs at once: loaded with 32k and `--parallel 4`, the same message came back on
2026-10-04, and with `--parallel 1` it went away. Load it with one prediction, or with a context
that leaves 16k to each:

```bash
lms load qwen/qwen3-30b-a3b-2507 --context-length 32768 --parallel 1
```

**The model must call tools.** An instruct model with tool calling works; a model that answers in
prose only never acts. A reasoning model's `<think>…</think>` in the answer is taken out before the
person sees it.

**Verified on 2026-10-04** against LM Studio with `qwen/qwen3-30b-a3b-2507` loaded with 32k: in the
UI's chat the model filed a ticket, set its urgency to `now` and moved it to `analysed`; the open
board showed the card at once, and the ticket's activity records every act as
`chat/qwen:qwen3-30b-a3b-2507/<conversation>`.

`make dev` uses LM Studio by itself when it answers on `:1234` and lists the model
`COWORK_DEV_CHAT_MODEL` (`qwen/qwen3-30b-a3b-2507` `# default`), with the provider declared inside;
without it, `make dev` runs without the chat and says so.

**LM Studio on another machine of the network** is reached by its address or name:
`http://192.168.1.20:1234/v1` `# example` is accepted, because plain `http://` is allowed on a
private address and on a name of the operator's network
([H-41](../security/chat.md#h-41)); LM Studio has to serve on the local network for it, and from a
cluster the egress has to reach it ([in the chart](#in-the-chart)).

## OpenAI, and the servers that speak its format

```bash
COWORK_CHAT_PROVIDER=openai
COWORK_CHAT_URL=https://api.openai.com/v1          # example; Ollama: http://ollama.ai.svc:11434/v1, vLLM likewise: the base ends in /v1
COWORK_CHAT_API_KEY=…                               # from a Secret; sent as Authorization: Bearer
COWORK_CHAT_MODEL=gpt-4.1                           # example
```

A hosted provider is outside: leave `COWORK_CHAT_INSIDE` unset, and each tenant's administrators
decide. The gateway sends `max_tokens: 4096` with every call; OpenAI's reasoning models (the
o-series) want `max_completion_tokens` instead — a known limit of the adapter: pick a model that
takes `max_tokens`. **Not verified:** the gateway has been tested against the stub provider of the
tests and against LM Studio, never against OpenAI's API, Ollama or vLLM.

## Anthropic

```bash
COWORK_CHAT_PROVIDER=anthropic
COWORK_CHAT_URL=https://api.anthropic.com           # the gateway appends /v1/messages
COWORK_CHAT_API_KEY=…                               # required; from a Secret; sent as x-api-key
COWORK_CHAT_MODEL=claude-sonnet-4-5                 # example
```

The gateway speaks the Messages API with `anthropic-version: 2023-06-01` and asks for at most 4096
tokens an answer. A provider outside, as above. **Not verified:** no request reached Anthropic's API;
the format is tested against the stub provider.

## In the chart

The key comes from a Secret of yours, never from the values
([ADR 0058](../adr/0058-postgresql-and-object-storage-are-external-the-chart-takes-references-with-configurable-keys.md) D3):

```bash
kubectl -n cowork create secret generic cowork-chat --from-literal=apiKey='CHANGE-ME'
helm upgrade cowork cowork/cowork -n cowork --reuse-values \
  --set chat.provider=anthropic \
  --set chat.url=https://api.anthropic.com \
  --set chat.model=claude-sonnet-4-5 \
  --set chat.existingSecret=cowork-chat
```

Every value above is an example. The `chat` block is in
[README.md, Helm chart values](../../README.md#helm-chart-values): `provider` turns the chat on, and
emptying it alone turns it off — the other values are rendered only with it. Rendering fails,
naming the value, with a provider that is neither `openai` nor `anthropic`, without `chat.url` or
`chat.model`, with `anthropic` and no `chat.existingSecret`, and with a `chat.inside` that is not the
boolean `true` or `false` (a quoted `"false"` is refused, because the notes read it as true). The
notes say whether the chat is on, which model, where the key comes from, and warn while the provider
counts as outside. `COWORK_CHAT_TURNS_PER_PERSON` has no value of the chart: set it through
`backend.extraEnv`, and only together with `chat.provider` — without the provider the backend refuses
to start on it.

**The NetworkPolicy restricts no egress.** The chart's policy admits the frontend's pods to the
backend and nothing else coming in; what the backend reaches is not limited, and it reaches the
provider directly. A namespace that blocks egress by a policy of its own must admit the backend's
pods to the provider's address and port and to DNS.

**Behind an Ingress, a turn is a stream.** A turn is a `POST` answered with `text/event-stream`,
which lasts up to `COWORK_CHAT_TURN_TIMEOUT` and stays open through a model's long silences by a
`: keep-alive` comment every ten seconds. The frontend's nginx passes it unbuffered, because the
backend answers `X-Accel-Buffering: no`, and its read timeout for `/api/` — `requestTimeout` plus ten
seconds — stays above the comments' interval. An Ingress or a load balancer in front must not buffer
it either: the annotations of [runtime.md, behind an Ingress](runtime.md#behind-an-ingress) for the
event stream serve the chat too. A proxy that buffers shows as answers that arrive whole at the end
of a turn, or not at all; one that closes a response after a few seconds of silence cuts turns.

## Limits and load

| Variable | Default | Bounds | `0` |
|---|---|---|---|
| `COWORK_CHAT_TURN_TIMEOUT` | `5m` | one turn — the model's calls and the tools'; past it the turn ends with the `error` event `timeout` | no limit |
| `COWORK_CHAT_MAX_STEPS` | `8` | the calls of the model in one turn; the turn then ends and the panel asks the person to write to go on | no limit |
| `COWORK_CHAT_TURNS_PER_PERSON` | `2` | the turns one person runs at once on one replica — two tabs, say; one more is `429 chat_busy` | no limit |

A turn holds one connection to the browser and, while the model writes, one to the provider; every
tool call is a request of its own inside the backend, with a database transaction of its own. The
body of a turn — the whole conversation — is held to `COWORK_MAX_JSON_BODY`, and the conversation's
shape to fixed bounds (400 messages, 100,000 characters a text); a conversation past them is refused
with `400 validation_failed`, and the person begins a new one. There is no budget of turns or tokens:
a paid provider's spending limit is the provider's to set ([H-43](../security/chat.md#h-43)).

## What the operator sees

| Log line | Level | Means |
|---|---|---|
| `the chat talks to a model` | info | at start: the provider's wire format, the model, whether it is declared inside, the limits — never the URL or the key |
| `the chat's provider is outside the installation: a tenant has the chat once its administrators allow it` | info | at start, with `COWORK_CHAT_INSIDE` false |
| `the chat's provider failed` | warn | a turn's call of the model failed: `kind` (`unreachable`, `refused`, `malformed`), the provider's `status`, and `answer`, at most 300 characters of its message with the configured key replaced by `[key]` ([H-42](../security/chat.md#h-42)) |
| `a turn of the chat failed` | error | anything else failed in a turn; the person saw `internal` with the request id |

The request log has a turn as one line, `POST /api/v1/tenants/<slug>/chat`, written when the turn
ends, with its whole duration; and every tool call of it as a line of its own, under a request id of
its own — the calls run through the whole server. No line carries a message, an instruction or a
tool's answer. The tenant's audit view shows the chat's acts with the agent
`chat/<model>/<conversation>`, and `…+confirmed` for an act the person ran from a proposal.

## When something does not work

| Symptom | Check |
|---|---|
| No assistant in the top bar | `GET /api/v1/tenants/<slug>/chat` answers `reason`: `not_configured` — no `COWORK_CHAT_PROVIDER` reached the backend; `not_allowed_in_tenant` — the provider counts as outside and the tenant has not allowed it, or allowed another model or host |
| The first message ends with *Chat provider failed* | the log line `the chat's provider failed`: `unreachable` — the URL, DNS, an egress policy; `refused` with `401`/`403` — the key; with `404` — the URL (for `openai` it ends in `/v1`, for `anthropic` it has none) or the model's name; LM Studio's message about the context length — load the model with a larger context, or fewer parallel predictions (`lms ps` shows both, [above](#lm-studio-on-the-operators-machine)) |
| The text arrives all at once at the end, or turns break off | a proxy in front buffers or cuts the stream ([in the chart](#in-the-chart)) |
| *Timeout* after minutes | the model is slow or a tool call hangs: `COWORK_CHAT_TURN_TIMEOUT` |
| *The assistant took as many steps as one turn allows* | `COWORK_CHAT_MAX_STEPS`; writing a new message goes on |
| `429 chat_busy` | the person runs as many turns as `COWORK_CHAT_TURNS_PER_PERSON` allows, in another tab perhaps |
| `409 chat_unavailable` while a conversation runs | the tenant's consent was withdrawn, or the provider in the configuration changed |
| The assistant says it did something the cards do not show | the cards are what happened ([H-44](../security/chat.md#h-44)); the ticket's activity confirms |
| An answer is marked *No tool was called for this answer* | the model answered from what it assumes, without reading cowork; ask again naming what to look up, or use a model that calls tools more readily |

Not verified, and this is the gap: the chat with a provider other than LM Studio, and the chat
behind a real Ingress controller.
