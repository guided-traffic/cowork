---
id: T48
title: phase 5 (the LLM interface, the workflow of Claude Code and the chat) was built in one night on provisional decisions — the owner reviews them before the release
state: in-progress
severity: medium
security: hardening
threat: the open questions would additionally cover an agent that closes a ticket while its question is still unanswered (Q1) and a release binary replaced on its way to the person who installs it (Q2)
urgency: release      # rule 2: gates the release — merging the branch releases phase 5
effort: S
blocked-by: decision
filed-from: docs/planning/project-plan.md phase 5, converted and built 2026-10-04 (ADR 0074 D2)
opened: 2026-10-04
decided:
done:
---

## Current state

The family ticket of phase 5 ([ADR 0074](../adr/0074-the-question-catalog-is-consumed-phases-become-tickets-when-they-start-in-their-own-session.md)
D2). **Goal:** a Claude Code session in a bound repository starts with its ticket context and ends
with the ticket updated by Claude; in the UI, a chat lets an agent operate cowork for the person.
Built on the branch `feat/phase-4-and-5` on the owner's instruction of 2026-10-03 (build to best
knowledge, leave a gate open when in doubt, file what the owner should look at).

- **Built:** the repository binding by normalised remote and the lookup across the person's tenants
  ([ADR 0066](../adr/0066-repositories-are-bound-by-their-normalised-remote-identity-creation-proposed-by-the-agent-confirmed-by-the-person.md)),
  `/context` ([ADR 0044](../adr/0044-two-endpoints-markdown-is-the-canonical-ticket-context-is-the-ticket-with-what-surrounds-it.md)),
  `cowork-mcp` over stdio with fifteen workflow tools and the `api` escape hatch, its subcommands
  and release binaries for six platforms ([ADR 0040](../adr/0040-rest-is-the-contract-mcp-is-the-ergonomic-surface-and-can-do-nothing-the-api-cannot.md)–[ADR 0042](../adr/0042-twelve-workflow-tools-and-one-escape-hatch.md),
  [ADR 0067](../adr/0067-session-context-comes-from-a-user-level-sessionstart-hook-the-tool-refreshes-a-stop-hook-reminds.md),
  [ADR 0070](../adr/0070-no-general-cli-the-mcp-binary-grows-workflow-subcommands.md)), the Claude
  Code plugin with its skills and hooks under `claude/`,
  [claude-code.md](../operations/claude-code.md); the chat (T44,
  [ADR 0076](../adr/0076-the-chat-in-the-ui-runs-its-loop-in-the-backend-as-an-agent-of-the-person.md)).
- **Verified 2026-10-04:** `cowork-mcp session-context` in this repository bound it by its remote to
  a project; over stdio `session_start`, `file_ticket` (with the commit subject, trailer and branch
  of [ADR 0068](../adr/0068-commits-carry-a-component-scope-the-short-key-in-the-subject-and-the-full-key-in-a-trailer.md)),
  three transitions, `record_state`, `open_question`, `finish_work` and `get_ticket` against a fresh
  backend, every act in the activity as "via claude-code/…"; the chat against LM Studio (T44).
  **Not verified:** a live `claude` session with the plugin, its hooks and skills; the `release-mcp`
  job (it runs only on a published release); Windows beyond compiling.
- **Decided without the owner**, recorded in the ADRs they amend (2026-10-04): the Stop hook reminds
  after every answer while the repository shows work and the ticket shows no act since the session
  started (ADR 0067 D4, D5); an agent with `create-project` may also unbind a repository; a fork with
  an `upstream` remote binds to the original's project; `repository_bound` tells that some project
  binds a repository, as `project_key_taken` tells a key is taken; `/context` is recorded as an
  export for every caller; the agent mark of `cowork-mcp` names the model `unknown`, because MCP
  does not tell the server the model.
- **Not built:** `cowork-mcp export` (it needs the export route of
  [ADR 0051](../adr/0051-import-is-a-server-side-two-phase-atomic-job-export-is-its-mirror.md)); the
  inbox and the score in the session block; `/me/next` and `/me/search` (T35, T37).

## Required changes

1. The owner answers Q1 and Q2; an answer that changes the build amends its ADR and the code.
2. A live check of the plugin: install it from this repository, start `claude` in a bound
   repository, and walk the phase's verification — the session names its ticket, Claude records its
   state, opens a question and finishes with a verification note, all visible in the UI with the
   agent's mark.
3. The SessionStart hook passes the model to `cowork-mcp` where Claude Code's hook input carries it,
   so the agent mark names it.
4. Phase close: T44 answered, the remaining items here or in their tickets, the phase-5 lines of
   [project-plan.md](../planning/project-plan.md) gone, this ticket archived.

## Open questions

### Q1: May `finish_work` close a ticket whose question is still open?

Nothing in the ADRs ties `done` to the open questions, and the live check closed a ticket with an
unanswered question. The working rule says not to build on an unanswered question.

- **(a) As built:** it closes, and the question stays open on a done ticket.
- **(b) `finish_work` refuses while a question of the ticket is open**, naming it; the API and the
  person's own moves stay as they are.
- **(c) The API refuses `done` with an open question** for everyone, the override of a person
  aside.

Recommended: **(b)** — the agent is the one that builds on decisions it did not take; the person
may still close what they decide to close.

**Answer:** _open_

### Q2: Are the release binaries of `cowork-mcp` signed?

The release job uploads six binaries and their `.sha256` files to the GitHub release; the checksums
come from the same page as the binaries (H-35), and macOS quarantines an unsigned download.

- **(a) As built:** checksums only.
- **(b) Keyless signatures** with the release workflow's own identity (Sigstore `cosign` or GitHub
  artifact attestations, which the image builds already make), verifiable without a key of ours.
- **(c) (b) plus Apple notarisation** of the darwin binaries.

Recommended: **(b)** — the workflow already holds `attestations: write`, so one step makes every
binary verifiable against the commit that built it; (c) when Gatekeeper is in the way in practice.

**Answer:** _open_

## Related

- T44 — the chat, its open questions
- T22 — the agent gates the MCP tools and the chat inherit
- T47 — phase 4
