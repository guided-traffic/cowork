---
id: T44
title: the assistant in the browser is built on provisional answers — the owner decides where its loop runs, who acts, what leaves the installation and what is confirmed
state: in-progress
severity: medium
security: hardening
threat: the model check would additionally cover a model that answers without calling the tools and invents tickets, which the panel marks but cannot prevent
urgency: later        # rule 4: what remains is a known piece of work, the model check
effort: M
filed-from:
opened: 2026-10-03
decided: 2026-10-04
done:
---

## Current state

The chat at the right edge of every tenant page is released in `0.3.0`
([ADR 0076](../adr/0076-the-chat-in-the-ui-runs-its-loop-in-the-backend-as-an-agent-of-the-person.md)),
with the owner's answers of 2026-10-04 below.

- The backend runs the loop: the browser posts the conversation, the backend calls the provider the
  person picked from the chart's list (`chat.providers`, `COWORK_CHAT_PROVIDERS`: OpenAI Chat
  Completions with tool calling — LM Studio, Ollama, OpenAI — or the Anthropic Messages API), runs its
  tool calls in-process through the API's own pipeline, and streams text, tool calls, results and
  navigation back.
- Every tool call is an agent's act of the person, marked `chat/<model>/<conversation>`, confined to
  the tenant of the turn, and runs at once; the chat holds the capabilities the person chose in the
  panel — by default all but `decide`, `close`, `drop` and `record-answer` — and the API refuses the
  rest. A configured provider receives everything the person can read, confidential tickets
  included: the owner's accepted risk ([H-37](../security/chat.md#h-37)).
- Stop aborts the turn's request and calls `DELETE …/chat/turns`, which ends the person's turns on
  the replica it reaches ([H-48](../security/chat.md#h-48)); a busy notice offers the same.
- **Verified 2026-10-04** against LM Studio `qwen/qwen3-30b-a3b-2507` (MLX, 32k, one prediction), in
  Chromium: the pick of a provider reached the turn; filing, urgency `now` and `analysed` ran at once,
  each act marked as the chat's without a confirmation; `decided` was refused with
  `403 agent_forbidden: missing capability: decide` while `decide` was off, and ran once the panel
  switched it on — in a new conversation, because the model went on refusing in the old one, which
  the panel now says and offers a new one for; Stop in the tab ended the turn in 69 ms and Stop from
  another tab in 22 ms, and LM Studio logged that it stopped generating in the same second.

## Required changes

1. A model check: which models of LM Studio, Ollama and the hosted providers call the tools reliably
   enough; a short list in the chat's operations page.

## Open questions

### Q1: Where does the agent's loop run?

- **(a) In the browser**: the backend forwards each model call; the tools are the pages' own
  services and navigation.
- **(b) In the backend** (built): the tools are the MCP server's catalogue run in-process through the
  API's own pipeline; navigation reaches the page as stream events.

Recommended: **(b)** — the earlier recommendation was (a); (b) won once the catalogue existed: one
Go implementation of every tool for Claude Code and the chat instead of a second one in TypeScript,
every call through validation, authorization, agent rules, audit and events, and the page still
follows the agent.

**Answer:** (b) — the loop runs in the backend, as built. Recorded in ADR 0076.

### Q2: Who acts when the chat's agent acts?

- **(a) The person**, with their full session.
- **(b) An agent of the person** (built): `X-Cowork-Agent` on the session, held to the hard-off list
  of ADR 0043 D3 and refused the session-only operations — with **every** capability, because the
  choice of capabilities per person is not built.

Recommended: **(b) with the choice built** (required change 2), its default leaving `decide`,
`close` and `drop` to the person — the confirmations then stop being the only brake.

**Answer:** (b) with the choice built — the person chooses the chat's capabilities from the nine of
ADR 0043 D4; `decide`, `close` and `drop` are off by default, so the API refuses them to the chat
even after Run. Recorded in ADR 0076 and ADR 0043 D5.

### Q3: What may be sent to a provider outside the installation?

- **(a) Everything** the person can read.
- **(b) Per tenant, after an administrator's consent** (built): bound to the provider's kind, host
  and model, so a switched provider asks again; a provider on the operator's machine or network
  counts as inside. Confidential tickets are **not** withheld yet.
- **(c) Only providers inside the installation.**

Recommended: **(b) with confidential tickets withheld** (required change 3) — a client's data leaves
only with that tenant's consent, and a confidential finding never.

**Answer:** (a), with the risk accepted by the owner — the providers are configured in the Helm
chart and nowhere in the app; a configured provider may see everything the person can read,
confidential tickets included; no tenant consent, no inside or outside. The chart holds a list of
providers, and the person picks one in the panel. Recorded in ADR 0076.

### Q4: Which writes does the person confirm before they run?

- **(a) None.**
- **(b) The acts a person owes a reason, a note or a decision for** (built): done, dropped, a block,
  a backward move or reopen, `decided`, a stage that closes or reopens, `finish_work`,
  `record_answer`, `create_project` — and every write in a turn after a tool returned a confidential
  ticket.
- **(c) Every write.**

Recommended: **(b)** — quick at filing, ranking and editing, and the acts that need the person stay
the person's.

**Answer:** (a) — nothing waits for a Run; the capabilities the person chooses (Q2) are the only
limit, and the proposals, the `+confirmed` mark and the confidential hold go. Recorded in ADR 0076.

### Q5: May every member use the chat?

Any member, viewers included, may chat; the API refuses a viewer's writes as tool results. A turn
costs the operator a provider call (a paid one for an outside provider); turns in flight are bounded
per person (`COWORK_CHAT_TURNS_PER_PERSON`), there is no budget (ADR 0039 D1).

- **(a) Every member** (built).
- **(b) Members and administrators only.**

Recommended: **(a)** — a viewer reads, and the chat is a way to read; the bound keeps one person
from occupying a local model.

**Answer:** (a) — every member, viewers included, as built. Recorded in ADR 0076.

## Not verified

The hosted OpenAI and Anthropic endpoints (only the stub provider in the tests); models other than
`qwen/qwen3-30b-a3b-2507` (the `qwen3.6` models did not load in this LM Studio); a screen reader on
the panel.

## Related

- T48 — phase 5, closed with 0.3.0 (archived)
- T22 — the agent gates the chat inherits
