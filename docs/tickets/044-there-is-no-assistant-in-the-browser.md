---
id: T44
title: there is no assistant in the browser — an LLM agent cannot operate the UI
state: filed
severity: medium
security: none
threat:
urgency: icebox       # rule 5: needs product calls (Q1–Q4)
effort: L
blocked-by: decision
filed-from:
opened: 2026-10-03
decided:
done:
---

## Current state

cowork is what an LLM talks to: agent tokens carry capabilities
([ADR 0043](../adr/0043-agent-capabilities-are-chosen-per-token-the-default-is-everything-reversible-and-attributable.md)),
and the MCP server for Claude Code is planned for phase 5 of
[the project plan](../planning/project-plan.md). The UI has no chat, and cowork never calls a
model. The owner wants a chat panel at the right edge of every page through which an agent
operates the UI for the person — connected first to a local LM Studio, later to other
providers such as Anthropic and OpenAI — and has decided that every call to a model goes
through the backend: the browser never talks to a provider.

What the design works with:

- **LM Studio** serves on `http://localhost:1234` OpenAI-compatible routes
  (`/v1/chat/completions` with tool calling and streaming, `/v1/responses`, `/v1/models`,
  `/v1/embeddings`), an Anthropic-compatible `/v1/messages`, and its own `/api/v1/*`; it has
  switches for CORS, for serving on the local network and for requiring an API token.
- **OpenAI Chat Completions with tool calling is the common format**: OpenAI, LM Studio,
  Ollama, vLLM, llama.cpp and OpenRouter speak it. **Anthropic** speaks its Messages API; its
  OpenAI-compatible endpoint is documented as meant for testing and comparison, not for
  production (a tool's `strict` is ignored, no prompt caching, no thinking output).
- **A browser could not reach a local model anyway**: Safari refuses requests from an HTTPS
  page to `http://localhost` as mixed content, and Chromium asks for the Local Network Access
  permission since version 142. The backend reaches what its network reaches: in `make dev` a
  local LM Studio, in a cluster only a model the cluster can reach.
- **The UI shell sends no `Content-Security-Policy`**
  ([trust-boundaries.md](../security/trust-boundaries.md#the-transport-in-front-of-the-pods)).
  A chat renders a model's output, and a prompt injected through a ticket can steer that output.
- **WebMCP** (W3C Web Machine Learning Community Group, `document.modelContext`) lets a page
  register tools that agents built into a browser call; Chrome runs it as an origin trial, and
  no other browser ships it.

## Required changes

### Independent of the open questions

1. One tool catalogue: each tool a name, a description and a JSON Schema, mapped to an
   operation of the API document
   ([ADR 0046](../adr/0046-spec-first-the-openapi-document-is-the-contract.md)) or to a UI
   action (open a ticket, the backlog or the board; filter the backlog). The MCP server of
   phase 5 serves the same catalogue, so a tool means the same to Claude Code and to the chat.
2. A provider gateway in the backend behind one interface: an adapter for OpenAI Chat
   Completions with tool calling and streaming first (LM Studio, OpenAI and the rest), one for
   the Anthropic Messages API second. The provider's address and key are operator
   configuration (`COWORK_*`, the key from a Secret), never a parameter of a request, so the
   gateway cannot be pointed at another host.
3. A chat panel on the right edge of the shell: collapsible, the answer streamed, every tool
   call shown with its arguments and its result; a model's output rendered as text or
   sanitised Markdown, never as raw HTML.
4. A `Content-Security-Policy` for the shell before the panel ships.
5. Tests: unit tests for the loop and every tool; the gateway against a stub provider in the
   integration tier; an e2e path that holds a conversation against the stub.

### Depends on the answers

6. Where the loop runs (Q1), who acts (Q2), what may leave the installation (Q3) and which
   writes are confirmed (Q4), each answer an ADR.

## Open questions

### Q1: Where does the agent's loop run?

Every model call goes through the backend; open is who executes the tools.

- **(a) The browser runs the loop**, and the backend forwards each model call to the
  configured provider and streams the answer back. The tools are the frontend's own actions —
  the services the pages use, and navigation — and their results show on screen as they happen.
- **(b) The backend runs the loop** and executes the API tools itself; the browser shows the
  conversation and receives navigation as messages over the event stream. One place holds the
  conversation; a navigation tool needs that channel back to the page.

Recommended: **(a)** — "the UI operated by an agent" is then literal: the agent uses the
actions the person uses, every change arrives through the same event stream within a second,
and the backend stays a gateway that holds no conversation. Q2's marker keeps the limits on
the server either way.

**Answer:** _open_

### Q2: Who acts when the chat's agent acts?

Ticket bodies and comments are written by other people and by agents, and a model that reads
them can be steered by them.

- **(a) The person**: the tool calls carry the person's session and nothing else. Simple, but
  an injected instruction then acts with the person's full power, including what ADR 0043 D3
  keeps from every agent (members, tokens, deletion, booking time).
- **(b) An agent of the person**: every tool call is marked as the chat's, attributed to the
  person "via chat", limited to capabilities the person picks for the chat as for a token,
  and held to the hard-off list.

Recommended: **(b)** — the limits cowork already draws around agents are the ones a steerable
model needs, and the timeline keeps telling the person's acts from the model's.

**Answer:** _open_

### Q3: What may be sent to a provider outside the installation?

The chat sends what its tools read to the model: tickets of client tenants, confidential
tickets ([ADR 0065](../adr/0065-a-confidential-flag-replaces-the-file-name-embargo-set-automatically-lifted-only-by-a-person.md)),
`live` security findings.

- **(a) Everything** the person can read.
- **(b) Per tenant, an administrator allows a provider**; confidential tickets never leave, and
  a provider on the person's own machine or network, such as LM Studio, counts as inside.
- **(c) Only providers inside the installation's network.**

Recommended: **(b)** — a client's data leaves only with that tenant's consent, and the local
case the owner starts with needs no consent at all.

**Answer:** _open_

### Q4: Which writes does the person confirm before they run?

- **(a) None**: the agent acts, and the timeline records every act.
- **(b) The acts a person owes a reason or a note for** — done, dropped, a backward move, a
  block — shown as a proposal with Run and Skip.
- **(c) Every write.**

Recommended: **(b)** — the agent stays quick at filing, ranking and editing, and the acts that
need a person's reason or verification stay the person's.

**Answer:** _open_

## Not verified

Which models in LM Studio call tools reliably enough for the catalogue; a spike with two or
three models against the stub tools would settle it.
