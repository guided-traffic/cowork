# The chat in the UI

How an installation gives its people the assistant at the right edge of the UI: the backend calls a
model of a provider the operator lists, the person picks one of them in the panel, and the model
works in the tenant through the API as the person's agent, with the capabilities the person chose
([ADR 0076](../adr/0076-the-chat-in-the-ui-runs-its-loop-in-the-backend-as-an-agent-of-the-person.md)).
Without a provider there is no chat and nothing changes. The variables are
[README.md, Configuration](../../README.md#configuration), the chart's values
[README.md, Helm chart values](../../README.md#helm-chart-values); what the chat may do and what it
leaves open is [docs/security/chat.md](../security/chat.md).

```
browser ── POST /api/v1/tenants/<slug>/chat (the session, a provider id) ──► the Ingress ──► backend ──► the provider picked
        ◄── text/event-stream: text, tool calls, results ────────────────────────────────┘   │        (COWORK_CHAT_<ID>_URL)
        ── DELETE /api/v1/tenants/<slug>/chat/turns (Stop) ──────────────────────────────────►│
                                                                                              └─► the tools: the API
                                                                                                  in the same process
```

## What the chat needs

- **One provider or more** that speak one of two wire formats — `openai`, OpenAI Chat Completions,
  which LM Studio, OpenAI, Ollama and vLLM serve, or `anthropic`, the Anthropic Messages API — and a
  model of each that calls tools.
- **The backend's pods reach them.** The browser never talks to a provider; the backend calls the
  picked one for every step of a turn.
- **A browser session.** A turn is a person's in a session: a login and `COWORK_BASE_URL`
  ([installation.md](installation.md#the-local-administrator)).

Every member of every tenant has the chat once a provider is configured; no tenant is asked.

## Providers

`COWORK_CHAT_PROVIDERS` lists the providers by id, in order — the first is what a turn uses when the
person picked none — and each id has variables of its own, `COWORK_CHAT_` with the id upper-cased and
its dashes as underscores:

| Variable | Required | Means |
|---|---|---|
| `COWORK_CHAT_<ID>_NAME` | no — the id when unset | the name the panel shows, 1 to 64 characters without control characters |
| `COWORK_CHAT_<ID>_KIND` | yes | `openai` or `anthropic` |
| `COWORK_CHAT_<ID>_URL` | yes | the base URL: `/chat/completions` is appended for `openai` (so it ends in `/v1`), `/v1/messages` for `anthropic`; `https://`, or `http://` on the operator's network only ([H-41](../security/chat.md#h-41)) |
| `COWORK_CHAT_<ID>_MODEL` | yes | the model by the name its provider knows it |
| `COWORK_CHAT_<ID>_API_KEY` | for `anthropic` | the key; `Authorization: Bearer` for `openai`, `x-api-key` for `anthropic` |

An id is 1 to 32 lowercase letters, digits and dashes, a dash neither first nor last, named once:
`lmstudio`, `claude-work` (`COWORK_CHAT_CLAUDE_WORK_URL`). The start refuses a missing or wrong
variable and names it, never quoting a URL or a key; it logs one line per provider with its id, kind
and model. Where the installation lists more than one, the panel's header offers them by name, and
the person's pick is kept in that browser.

### Adding a hosted provider

**A provider receives everything the chat reads for its person, in every tenant of the installation,
confidential tickets included.** That is the owner's decision of 2026-10-04 with its risk accepted
([H-37](../security/chat.md#h-37)): there is no tenant consent and no inside or outside, and listing a
provider is the only gate. Before adding a hosted provider — OpenAI, Anthropic, any model server
outside the machines the installation's operators run — make sure every client tenant's data,
confidential security findings among it, may go to that provider under its terms, retention and
region. A model on the operator's machine or in the cluster sends nothing beyond them.

## LM Studio, on the operator's machine

LM Studio serves the OpenAI format on `http://localhost:1234/v1`.

```bash
COWORK_CHAT_PROVIDERS=lmstudio
COWORK_CHAT_LMSTUDIO_NAME="LM Studio"                      # example
COWORK_CHAT_LMSTUDIO_KIND=openai
COWORK_CHAT_LMSTUDIO_URL=http://localhost:1234/v1          # the gateway appends /chat/completions
COWORK_CHAT_LMSTUDIO_MODEL=qwen/qwen3-30b-a3b-2507          # example: the id GET /v1/models lists
# COWORK_CHAT_LMSTUDIO_API_KEY=…                            # only when LM Studio is set to require an API token
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
prose only never acts. MLX models that call tools here are `qwen/qwen3-30b-a3b-2507` and
`openai/gpt-oss-20b` `# example`. A reasoning model's `<think>…</think>` in the answer is taken out
before the person sees it.

**Verified on 2026-10-04** against LM Studio with `qwen/qwen3-30b-a3b-2507` loaded with 32k, before
the list of providers was built: in the UI's chat the model filed a ticket, set its urgency to `now`
and moved it to `analysed`; the open board showed the card at once, and the ticket's activity records
every act as `chat/qwen:qwen3-30b-a3b-2507/<conversation>`. Not verified: the list of providers, the
person's capabilities and the stop route against LM Studio — the tests run them against the stub
provider of the test tier.

`make dev` uses LM Studio by itself, as the one provider `lmstudio`, when it answers on `:1234` and
lists the model `COWORK_DEV_CHAT_MODEL` (`qwen/qwen3-30b-a3b-2507` `# default`); without it, `make dev`
runs without the chat and says so. It never loads a model: when the `lms` CLI shows the model is not
loaded, it prints the `lms load` command above, because LM Studio would otherwise load it on the
first turn with its own defaults ([`hack/dev.sh`](../../hack/dev.sh)).

**LM Studio on another machine of the network** is reached by its address or name:
`http://192.168.1.20:1234/v1` `# example` is accepted, because plain `http://` is allowed on a
private address and on a name of the operator's network
([H-41](../security/chat.md#h-41)); LM Studio has to serve on the local network for it, and from a
cluster the egress has to reach it ([in the chart](#in-the-chart)).

## OpenAI, and the servers that speak its format

```bash
COWORK_CHAT_PROVIDERS=openai
COWORK_CHAT_OPENAI_NAME=OpenAI                              # example
COWORK_CHAT_OPENAI_KIND=openai
COWORK_CHAT_OPENAI_URL=https://api.openai.com/v1            # example; Ollama: http://ollama.ai.svc:11434/v1, vLLM likewise: the base ends in /v1
COWORK_CHAT_OPENAI_API_KEY=…                                # from a Secret; sent as Authorization: Bearer
COWORK_CHAT_OPENAI_MODEL=gpt-4.1                            # example
```

A hosted provider receives every tenant's text the chat reads ([above](#adding-a-hosted-provider)).
The gateway sends `max_tokens: 4096` with every call; OpenAI's reasoning models (the o-series) want
`max_completion_tokens` instead — a known limit of the adapter: pick a model that takes `max_tokens`.
**Not verified:** the gateway has been tested against the stub provider of the tests and against LM
Studio, never against OpenAI's API, Ollama or vLLM.

## Anthropic

```bash
COWORK_CHAT_PROVIDERS=claude
COWORK_CHAT_CLAUDE_NAME=Claude                              # example
COWORK_CHAT_CLAUDE_KIND=anthropic
COWORK_CHAT_CLAUDE_URL=https://api.anthropic.com            # the gateway appends /v1/messages
COWORK_CHAT_CLAUDE_API_KEY=…                                # required; from a Secret; sent as x-api-key
COWORK_CHAT_CLAUDE_MODEL=claude-sonnet-4-5                  # example
```

The gateway speaks the Messages API with `anthropic-version: 2023-06-01` and asks for at most 4096
tokens an answer. A hosted provider, as above. **Not verified:** no request reached Anthropic's API;
the format is tested against the stub provider.

## In the chart

Each provider is an entry of `chat.providers`; a key comes from a Secret of that provider's own, never
from the values
([ADR 0058](../adr/0058-postgresql-and-object-storage-are-external-the-chart-takes-references-with-configurable-keys.md) D3):

```bash
kubectl -n cowork create secret generic cowork-chat-claude --from-literal=apiKey='CHANGE-ME'
cat > chat.yaml <<'EOF'
chat:
  providers:
    - id: lmstudio
      name: LM Studio
      kind: openai
      url: http://lmstudio.ai.svc:1234/v1
      model: qwen/qwen3-30b-a3b-2507
    - id: claude
      name: Claude
      kind: anthropic
      url: https://api.anthropic.com
      model: claude-sonnet-4-5
      existingSecret: cowork-chat-claude
EOF
helm upgrade cowork cowork/cowork -n cowork --reuse-values -f chat.yaml
```

Every value above is an example. The `chat` block is in
[README.md, Helm chart values](../../README.md#helm-chart-values): `providers` turns the chat on, and
emptying it alone turns it off — the limits are rendered only with a provider. Rendering fails, naming
the entry, with an id that is not one or names two providers, a kind that is neither `openai` nor
`anthropic`, no `url` or `model`, `anthropic` without `existingSecret`, and an `apiKey` in an entry —
the key has no inline value. A Secret's key name is `apiKey` unless the entry's `keys.apiKey` names
another. The notes list the providers with where each key comes from, and warn that every provider
receives what the chat reads in every tenant. `COWORK_CHAT_TURNS_PER_PERSON` has no value of the
chart: set it through `backend.extraEnv`, and only together with a provider — without one the backend
refuses to start on it.

**The chart restricts no egress.** It ships no NetworkPolicy; what the backend reaches is the
cluster's to limit, and it reaches the providers directly. A namespace that blocks egress by a
policy of the cluster's must admit the backend's pods to each provider's address and port and to
DNS.

**Behind an Ingress, a turn is a stream.** A turn is a `POST` answered with `text/event-stream`,
which lasts up to `COWORK_CHAT_TURN_TIMEOUT` and stays open through a model's long silences by a
`: keep-alive` comment every ten seconds. The Ingress routes it to the backend like any `/api/`
path: a controller that honours the backend's `X-Accel-Buffering: no` — nginx does — passes it
unbuffered, and the comments keep it inside any read timeout above ten seconds. A controller that
does not honour the header, or a load balancer in front, must not buffer it either:
what [runtime.md, behind an Ingress](runtime.md#behind-an-ingress) says for the event stream serves
the chat too. A proxy that buffers shows as answers that arrive whole at the end
of a turn, or not at all; one that closes a response after a few seconds of silence cuts turns.

## Stop

The panel's Stop ends a running turn at once: it aborts the turn's request, whose end cancels the
model's request and a tool call in flight, and it calls `DELETE /api/v1/tenants/<slug>/chat/turns`,
which ends every running turn of the person in that tenant on the replica that answers it and answers
`204` once they have ended, or after five seconds. The route covers a proxy that keeps the backend's
request open after the browser aborted it, and the panel's `chat_busy` notice — a turn of the person
runs in another tab — offers it as its Stop. A stopped turn's stream ends with `done` and the reason
`stopped`; its calls that completed before have happened. The route stops only the turns of the
replica it reaches: with one backend replica, the chart's default, that is every turn; with more, a
turn another replica runs ends by its own request's abort or its time ([H-48](../security/chat.md#h-48)).

## Limits and load

| Variable | Default | Bounds | `0` |
|---|---|---|---|
| `COWORK_CHAT_TURN_TIMEOUT` | `5m` | one turn — the model's calls and the tools'; past it the turn ends with the `error` event `timeout` | no limit |
| `COWORK_CHAT_MAX_STEPS` | `8` | the calls of the model in one turn; the turn then ends and the panel asks the person to write to go on | no limit |
| `COWORK_CHAT_TURNS_PER_PERSON` | `2` | the turns one person runs at once on one replica — two tabs, say; one more is `429 chat_busy` | no limit |

A turn holds one connection to the browser and, while the model writes, one to the provider; every
tool call is a request of its own inside the backend, with a database transaction of its own, and
every request a tool call makes reads the person's chat capabilities once more. The body of a turn —
the whole conversation — is held to `COWORK_MAX_JSON_BODY`, and the conversation's shape to fixed
bounds (400 messages, 100,000 characters a text); a conversation past them is refused with `400
validation_failed`, and the person begins a new one. There is no budget of turns or tokens: a paid
provider's spending limit is the provider's to set ([H-43](../security/chat.md#h-43)).

## What the operator sees

| Log line | Level | Means |
|---|---|---|
| `the chat talks to a model` | info | at start, once per provider: its id, kind and model — never the URL or the key |
| `the chat's limits` | info | at start: the turn's timeout, the steps, the turns per person |
| `the chat's provider failed` | warn | a turn's call of the model failed: `kind` (`unreachable`, `refused`, `malformed`), the provider's `status`, and `answer`, at most 300 characters of its message with that provider's key replaced by `[key]` ([H-42](../security/chat.md#h-42)) |
| `a turn of the chat failed` | error | anything else failed in a turn; the person saw `internal` with the request id |
| `reading the chat's capabilities failed` | error | a request the chat's mark carries could not read the person's capabilities; it was answered `internal` |

The request log has a turn as one line, `POST /api/v1/tenants/<slug>/chat`, written when the turn
ends, with its whole duration; every tool call of it as a line of its own, under a request id of its
own — the calls run through the whole server —; and a stop as `DELETE …/chat/turns`. No line carries
a message, an instruction or a tool's answer. The tenant's audit view shows the chat's acts with the
agent `chat/<model>/<conversation>` and the capabilities the chat held; a person's change of the
chat's capabilities is an installation-level act of that person.

## When something does not work

| Symptom | Check |
|---|---|
| No assistant in the top bar | `GET /api/v1/tenants/<slug>/chat` answers `reason`: `not_configured` — no `COWORK_CHAT_PROVIDERS` reached the backend |
| The first message ends with *Chat provider failed* | the log line `the chat's provider failed`: `unreachable` — the URL, DNS, an egress policy; `refused` with `401`/`403` — the key; with `404` — the URL (for `openai` it ends in `/v1`, for `anthropic` it has none) or the model's name; LM Studio's message about the context length — load the model with a larger context, or fewer parallel predictions (`lms ps` shows both, [above](#lm-studio-on-the-operators-machine)) |
| The text arrives all at once at the end, or turns break off | a proxy in front buffers or cuts the stream ([in the chart](#in-the-chart)) |
| *Timeout* after minutes | the model is slow or a tool call hangs: `COWORK_CHAT_TURN_TIMEOUT`; Stop ends it sooner |
| *The assistant took as many steps as one turn allows* | `COWORK_CHAT_MAX_STEPS`; writing a new message goes on |
| `429 chat_busy` | the person runs as many turns as `COWORK_CHAT_TURNS_PER_PERSON` allows, in another tab perhaps; the notice's Stop ends them |
| A tool card says `agent_forbidden: missing capability: …` | the person did not give the chat that capability; *What the assistant may do* in the panel switches it on, or the act stays the person's |
| The assistant still refuses an act after its capability was switched on | it holds to what it said earlier in the conversation — seen on 2026-10-04 with a local model, although each turn tells it the current capabilities; the panel says so after the change and offers a new conversation, which starts clean |
| `400 validation_failed` at `/provider` | the panel sent a provider the installation no longer lists; reloading the page offers the configured ones |
| The assistant says it did something the cards do not show | the cards are what happened ([H-44](../security/chat.md#h-44)); the ticket's activity confirms |
| An answer is marked *No tool was called for this answer* | the model answered from what it assumes, without reading cowork; ask again naming what to look up, or use a model that calls tools more readily |

Not verified, and this is the gap: the chat with a provider other than LM Studio, and the chat
behind a real Ingress controller.
