---
id: T44
title: the assistant in the browser is built on provisional answers — the owner decides where its loop runs, who acts, what leaves the installation and what is confirmed
state: in-progress
severity: medium
security: hardening
threat: answering would additionally cover a model steered by text in a ticket or comment into acts the person did not want, confidential tickets read by the chat reaching a provider outside the installation, and a chat that acts with every capability of its person
urgency: release      # rule 2: gates the release — merging the branch releases the chat
effort: M
blocked-by: decision
filed-from:
opened: 2026-10-03
decided:
done:
---

## Current state

The chat at the right edge of every tenant page is built on the branch `feat/phase-4-and-5`
([ADR 0076](../adr/0076-the-chat-in-the-ui-runs-its-loop-in-the-backend-as-an-agent-of-the-person.md),
accepted provisionally). The owner had not answered Q1–Q4 when phase 5 was built in one night; on
the owner's instruction (build to best knowledge, leave a gate open when in doubt, file what needs
the owner) each was built as recommended — Q1 against the earlier recommendation, see there.

- The backend runs the loop: the browser posts the conversation, the backend calls the configured
  model (`COWORK_CHAT_*`: OpenAI Chat Completions with tool calling — LM Studio, Ollama, OpenAI — or
  the Anthropic Messages API), runs its tool calls in-process through the API's own pipeline, and
  streams text, tool calls, results and navigation back. The tools are the MCP server's catalogue
  without `api` and `session_start`, plus `open_ticket`, `open_backlog` and `open_board`.
- Every tool call is an agent's act of the person, marked `chat/<model>/<conversation>`, confined to
  the tenant of the turn; acts that need a person's reason, note or decision wait for Run or Skip.
- An outside provider needs the tenant's consent, which an administrator gives in a browser session
  and which is bound to the provider's kind, host and model; a provider inside the installation
  (`COWORK_CHAT_INSIDE`) needs none.
- The panel renders text only; the shell sends a `Content-Security-Policy`.
- **Verified 2026-10-04** against LM Studio `qwen/qwen3-30b-a3b-2507` with a 32k context: the chat
  filed a ticket, set its urgency to `now` and moved it to `analysed` in 16 s; the open board showed
  the card at once; every act is recorded as `chat/qwen:qwen3-30b-a3b-2507/<conversation>`. The small
  model sometimes claims an act a tool result refused; the tool cards show what happened.
- The gaps are named in [chat.md](../security/chat.md).

## Required changes

1. The owner answers Q1–Q5; an answer that differs from the build amends ADR 0076 and changes the
   code, the panel and [chat.md](../security/chat.md) in the same change.
2. Depending on Q2: the person's choice of the chat's capabilities, on the tokens page or in the
   panel, with the ADR 0043 D4 capabilities.
3. Depending on Q3: an outside provider never receives a confidential ticket
   ([ADR 0065](../adr/0065-a-confidential-flag-replaces-the-file-name-embargo-set-automatically-lifted-only-by-a-person.md)) —
   the loopback withholds it from the tool results.
4. A model check: which models of LM Studio, Ollama and the hosted providers call the tools reliably
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

**Answer:** _open_

### Q2: Who acts when the chat's agent acts?

- **(a) The person**, with their full session.
- **(b) An agent of the person** (built): `X-Cowork-Agent` on the session, held to the hard-off list
  of ADR 0043 D3 and refused the session-only operations — with **every** capability, because the
  choice of capabilities per person is not built.

Recommended: **(b) with the choice built** (required change 2), its default leaving `decide`,
`close` and `drop` to the person — the confirmations then stop being the only brake.

**Answer:** _open_

### Q3: What may be sent to a provider outside the installation?

- **(a) Everything** the person can read.
- **(b) Per tenant, after an administrator's consent** (built): bound to the provider's kind, host
  and model, so a switched provider asks again; a provider on the operator's machine or network
  counts as inside. Confidential tickets are **not** withheld yet.
- **(c) Only providers inside the installation.**

Recommended: **(b) with confidential tickets withheld** (required change 3) — a client's data leaves
only with that tenant's consent, and a confidential finding never.

**Answer:** _open_

### Q4: Which writes does the person confirm before they run?

- **(a) None.**
- **(b) The acts a person owes a reason, a note or a decision for** (built): done, dropped, a block,
  a backward move or reopen, `decided`, a stage that closes or reopens, `finish_work`,
  `record_answer`, `create_project` — and every write in a turn after a tool returned a confidential
  ticket.
- **(c) Every write.**

Recommended: **(b)** — quick at filing, ranking and editing, and the acts that need the person stay
the person's.

**Answer:** _open_

### Q5: May every member use the chat?

Any member, viewers included, may chat; the API refuses a viewer's writes as tool results. A turn
costs the operator a provider call (a paid one for an outside provider); turns in flight are bounded
per person (`COWORK_CHAT_TURNS_PER_PERSON`), there is no budget (ADR 0039 D1).

- **(a) Every member** (built).
- **(b) Members and administrators only.**

Recommended: **(a)** — a viewer reads, and the chat is a way to read; the bound keeps one person
from occupying a local model.

**Answer:** _open_

## Not verified

The hosted OpenAI and Anthropic endpoints (only the stub provider in the tests); models other than
`qwen/qwen3-30b-a3b-2507` (the `qwen3.6` models did not load in this LM Studio); a screen reader on
the panel.

## Related

- T48 — phase 5, the MCP server and the workflow of Claude Code
- T22 — the agent gates the chat inherits
