# The assistant in the browser

What the chat in the UI may make cowork do and on whose behalf, what of a tenant leaves the
installation and with whose consent, how a turn is kept in its tenant, which acts wait for the
person, how the panel shows what a model writes, and what bounds a turn — as built on 2026-10-04,
provisionally ([ADR 0076](../adr/0076-the-chat-in-the-ui-runs-its-loop-in-the-backend-as-an-agent-of-the-person.md);
the owner's review is pending). The agent the person runs on their own machine with a token is
[agent-client.md](agent-client.md); the agent rules both meet are [tokens.md](tokens.md); the
policy the shell sends is [trust-boundaries.md](trust-boundaries.md#the-shells-content-security-policy);
running the chat is [docs/operations/chat.md](../operations/chat.md).

## Who trusts whom

```
 browser — the person's session                backend                                provider
┌───────────────────────────┐  POST …/chat   ┌──────────────────────────────────┐   ┌───────────────┐
│ panel: text only          │ ─────────────► │ turn: session, CSRF, tenant,     │   │ the model the │
│ conversation, per tenant, │                │ consent, limits                  │   │ operator      │
│ sent back with each turn  │ ◄───────────── │ loop ─► gateway ───────────────────►│ configured    │
└───────────────────────────┘  event stream  │  │  instructions, conversation,   │   │ (inside or    │
                                             │  │  tools' answers, key           │◄──│  outside)     │
                                             │  └─► tools ─► loopback ─► the API │   └───────────────┘
                                             │      pipeline as the person's    │
                                             │      agent, in the turn's tenant │
                                             └──────────────────────────────────┘
```

| Party | Trusted for | Not trusted for |
|---|---|---|
| The operator's configuration | where the provider is, its key and model (`COWORK_CHAT_*`), and the statement that it runs inside the installation (`COWORK_CHAT_INSIDE`) — taken as given | — |
| A tenant's administrators | consent to send the tenant's text to a provider outside | — |
| The provider | keeping what a turn sends it, under the consent | anything it answers: its text is shown as text, its tool calls are requests the API judges |
| The model | nothing | its calls run as an agent's with the agent rules, and the acts of [below](#what-waits-for-the-person) wait for the person |
| The person's browser | the person's own messages and decisions | the conversation it sends back: the backend holds its shape to the rules ([`chat.Check`](../../backend/internal/chat/check.go)), not its truth ([H-39](#h-39)) |
| Text in tickets, comments, questions, answers | — | it was written by other people and agents, and it can steer the model ([H-38](#h-38)) |

## What leaves the installation, and with whose consent

**What a call of the model carries** ([`chat/prompt.go`](../../backend/internal/chat/prompt.go),
[`chat/chat.go`](../../backend/internal/chat/chat.go)): the instructions — the tenant's name and slug,
the page the person is on (its path, the project's key, the ticket's short key), the date, the rules;
the conversation as the browser sent it; every tool's answer of the conversation, each clipped to
16,000 characters — ticket titles, bodies, comments, questions, answers, names, keys; the tools'
descriptions with the capabilities the chat holds; and the key in a header. Not the session cookie,
no token, no audit row. The provider receives every call from the backend's pods, never from the
browser.

**Inside or outside** ([`config/chat.go`](../../backend/internal/config/chat.go),
[`api/chat.go`](../../backend/internal/api/chat.go) `chatAvailability`). With
`COWORK_CHAT_INSIDE=true` the operator states that the provider runs inside the installation's trust
boundary — the operator's machine or network — and every tenant has the chat without being asked;
nothing checks the statement. Otherwise a tenant has the chat only while its consent holds:

- **Switched on by an administrator in a browser session** (`chat_external_allowed` in
  `PATCH /api/v1/tenants/{tenant}`); a token that tries is `403 session_required` — a consent made
  with a leaked token would outlive the token's revocation
  ([tokens.md](tokens.md#what-only-a-session-does)). Switched off by an administrator's `admin`-scope
  token as well, which only takes something away. No agent switches it: it is administration.
- **Bound to the provider it was given to.** Switching it on stores the configured provider's
  fingerprint beside it — the wire format, the URL's host with its port, and the model
  (`chat_external_provider`, [migration 24](../../backend/internal/store/migrations/000024_tenant_chat_consent.up.sql);
  `Chat.Fingerprint` in [`config/chat.go`](../../backend/internal/config/chat.go)) — and the consent
  holds only while the configuration names the same: another model or another host is a provider the
  tenant did not allow, the tenant reads `chat_external_allowed: false` and has no chat until an
  administrator allows it again (`TestTheConsentNamesItsProvider`). The scheme, the URL's path and
  the key are not part of the fingerprint. With no provider configured, switching it on is
  `409 chat_unavailable`.
- **Recorded** as the tenant's `updated` act with the values before and after — the consent and the
  fingerprint, so the tenant's administrators read in the audit view the host and the model they
  allowed (`TestChatAvailability`).
- **Read again before every call of the model**, so a consent withdrawn while a turn runs ends that
  turn before the next call.

What the members are told: the availability (`GET …/chat`, every member, a token as well) names the
wire format, the model and whether the operator declares the provider inside — never its address
(`TestChatAvailability` asserts the answer's whole shape). The panel's empty state says, for a
provider outside, that what the assistant reads is sent to it; the consent switch names the model
and that the tenant's text then leaves the installation.

**The address and the key are configuration only.** No request parameter names a host, a path or a
key, so no request can point the gateway elsewhere. The URL is `https://`, or `http://` on a host of
the operator's network by its name or address ([H-41](#h-41)); it carries no user, query or
fragment. The gateway follows no redirect — a provider that redirects fails the call, and the key
travels to the configured host only ([`llm/client.go`](../../backend/internal/llm/client.go)
`Client`; `TestNoRedirectIsFollowed`). The key comes from a Secret in the chart, with no inline value
([installation.md](../operations/installation.md#the-chat)), and is never echoed: a configuration
error names the variable, a provider's refusal reaches the person as a sentence of the gateway's
own, and the log gets a clip with the key taken out ([H-42](#h-42)).

**Confidential tickets are not withheld** ([H-37](#h-37)).

## A turn works in its tenant

The tool calls go to the server's own handler, through
[`chat.Loopback`](../../backend/internal/chat/loopback.go), which sends a request only to
`/api/v1/tenants/<the turn's tenant>` and below and to the key resolver of that tenant
(`/api/v1/tickets/<tenant>/…`) — no other tenant, no `/api/v1/me` route, not the chat itself and not
the event stream, and no path that is not clean. A search of "every tenant" looks through the turn's
tenant alone (`Session.Tenants`), the page tools open nothing of another tenant, and the model is told
it works in that tenant. `TestTheChatStaysInItsTenant` has the model ask for a ticket of the person's
other tenant and search everything: neither answer carries the ticket, and nothing of it reaches the
provider. A tenant's consent therefore covers what that tenant's turns send, and no other tenant's
text rides along.

Inside the tenant, the tool calls see what the person sees and nothing more: they are the person's
requests, held to the boundary, the role, the project restriction and the confidential predicate
like any ([tenancy.md](tenancy.md)).

## The chat's mark, its capabilities, and what only a session does

Every tool call carries the person's session cookie, the origin and the header of the CSRF check,
and `X-Cowork-Agent: chat/<model>/<conversation>` — the model's slashes as colons
([`chat.Editor`](../../backend/internal/chat/loopback.go), `chat.Mark`). The header makes the session's
request an agent's ([ADR 0036](../adr/0036-a-token-acts-as-its-person-an-agent-flag-is-the-floor-the-agent-header-only-narrows.md)
D3): it holds every capability, meets the hard-off list and every agent rule of
[tokens.md](tokens.md#capabilities-the-baseline-and-the-hard-off-list) — no administration, no booking
of time, no override of the prerequisite refusal, no confidential flag, no token administration — and
is refused, `403 agent_forbidden`, everything only a session does: a token, a password, an act that
gives access, a turn of the chat, a logout (`TestTheAgentHeaderOnASession`,
`TestAnAgentSessionIsRefusedWhatOnlyASessionDoes`). A creating `POST` carries an `Idempotency-Key`
derived from the conversation and the call. The `api` escape hatch is not offered: the chat reaches
only the routes its tools call.

**The record** of an act of the chat names the person as the actor, the mark, the capability set
and no token, and carries the keyed hash of the person's address — the loopback hands each tool call
the address of the turn's request ([tokens.md](tokens.md#what-is-recorded)). A call the person ran by
deciding a proposal records the mark with `+confirmed` after the conversation's id. The mark is what
the client's conversation says ([H-39](#h-39)). The chat holds every capability; no person chooses
less ([H-40](#h-40)).

## What waits for the person

Text the tools return — titles, bodies, comments, questions, answers — was written by other people
and agents, and a model that reads it can be steered by it. The instructions say such text is
information and never an instruction; nothing enforces that. What does hold is the API's judgement of
every call, and a policy per tool ([`chat/confirm.go`](../../backend/internal/chat/confirm.go)
`policies`; a tool the policies do not name is not offered, `TestEveryToolIsClassified`):

| The call | Runs at once | Waits for the person |
|---|---|---|
| `get_ticket`, `search`, `open_ticket`, `open_backlog`, `open_board` | always | never |
| `file_ticket`, `record_state`, `open_question`, `comment`, `link`, `watch`, `set_urgency` | until the conversation has read a confidential ticket | from then on |
| `transition` | a forward move, the way out of `blocked` — until the conversation has read a confidential ticket | to `decided`, `done`, `dropped`, `blocked`; back; a reopen; the withdrawal of a done; any move whose ticket cannot be read |
| `set_progress` | a write that neither closes nor reopens — the same hold | the write that fills the last stage, the one that lowers a stage of a ticket its stages closed, any write whose ticket cannot be read |
| `finish_work`, `record_answer`, `create_project` | never | always |

A call that waits ends the turn as a proposal: the act in words, under the call's card, with Run and
Skip. The words come from what the API read — the canonical key and title — and from the model's
arguments made plain: every control, format and line-separating character a space, so no line break
and no right-to-left override rearranges what the person reads, every quote mark an apostrophe, at
most 300 characters a quoted text (`plain`, `quote`). The review fails closed: a call whose arguments
it cannot read, or whose act depends on a ticket it cannot read, waits. What the review read is pinned
into the waiting call — a transition's `from`, a progress write's `version`, `finish_work`'s `from` —
so the API refuses it, `409 state_conflict` or `412 precondition_failed`, or the tool does nothing,
when the ticket moved before the person decided (`TestAProposalIsPinnedToWhatItRead`);
`record_answer` and `create_project` carry none ([H-46](#h-46)). Only the first waiting call takes a
decision; a later one of the same answer that needs a decision did not run and is answered so, and
the model must make it again; a decision on any other call is `400 validation_failed`
([`chat.Check`](../../backend/internal/chat/check.go)). Skip, or a new message instead, answers the
model that the call did not run. A Run sent twice makes one act where the call creates or is pinned:
a creating `POST` carries a key derived from the conversation and the call, which the API replays,
and a pinned call meets its pin (`TestADecidedCallIsMarkedAndReplays`). An overwriting call without a
pin sends its write again: an equal answer or body changes nothing, and a held `set_urgency` records
its override a second time.

**The hold after a confidential ticket.** When an answer of the API to a tool call carries a ticket
with `"confidential": true`, the tool's answer begins with a note, and every write of the conversation
waits for the person from then on: what the model read, it could write where people who may not read
it would. The model is also told never to copy confidential text into another ticket, a comment or a
question. The hold lives in the conversation the browser keeps ([H-45](#h-45)).

What runs without a decision is [H-38](#h-38).

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
([trust-boundaries.md](trust-boundaries.md#the-shells-content-security-policy)).

The conversation is held in the page's memory per tenant and is gone when another tenant's pages
open or the page reloads; nothing of it is written to browser storage. Whether the panel is open is
the person's preference, `cowork.chat.<person id>` in `localStorage`. Stop aborts the stream; what the
calls reported before it has happened.

## Limits

A turn is bounded by `COWORK_CHAT_TURN_TIMEOUT` (5 minutes), `COWORK_CHAT_MAX_STEPS` (8 calls of the
model) and `COWORK_CHAT_TURNS_PER_PERSON` (2 turns of one person at once on one replica, beyond which
`429 chat_busy`), each disabled by `0`, and its body by `COWORK_MAX_JSON_BODY`. Fixed: 400 messages,
100,000 characters a text, 32 calls a message, a tool's answer clipped for the model and the person;
the gateway's two minutes to begin an answer, ninety seconds of silence, and the sizes it reads
([ADR 0039](../adr/0039-no-request-budgets-size-and-time-limits-instead-configurable-and-switchable.md)
D2 as amended). There is no budget ([H-43](#h-43)).

## What is recorded and logged

- **The acts** are audit rows like any agent's (above); the consent is the tenant's `updated` act.
- **The request log** has the turn as one line — `POST /api/v1/tenants/<slug>/chat`, its status and
  its whole duration, written when the turn ends — and each tool call as a line of its own under a
  request id of its own: the loopback runs the whole server. No body, no header, no query, so no
  message, no instruction and no tool answer.
- **A provider's failure** is the line `the chat's provider failed`, with the kind of failure, the
  status and a clip of at most 300 characters of the provider's message with the configured key
  replaced ([H-42](#h-42)); the person gets the gateway's own sentence and the request id.
- **Nothing keeps the conversation**: the backend holds it for the length of a turn; a keyed tool
  call's answer is stored for twenty-four hours with its act, as any keyed `POST`'s
  ([tokens.md](tokens.md#an-agents-post-carries-an-idempotency-key)).

## What this does not cover

<a id="h-37"></a>
### H-37 — Confidential tickets reach the provider

Live in every tenant whose chat is available. The chat sends the model what its tools read, and the
tools read what the person may read: a confidential ticket's title, body, comments and questions
included, for a person who sees it — an administrator, the assignee, the reporter
([tenancy.md](tenancy.md#the-confidential-flag)). A provider outside the installation then holds them
under its own retention and terms in every tenant whose administrators allowed it, and with
`COWORK_CHAT_INSIDE=true` the provider receives every tenant's, no administrator asked — and nothing
checks that it runs where the operator says. The hold of [What waits for the person](#what-waits-for-the-person)
keeps the model's later writes from spreading such text inside the tenant; it does not keep the text
from the provider. Withholding confidential tickets from a provider outside is left open
([ADR 0076](../adr/0076-the-chat-in-the-ui-runs-its-loop-in-the-backend-as-an-agent-of-the-person.md)).
Mitigation: `COWORK_CHAT_INSIDE` only for a provider on machines the installation's operators run; a
tenant whose confidential tickets must not leave does not allow a provider outside.

<a id="h-38"></a>
### H-38 — Reversible acts run without the person's decision, and the model can be steered into them

Live by design. Text in a ticket, a comment, a question or an answer can carry instructions, and a
model may follow them; the person may not even have read that text. Without a decision, until the
conversation has read a confidential ticket, the chat can: file a ticket in any project of the tenant
the person may file in, replace a ticket's body, comment, ask a question of anyone in the tenant,
link two tickets, watch a ticket, set or withdraw an urgency override, move a ticket forward
(`filed → analysed`, `decided → in-progress`, `in-progress → review`) or out of `blocked` to where it
came from, and set a progress stage short of the write that closes or reopens. Each is recorded with
the chat's mark, shown as a card while it happens, and reaches the open pages through the event
stream; each can be undone by a person — a body replaced again from the record, a comment withdrawn,
a link removed, an override withdrawn, a ticket dropped with a reason, as tickets cannot be deleted —
but an act that told other people something has told them. The open agent gates of
[tokens.md H-6](tokens.md#h-6) have no tool in the chat. Mitigation: the cards, the activity's mark,
and Stop.

<a id="h-39"></a>
### H-39 — "Via the chat" is the client's word

Live by construction. The mark says a request was the chat's, and the server writes it from the turn
— but the turn's content is the browser's: it holds the conversation and sends it back with every
turn, and the backend holds that conversation to its shape, not to its truth. A person, or a script
with the person's session, can send a conversation whose last message is a call no model made, decide
it, and have it run marked as the chat's and `+confirmed`; or send `X-Cowork-Agent: chat/…/…` on a
request of their own session. Either way the actor is the person and every agent rule holds: the mark
can make a person's act look like the model's, never let it do more than the person may, and it
narrows the request. Mitigation: read the mark as "the person, through the chat", which is all the
record means by it.

<a id="h-40"></a>
### H-40 — The chat holds every capability; no person chooses less

Live today, a gate left open
([ADR 0076](../adr/0076-the-chat-in-the-ui-runs-its-loop-in-the-backend-as-an-agent-of-the-person.md)).
[ADR 0043](../adr/0043-agent-capabilities-are-chosen-per-token-the-default-is-everything-reversible-and-attributable.md)'s
capabilities are chosen per token; the chat has no token, and its requests hold the full set for every
person in every tenant where it is available. The proposals hold the acts behind `decide`, `close`,
`drop`, `record-answer` and `create-project` for the person's Run; `override-urgency` acts at once
through `set_urgency`; `rank`, `interest` and `upload` have no tool in the chat. A person or a tenant
that wants an "assisted" chat cannot have one. Mitigation: none per person; the operator switches the
chat off, a tenant's administrators withdraw a provider's consent.

<a id="h-41"></a>
### H-41 — Plain http to the provider is allowed by the host's name

Live where `COWORK_CHAT_URL` is `http://`. The rule accepts plain http for `localhost`, a loopback or
private address, and a name without a dot or under `.localhost`, `.svc`, `.cluster.local`,
`.internal`, `.local`, `.lan` or `.home.arpa` — by the name as written, never by what it resolves to
([`config/chat.go`](../../backend/internal/config/chat.go) `ownNetwork`). A name under one of those
suffixes that resolves beyond the operator's network — a public record of a `.lan` or `.internal`
domain, a search domain that completes a dotless name — carries the key and every turn's text in
plain text across that network. Mitigation: `https://` wherever the provider is not on the same host
or in the same cluster, and a look at what the name resolves to from the backend's pods.

<a id="h-42"></a>
### H-42 — The log's clip takes out the configured key and nothing else

Live when a provider fails. The line `the chat's provider failed` carries up to 300 characters of the
provider's error message, controls made spaces and the configured key replaced by `[key]`
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
and nothing enforces that: a model may say it closed a ticket whose close it only proposed, or that a
refused call succeeded. Every call is a card with its state — running, ok, failed, waits for you,
skipped, no result — and the start of its answer, and the ticket's activity is what happened. A
turn that answered without calling any tool is marked beneath its answer as one that looked nothing
up: on 2026-10-04 a local model asked for a project's tickets called no tool and listed three it had
invented. Mitigation: read the cards and that mark, not the prose.

<a id="h-45"></a>
### H-45 — The hold after a confidential ticket lives in the conversation the browser keeps

Live wherever the chat reads a confidential ticket. The hold begins when an answer of the API to a tool
call carries a ticket whose JSON says `"confidential": true`, and a later turn finds it by the note at
the start of that tool's answer in the conversation the browser sends back
([`chat/chat.go`](../../backend/internal/chat/chat.go) `tainted`). A new conversation, a reloaded
page or a client that leaves the message out writes without the hold; and text of a confidential
ticket that reaches a tool's answer without that ticket in JSON beside it starts none — the title of
a linked confidential ticket in another ticket's links or context document, a comment that quotes
confidential text. The hold keeps the model from copying what it read; it was never a boundary
against the person, who may read the ticket. Mitigation: the proposals and the cards; H-37 for what
the provider holds.

<a id="h-46"></a>
### H-46 — `record_answer` and `create_project` wait, but run against what is there when the person runs them

Live where the chat proposes either. A transition, a progress write and `finish_work` carry what the
review read as their precondition, so a ticket that moved before the person decided refuses the call.
`record_answer` and `create_project` carry none: the person's Run sends the call as the model wrote
it, against the question or the tenant as they are at that moment. An answer an agent of the person
recorded meanwhile is replaced — a person's own answer an agent may not change, and the API refuses
that — and a project created meanwhile for the same remote is answered as the one that binds it, or
`409 project_key_taken` for its key. The proposal's words describe the call, not the state it will
meet. Mitigation: the proposal names the question and the answer, the project and the remote; the
activity shows what ran.

<a id="h-47"></a>
### H-47 — The consent's audit row shows the provider's host to the tenant's administrators

Live in every tenant that allowed a provider outside. The availability never names the provider's
address, but the consent stores the provider's fingerprint — the wire format, the host and port of
`COWORK_CHAT_URL`, the model — and its audit row carries it, so the tenant's administrators read the
host in the audit view. A host name of the operator's network that says more than the operator wants
a client's administrators to know reaches them that way. The path, the key and the scheme are not in
it. Mitigation: a provider host whose name is fit to be read by the tenants' administrators.

### What the provider does with what it receives

Retention, training on the text, the provider's own access control and its region are the provider's
and its terms', for a hosted provider and for a model server alike; cowork sends a turn and keeps no
record of what the provider kept. The tenant's consent is consent to that provider as the operator
configured it, not a review of its terms.
