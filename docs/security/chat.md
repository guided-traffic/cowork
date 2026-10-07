# The assistant in the browser

What the chat in the UI may make cowork do and on whose behalf, what of a tenant reaches a model and
where, how a turn is kept in its tenant, what the person's choice of capabilities bounds, how a turn
is stopped, how the panel shows what a model writes, and what bounds a turn — as built on 2026-10-04
with the owner's answers of that day
([ADR 0076](../adr/0076-the-chat-in-the-ui-runs-its-loop-in-the-backend-as-an-agent-of-the-person.md)).
The agent the person runs on their own machine with a token is [agent-client.md](agent-client.md);
the agent rules both meet are [tokens.md](tokens.md); the policy the shell sends is
[trust-boundaries.md](trust-boundaries.md#the-shells-content-security-policy); running the chat is
[docs/operations/chat.md](../operations/chat.md).

## Who trusts whom

```
 browser — the person's session                backend                                providers
┌───────────────────────────┐  POST …/chat   ┌──────────────────────────────────┐   ┌───────────────┐
│ panel: text only          │ ─────────────► │ turn: session, CSRF, tenant,     │   │ the models the│
│ conversation, per tenant, │  (provider id) │ limits, the provider picked      │   │ operator lists│
│ sent back with each turn  │ ◄───────────── │ loop ─► gateway ───────────────────►│ in the chart  │
│ Stop: abort + DELETE      │  event stream  │  │  instructions, conversation,   │   │ (one per turn)│
│ capabilities: PUT /me/chat│ ─────────────► │  │  tools' answers, its key       │◄──│               │
└───────────────────────────┘                │  └─► tools ─► loopback ─► the API │   └───────────────┘
                                             │      pipeline as the person's    │
                                             │      agent: the person's chosen  │
                                             │      capabilities, the tenant    │
                                             └──────────────────────────────────┘
```

| Party | Trusted for | Not trusted for |
|---|---|---|
| The operator's configuration | which providers exist — their addresses, keys and models (`COWORK_CHAT_PROVIDERS` and `COWORK_CHAT_<ID>_*`, the chart's `chat.providers`) —, taken as given, and with it where every tenant's text may go | — |
| The person | which provider a turn talks to, among those configured; which capabilities the chat holds | the conversation the browser sends back ([H-39](#h-39)) |
| A provider | keeping what a turn sends it, under its own terms | anything it answers: its text is shown as text, its tool calls are requests the API judges |
| The model | nothing | its calls run as an agent's with the person's chosen capabilities and the agent rules ([H-38](#h-38)) |
| The person's browser | the person's own messages and choices | the conversation it sends back: the backend holds its shape to the rules ([`chat.Check`](../../backend/internal/chat/check.go)), not its truth ([H-39](#h-39)) |
| Text in tickets, comments, questions, answers | — | it was written by other people and agents, and it can steer the model ([H-38](#h-38)) |

## What reaches a provider

**What a call of the model carries** ([`chat/prompt.go`](../../backend/internal/chat/prompt.go),
[`chat/chat.go`](../../backend/internal/chat/chat.go)): the instructions — the tenant's name and slug,
the page the person is on (its path, the project's key, the ticket's short key), the date, the rules,
the capabilities the chat holds; the conversation as the browser sent it; every tool's answer of the
conversation, each clipped to 16,000 characters — ticket titles, bodies, comments, questions,
answers, names, keys; the tools' descriptions with the capabilities the chat holds; and the
provider's key in a header. Not the session cookie, no token, no audit row. A provider receives every
call from the backend's pods, never from the browser.

**Everything the person can read in the turn's tenant may reach the provider the person picked,
confidential tickets included.** The owner decided so on 2026-10-04 and accepted the risk: there is
no tenant consent, no statement that a provider runs inside or outside the installation, and no
confidential ticket is withheld ([H-37](#h-37)). Every member of every tenant has the chat once the
installation configures a provider. What gates where a tenant's text goes is the list of providers
the operator puts into the chart — nothing in the application asks anyone else.

**Which provider.** A turn names one by its id among those configured, or gets the first
([`chat.go`](../../backend/internal/api/chat.go) `chatProvider`); an id the installation does not
configure is `400 validation_failed`. The person's pick in the panel is a preference in browser
storage, `cowork.chat.provider.<person id>`, and grants nothing: any configured provider is any
member's to pick. The availability (`GET …/chat`, every member, a token as well) lists each
provider's id, name, kind and model — never its address or key (`TestChatAvailability` asserts the
answer's whole shape). The panel's empty state names the picked provider and model and says that
what the assistant reads is sent to it.

**The addresses and the keys are configuration only.** No request parameter names a host, a path or a
key, so no request can point the gateway elsewhere. A provider's URL is `https://`, or `http://` on a
host of the operator's network by its name or address ([H-41](#h-41)); it carries no user, query or
fragment. The gateway follows no redirect — a provider that redirects fails the call, and the key
travels to the configured host only ([`llm/client.go`](../../backend/internal/llm/client.go)
`Client`; `TestNoRedirectIsFollowed`). Each provider's key comes from a Secret of its own in the
chart, with no inline value — an `apiKey` in a provider's entry fails the rendering
([installation.md](../operations/installation.md#the-chat)) — and is never echoed: a configuration
error names the variable and quotes no URL, no key and no entry of the list that is no id, a
provider's refusal reaches the person as a sentence of the gateway's own, and the log gets a clip
with that provider's key taken out ([H-42](#h-42)).

## A turn works in its tenant

The tool calls go to the server's own handler, through
[`chat.Loopback`](../../backend/internal/chat/loopback.go), which sends a request only to
`/api/v1/tenants/<the turn's tenant>` and below and to the key resolver of that tenant
(`/api/v1/tickets/<tenant>/…`) — no other tenant, no `/api/v1/me` route, not the chat's own routes
(the turn and the stop) and not the event stream, and no path that is not clean. A search of "every
tenant" looks through the turn's tenant alone (`Session.Tenants`), the page tools open nothing of
another tenant, and the model is told it works in that tenant. `TestTheChatStaysInItsTenant` has the
model ask for a ticket of the person's other tenant and search everything: neither answer carries the
ticket, and nothing of it reaches the provider. A turn therefore sends one tenant's text, and no other
tenant's rides along.

Inside the tenant, the tool calls see what the person sees and nothing more: they are the person's
requests, held to the boundary, the role, the project restriction and the confidential predicate
like any ([tenancy.md](tenancy.md)).

## The chat's mark, its capabilities, and what only a session does

Every tool call carries the person's session cookie, the origin and the header of the CSRF check,
and `X-Cowork-Agent: chat/<model>/<conversation>` — the picked provider's model, its slashes as colons
([`chat.Editor`](../../backend/internal/chat/loopback.go), `chat.Mark`). The header makes the session's
request an agent's ([ADR 0036](../adr/0036-a-token-acts-as-its-person-an-agent-flag-is-the-floor-the-agent-header-only-narrows.md)
D3): it meets the hard-off list and every agent rule of
[tokens.md](tokens.md#capabilities-the-baseline-and-the-hard-off-list) — no administration, no booking
of time, no override of the prerequisite refusal, no confidential flag, no token administration — and
is refused, `403 agent_forbidden`, everything only a session does: a token, a password, an act that
gives access, a turn of the chat, the stop of one, the choice of the chat's capabilities, a logout
(`TestTheAgentHeaderOnASession`, `TestAnAgentSessionIsRefusedWhatOnlyASessionDoes`). A creating
`POST` carries an `Idempotency-Key` derived from the conversation and the call. The `api` escape hatch
is not offered: the chat reaches only the routes its tools call.

**The capabilities are the person's choice**
([ADR 0043](../adr/0043-agent-capabilities-are-chosen-per-token-the-default-is-everything-reversible-and-attributable.md)
D5). A request the header marks holds the set its person gave the chat — read on every such request
by the session's resolver ([`authenticateSession`](../../backend/internal/api/session.go)), so a
change reaches a running turn at its next call — and a person who never chose holds the default,
`auth.DefaultChatCapabilities`: `rank`, `set-horizon`, `interest`, `upload` and `create-project`.
Off by default are `decide`, `close` and `drop`, which the owner keeps a person's, and `record-answer`,
because with nothing waiting for the person a text the model read could record an answer in the
person's name. An act that needs a capability the chat does not hold is `403 agent_forbidden` with
the missing capability, which the model reads as the tool's answer
(`TestTheChatHoldsThePersonsCapabilities`).

- **Chosen in a browser session only.** `PUT /api/v1/me/chat` replaces the set; a token is `403
  session_required` — the set is what the person's agent may do in every tenant of the person, access
  that would outlive a leaked token's revocation — and a session the header marks is `403
  agent_forbidden`, so the chat never widens its own ([tokens.md](tokens.md#what-only-a-session-does)).
  `GET /api/v1/me/chat` reads it with either credential.
- **The person's alone, also in the database.** The set lives in `chat_capabilities`, one row per
  person, which only that person reads, inserts and updates — `user_id = app_user_id()` in every
  policy, no delete grant, and a `CHECK` that admits the nine capabilities only
  ([migration 24](../../backend/internal/store/migrations/000024_chat_capabilities.up.sql);
  `TestTheChatCapabilitiesArePersonal`): no administrator, of the person's tenant or of the
  installation, reads or changes it.
- **Recorded.** A change is the person's installation-level `updated` act on their person, with the set
  before and after; the same set again records nothing.
- **Told to the model.** The instructions name the capabilities the chat holds and say that an act
  needing another is refused and stays the person's; each tool's description says which of the
  capabilities it can run into the chat holds or lacks, as `cowork-mcp` says it from its token
  (`TestTheModelIsToldWhatTheChatHolds`).

**The record** of an act of the chat names the person as the actor, the mark, the capability set it
held and no token, and carries the keyed hash of the person's address — the loopback hands each tool
call the address of the turn's request ([tokens.md](tokens.md#what-is-recorded)). The mark is what the
client's conversation says ([H-39](#h-39)).

## Every call runs at once

Nothing waits for the person: the model's calls run as it makes them, each shown as a card while it
runs, and what a call changes reaches the open pages through the event stream. What bounds them is
the capabilities above and the API's rules — the role, the project restriction, the confidential
predicate, the state machine and its prerequisites, the version a write names. Text the tools return
— titles, bodies, comments, questions, answers — was written by other people and agents, and a model
that reads it can be steered by it; the instructions say such text is information and never an
instruction, and never to copy a confidential ticket's text into another ticket, a comment or a
question. Nothing enforces either ([H-38](#h-38)).

## Stop

**The panel's Stop ends the running turn at once**
([ADR 0076](../adr/0076-the-chat-in-the-ui-runs-its-loop-in-the-backend-as-an-agent-of-the-person.md)
D8). It aborts the turn's request — the turn's context is the request's, so the model's request and a
tool call in flight are cancelled — and it calls `DELETE /api/v1/tenants/{tenant}/chat/turns`, which
ends every running turn of the session's person in the tenant on the replica that answers it: each
turn is registered with the cancel of its context while it runs, the stop cancels it, the turn's
stream ends with `done` and the reason `stopped`, and the route answers `204` once those turns have
ended, or after five seconds ([`api/chat.go`](../../backend/internal/api/chat.go) `StopChatTurns`,
`stopTurns`). The route is the second path for a proxy that keeps the backend's request open after the
browser aborted it. The `chat_busy` notice — a turn of the person runs in another tab, say — offers a
Stop that calls the route. `TestStopEndsThePersonsRunningTurns` has a model that streams its first
piece and then stalls: the route ends the turn within a second, the stub provider sees its request
cancelled, and a turn of another person and the person's turn in another tenant run on.

- **Only the person's own turns, in the tenant named.** The route takes a session only, any member's;
  a token is `403 session_required`, a session the header marks `403 agent_forbidden` — the chat stops
  nothing — and the CSRF check holds.
- **What happened stays.** A tool call that completed before the stop has acted; one in flight is
  cancelled with its context, and its database transaction commits or rolls back whole, as any
  request's does when its client goes.
- **Per replica** ([H-48](#h-48)).

## What the panel shows

Everything the model or a tool wrote is text: Angular's interpolation writes it into the page as
text, line breaks kept, never as HTML and never as rendered Markdown
([`layout/chat-panel.html`](../../frontend/src/app/layout/chat-panel.html)); a tool's arguments are
JSON text in a folded block. On 2026-10-04 a model answer carrying `<img src=x onerror=…>`, `<script>`
and `**bold**` showed as those characters in Chromium and WebKit, with no element made and no script
run. A `ui` event opens a page only when it is a ticket, a backlog or a board of the turn's tenant
(`navigable` in [`core/chat.service.ts`](../../frontend/src/app/core/chat.service.ts)); anything else
is shown on the call's card as not opened. The shell's content-security policy is the second line
should text ever reach the page as markup
([trust-boundaries.md](trust-boundaries.md#the-shells-content-security-policy)). Not verified: the
panel's provider choice and capability switches in a real browser — their component tests run on
jsdom.

The conversation is held in the page's memory per tenant and is gone when another tenant's pages
open or the page reloads; nothing of it is written to browser storage. Whether the panel is open
(`cowork.chat.<person id>`) and which provider the person picked (`cowork.chat.provider.<person id>`)
are the person's preferences in `localStorage`, every access in `try`. What the calls reported before
a stop has happened.

## Limits

A turn is bounded by `COWORK_CHAT_TURN_TIMEOUT` (5 minutes), `COWORK_CHAT_MAX_STEPS` (8 calls of the
model) and `COWORK_CHAT_TURNS_PER_PERSON` (2 turns of one person at once on one replica, beyond which
`429 chat_busy`), each disabled by `0`, and its body by `COWORK_MAX_JSON_BODY`. Fixed: 400 messages,
100,000 characters a text, 32 calls a message, a tool's answer clipped for the model and the person;
the gateway's two minutes to begin an answer, ninety seconds of silence, and the sizes it reads
([ADR 0039](../adr/0039-no-request-budgets-size-and-time-limits-instead-configurable-and-switchable.md)
D2 as amended). There is no budget ([H-43](#h-43)).

## What is recorded and logged

- **The acts** are audit rows like any agent's (above); a change of the chat's capabilities is the
  person's installation-level `updated` act.
- **The request log** has the turn as one line — `POST /api/v1/tenants/<slug>/chat`, its status and
  its whole duration, written when the turn ends —, each tool call as a line of its own under a
  request id of its own (the loopback runs the whole server), and each stop as the line `DELETE
  …/chat/turns`. No body, no header, no query, so no message, no instruction and no tool answer.
- **A provider's failure** is the line `the chat's provider failed`, with the kind of failure, the
  status and a clip of at most 300 characters of the provider's message with that provider's
  configured key replaced ([H-42](#h-42)); the person gets the gateway's own sentence and the request
  id.
- **Nothing keeps the conversation**: the backend holds it for the length of a turn; a keyed tool
  call's answer is stored for twenty-four hours with its act, as any keyed `POST`'s
  ([tokens.md](tokens.md#an-agents-post-carries-an-idempotency-key)).

## What this does not cover

<a id="h-37"></a>
### H-37 — Every configured provider receives what the chat reads, confidential tickets included — an accepted risk

Accepted by the owner on 2026-10-04
([ADR 0076](../adr/0076-the-chat-in-the-ui-runs-its-loop-in-the-backend-as-an-agent-of-the-person.md)
D3), live in every tenant once a provider is configured. The chat sends the picked provider what its
tools read, and the tools read what the person may read: a confidential ticket's title, body,
comments and questions included, for a person who sees it — an administrator, the assignee, the
reporter ([tenancy.md](tenancy.md#the-confidential-flag)). A hosted provider then holds them under
its own retention and terms, for every tenant of the installation; no tenant's administrators are
asked, and nothing checks where a provider runs. Mitigation: the operator's choice of providers — a
hosted provider belongs in `chat.providers` only where every tenant's data, confidential findings
included, may go to it ([docs/operations/chat.md](../operations/chat.md#adding-a-hosted-provider)).

<a id="h-38"></a>
### H-38 — A model steered by injected text does at once whatever the chosen capabilities allow

Live by design. Text in a ticket, a comment, a question or an answer can carry instructions, and a
model may follow them; the person may not even have read that text. So can the title of a pull
request or a commit GitHub's webhook linked, which a ticket's context carries — written, of a pull
request, by the repository's owner, a member of its organisation or a collaborator only
([agent-client.md](agent-client.md#h-34) H-34). Nothing waits for the person:
within the API's rules the chat can, at once, file a ticket in any project of the tenant the person
may file in, replace a ticket's body, comment, ask a question of anyone in the tenant, link two
tickets, watch a ticket, move a ticket forward or out of `blocked`, set a progress stage short of
closing it, take a ticket backward, reopen it or withdraw a done where the API lets an agent — and,
with the default capabilities, rank, set a ticket's horizon, register interest and
create a project. A person who gives the chat `decide`, `close`, `drop` or `record-answer` gives it
those acts too, with no Run in front of them. The confidential instruction to the model is a request,
not a hold: a steered model can copy what it read into a ticket or a comment people who may not read
the original do read. Each act is recorded with the chat's mark, shown as a card while it happens,
and reaches the open pages through the event stream; most can be undone by a person — a body replaced
again from the record, a comment withdrawn, a link removed, a horizon set back, a ticket dropped
with a reason, as tickets cannot be deleted — but an act that told other people something has told
them. Of the five acts [tokens.md H-6](tokens.md#h-6) leaves to every agent, the chat makes the backward
moves, the reopens and the withdrawal of a done (`transition`, and `set_progress` for a lower stage)
and lowers its person's stake to a watch (`watch`); removing a link or a stake, editing a question and
editing a project have no tool in the chat, and neither does assigning a ticket or any act on a
saved filter, which H-6 leaves to every agent as well. Mitigation:
the default set, a narrower set in *What the assistant may do*, the cards, the activity's mark, and
Stop.

<a id="h-39"></a>
### H-39 — "Via the chat" is the client's word

Live by construction. The mark says a request was the chat's, and the server writes it from the turn
— but the turn's content is the browser's: it holds the conversation and sends it back with every
turn, and the backend holds that conversation to its shape, not to its truth. A person, or a script
with the person's session, can send a conversation whose history holds calls no model made, or send
`X-Cowork-Agent: chat/…/…` on a request of their own session. Either way the actor is the person and
every agent rule holds, with the person's chat capabilities: the mark can make a person's act look
like the model's, never let it do more than the person may, and it narrows the request. Mitigation:
read the mark as "the person, through the chat", which is all the record means by it.

<a id="h-41"></a>
### H-41 — Plain http to a provider is allowed by the host's name

Live where a provider's `COWORK_CHAT_<ID>_URL` is `http://`. The rule accepts plain http for
`localhost`, a loopback or private address, and a name without a dot or under `.localhost`, `.svc`,
`.cluster.local`, `.internal`, `.local`, `.lan` or `.home.arpa` — by the name as written, never by what
it resolves to ([`config/chat.go`](../../backend/internal/config/chat.go) `ownNetwork`). A name under
one of those suffixes that resolves beyond the operator's network — a public record of a `.lan` or
`.internal` domain, a search domain that completes a dotless name — carries the key and every turn's
text in plain text across that network. Mitigation: `https://` wherever the provider is not on the
same host or in the same cluster, and a look at what the name resolves to from the backend's pods.

<a id="h-42"></a>
### H-42 — The log's clip takes out the configured key and nothing else

Live when a provider fails. The line `the chat's provider failed` carries up to 300 characters of the
provider's error message, controls made spaces and that provider's configured key replaced by `[key]`
([`llm/client.go`](../../backend/internal/llm/client.go) `clip`;
`TestARefusalNeverEchoesTheProvider`). Whatever else a provider writes into an error — a part of the
key, the key in another encoding, an account's name, an echo of the request and with it a ticket's
text — reaches the log as written. It never reaches the person. Mitigation: treat the backend's log as
holding what the provider says, and keep its retention and its readers to those who may read the
tickets.

<a id="h-43"></a>
### H-43 — No budget: a person's turns are bounded at once, not in sum

Live today ([ADR 0039](../adr/0039-no-request-budgets-size-and-time-limits-instead-configurable-and-switchable.md)
D1). A turn is bounded by its time and its steps, and a person's turns by how many run at once on one
replica — not by how many run in an hour, how many tokens they use or what a paid provider bills, and
a person on several replicas runs that many more. A script with a person's session can run turns back
to back. Mitigation: the provider's own quota and spending limit; the request log, which has a line
per turn; the audit record.

<a id="h-44"></a>
### H-44 — The model's text can contradict the tool results; the cards are the record

Live by the nature of a model. The instructions tell it to claim only what a tool's result confirms,
and nothing enforces that: a model may say it closed a ticket the API refused it, or that a refused
call succeeded. Every call is a card with its state — running, ok, failed, no result — and the start
of its answer, and the ticket's activity is what happened. A turn that answered without calling any
tool is marked beneath its answer as one that looked nothing up: on 2026-10-04 a local model asked for
a project's tickets called no tool and listed three it had invented. Mitigation: read the cards and
that mark, not the prose.

<a id="h-48"></a>
### H-48 — The stop route reaches the turns of one replica

Live where the backend runs more than one replica (the chart runs one by default,
`backend.replicaCount`). `DELETE …/chat/turns` ends the person's turns that the replica answering it
runs; the registry it reads is that replica's, as the count of `COWORK_CHAT_TURNS_PER_PERSON` is
([`api/chat.go`](../../backend/internal/api/chat.go) `startTurn`, `stopTurns`). A turn another replica
runs is not reached by the route — the panel's abort of the turn's own request still reaches it
through that request's connection, unless a proxy in front keeps the backend's request open after the
browser went, and then that turn runs until it ends, its time runs out or the replica shuts down. The
same holds for the `chat_busy` notice's Stop when its request lands on another replica than the busy
turns. Mitigation: one backend replica; with more, a proxy in front that closes the backend's request
when the browser's ends, and `COWORK_CHAT_TURN_TIMEOUT` as the bound of a turn nobody can reach. Not
verified: whether session affinity anywhere in front of the backend would pin a person's requests to
one replica — the Ingress controller, not the browser, is the backend's client, and whether it pins a
client to one replica is the controller's configuration; none was tried.

### What a provider does with what it receives

Retention, training on the text, the provider's own access control and its region are the provider's
and its terms', for a hosted provider and for a model server alike; cowork sends a turn and keeps no
record of what the provider kept. Listing a provider in the chart is the operator's acceptance of
those terms for every tenant; it is not a review of them.
